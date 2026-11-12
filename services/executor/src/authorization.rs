//! Independently authenticated owner decisions over private Unix IPC or TLS 1.3.
use std::path::PathBuf;
use std::sync::Arc;
use std::time::Duration;

use bytes::Bytes;
use http_body_util::{BodyExt, Full};
use hyper::client::conn::http1;
use hyper::http::{header, Method, Request, StatusCode};
use hyper_util::rt::TokioIo;
use serde::{Deserialize, Serialize};
use tokio::net::{TcpStream, UnixStream};
use tokio_rustls::rustls::pki_types::ServerName;
use tokio_rustls::rustls::{ClientConfig, RootCertStore};
use tokio_rustls::TlsConnector;
use url::Url;
use zeroize::Zeroizing;

use crate::policy::{absolute, bounded_file, required};
use crate::transport::valid_coordinator_dns;
use crate::Error;

#[derive(Clone, Debug, Deserialize)]
#[serde(deny_unknown_fields)]
pub struct CredentialTicket {
    pub alias: String,
    pub ticket: String,
}

#[derive(Clone, Debug, Default, Deserialize, Serialize)]
#[serde(deny_unknown_fields)]
pub struct Decision {
    pub allowed: bool,
    #[serde(default, skip_serializing_if = "String::is_empty")]
    pub reason: String,
    #[serde(default, skip_serializing_if = "String::is_empty")]
    pub confirmation_id: String,
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub confirmation_expires_at: Option<String>,
    #[serde(default, skip_serializing)]
    pub credential_tickets: Vec<CredentialTicket>,
}
impl Decision {
    pub fn denied(reason: &str) -> Self {
        Self {
            reason: reason.to_owned(),
            ..Self::default()
        }
    }
}

#[derive(Clone)]
pub struct AuthorizationClient {
    source: Arc<Source>,
}
enum Source {
    Local(PathBuf),
    Remote(Remote),
}
struct Remote {
    endpoint: Url,
    server_name: String,
    tls: Arc<ClientConfig>,
}

async fn exchange<S>(
    io: S,
    host: &str,
    path: &str,
    method: Method,
    assertion: &str,
    content: Vec<u8>,
    limit: usize,
) -> Result<(StatusCode, Option<String>, Vec<u8>), Error>
where
    S: tokio::io::AsyncRead + tokio::io::AsyncWrite + Unpin + Send + 'static,
{
    let mut builder = http1::Builder::new();
    builder.max_buf_size(16 << 10);
    let (mut sender, connection) = builder
        .handshake(TokioIo::new(io))
        .await
        .map_err(|_| Error::Unavailable("authorization handshake failed"))?;
    tokio::spawn(async move {
        let _ = connection.await;
    });
    let mut request = Request::builder()
        .method(method)
        .uri(path)
        .header(header::HOST, host)
        .header(header::ACCEPT, "application/json")
        .header(header::CONNECTION, "close");
    if !assertion.is_empty() {
        if assertion.len() > 16 << 10 {
            return Err(Error::Denied("owner assertion too large"));
        }
        request = request.header("cf-access-jwt-assertion", assertion);
    }
    if !content.is_empty() {
        request = request.header(header::CONTENT_TYPE, "application/json");
    }
    let request = request
        .body(Full::new(Bytes::from(content)))
        .map_err(|_| Error::Protocol("invalid authorization request"))?;
    let response = sender
        .send_request(request)
        .await
        .map_err(|_| Error::Unavailable("authorization service unavailable"))?;
    let status = response.status();
    let content_type = response
        .headers()
        .get(header::CONTENT_TYPE)
        .and_then(|v| v.to_str().ok())
        .map(str::to_owned);
    let mut body = response.into_body();
    let mut received = Vec::new();
    while let Some(frame) = body.frame().await {
        let frame = frame.map_err(|_| Error::Protocol("authorization reply unreadable"))?;
        if let Ok(chunk) = frame.into_data() {
            if chunk.len() > limit.saturating_sub(received.len()) {
                return Err(Error::Protocol("authorization response too large"));
            }
            received.extend_from_slice(&chunk);
        }
    }
    Ok((status, content_type, received))
}

impl AuthorizationClient {
    pub fn load() -> Result<Self, Error> {
        let source = if let Ok(endpoint) = std::env::var("HOMELAB_EXECUTOR_AUTH_ENDPOINT") {
            if endpoint.is_empty() {
                return Err(Error::Configuration("empty authorization endpoint"));
            }
            let url = Url::parse(&endpoint)
                .map_err(|_| Error::Configuration("invalid authorizer endpoint"))?;
            if url.scheme() != "https"
                || url.host_str().is_none()
                || !url.username().is_empty()
                || url.password().is_some()
                || url.query().is_some()
                || url.fragment().is_some()
                || !matches!(url.path(), "" | "/")
            {
                return Err(Error::Configuration(
                    "authorizer endpoint must be an exact HTTPS origin",
                ));
            }
            let mut trust = RootCertStore::empty();
            let roots = bounded_file(&absolute("HOMELAB_EXECUTOR_AUTH_CA")?, 1 << 20, false)?;
            for cert in rustls_pemfile::certs(&mut roots.as_slice()) {
                trust
                    .add(cert.map_err(|_| Error::Configuration("invalid authority trust roots"))?)
                    .map_err(|_| Error::Configuration("invalid authority trust anchor"))?;
            }
            if trust.is_empty() {
                return Err(Error::Configuration("authorization trust anchors missing"));
            }
            let client_cert =
                bounded_file(&absolute("HOMELAB_EXECUTOR_AUTH_CERT")?, 1 << 20, false)?;
            let client_key = Zeroizing::new(bounded_file(
                &absolute("HOMELAB_EXECUTOR_AUTH_KEY")?,
                1 << 20,
                true,
            )?);
            let chain = rustls_pemfile::certs(&mut client_cert.as_slice())
                .collect::<Result<Vec<_>, _>>()
                .map_err(|_| Error::Configuration("invalid authorization client chain"))?;
            if chain.is_empty() || chain.len() > 8 {
                return Err(Error::Configuration(
                    "authorization client certificate missing",
                ));
            }
            let key = rustls_pemfile::private_key(&mut client_key.as_slice())
                .map_err(|_| Error::Configuration("invalid client private key"))?
                .ok_or(Error::Configuration("client private key missing"))?;
            let mut tls = ClientConfig::builder_with_protocol_versions(&[
                &tokio_rustls::rustls::version::TLS13,
            ])
            .with_root_certificates(trust)
            .with_client_auth_cert(chain, key)
            .map_err(|_| Error::Configuration("invalid client identity"))?;
            tls.alpn_protocols = vec![b"http/1.1".to_vec()];
            let server_name = required("HOMELAB_EXECUTOR_AUTH_SERVER_NAME")?;
            if !valid_coordinator_dns(&server_name) {
                return Err(Error::Configuration(
                    "invalid remote authorization server name",
                ));
            }
            ServerName::try_from(server_name.clone())
                .map_err(|_| Error::Configuration("invalid remote authorization server name"))?;
            Source::Remote(Remote {
                endpoint: url,
                server_name,
                tls: Arc::new(tls),
            })
        } else {
            Source::Local(absolute("HOMELAB_EXECUTOR_SECURITY_SOCKET")?)
        };
        Ok(Self {
            source: Arc::new(source),
        })
    }

    async fn request(
        &self,
        path: &str,
        method: Method,
        assertion: &str,
        content: Vec<u8>,
        limit: usize,
    ) -> Result<(StatusCode, Option<String>, Vec<u8>), Error> {
        tokio::time::timeout(Duration::from_secs(10), async {
            match self.source.as_ref() {
                Source::Local(socket) => {
                    let io = UnixStream::connect(socket)
                        .await
                        .map_err(|_| Error::Unavailable("local security authority unavailable"))?;
                    exchange(io, "unix", path, method, assertion, content, limit).await
                }
                Source::Remote(remote) => {
                    let host = remote
                        .endpoint
                        .host_str()
                        .ok_or(Error::Configuration("invalid TLS endpoint"))?;
                    let port = remote
                        .endpoint
                        .port_or_known_default()
                        .ok_or(Error::Configuration("missing authorization port"))?;
                    let stream = TcpStream::connect((host, port))
                        .await
                        .map_err(|_| Error::Unavailable("remote authority unavailable"))?;
                    let name = ServerName::try_from(remote.server_name.clone())
                        .map_err(|_| Error::Configuration("invalid TLS server identity"))?;
                    let tls = TlsConnector::from(remote.tls.clone())
                        .connect(name, stream)
                        .await
                        .map_err(|_| {
                            Error::Unavailable("remote owner identity verification failed")
                        })?;
                    let host_header = if host.contains(':') {
                        format!("[{host}]:{port}")
                    } else {
                        format!("{host}:{port}")
                    };
                    exchange(tls, &host_header, path, method, assertion, content, limit).await
                }
            }
        })
        .await
        .map_err(|_| Error::Unavailable("authorization deadline exceeded"))?
    }

    pub async fn decision(
        &self,
        operation: &str,
        target: &str,
        arguments: &[u8],
        assertion: &str,
        consume: bool,
    ) -> Result<Decision, Error> {
        let path = if consume {
            "/v1/authorize"
        } else {
            "/v1/preview"
        };
        self.decision_path(operation, target, arguments, assertion, path, consume)
            .await
    }

    /// Verify replay observation against the current independent owner
    /// session and finite operation policy without consuming an approval.
    pub async fn replay(
        &self,
        operation: &str,
        target: &str,
        arguments: &[u8],
        assertion: &str,
    ) -> Result<Decision, Error> {
        self.decision_path(operation, target, arguments, assertion, "/v1/replay", false)
            .await
    }

    async fn decision_path(
        &self,
        operation: &str,
        target: &str,
        arguments: &[u8],
        assertion: &str,
        path: &str,
        consume: bool,
    ) -> Result<Decision, Error> {
        if assertion.is_empty() || assertion.len() > 16 << 10 {
            return Err(Error::Denied("signed owner assertion required"));
        }
        let args: serde_json::Value = serde_json::from_slice(arguments)
            .map_err(|_| Error::Denied("invalid authorization arguments"))?;
        let payload = serde_json::to_vec(&serde_json::json!({
            "operation": operation, "target": target, "arguments": args
        }))
        .map_err(|_| Error::Protocol("authorization request encoding failed"))?;
        let (status, mime, result) = self
            .request(path, Method::POST, assertion, payload, 8192)
            .await?;
        if status != StatusCode::OK
            || !mime
                .as_deref()
                .is_some_and(|v| v.starts_with("application/json"))
        {
            return Err(Error::Denied(
                "independent authorization rejected the operation",
            ));
        }
        let decision: Decision = serde_json::from_slice(&result)
            .map_err(|_| Error::Protocol("invalid authorization decision"))?;
        if decision.allowed && (!decision.reason.is_empty() || !decision.confirmation_id.is_empty())
        {
            return Err(Error::Protocol("contradictory authorization decision"));
        }
        if (!consume || !decision.allowed) && !decision.credential_tickets.is_empty() {
            return Err(Error::Protocol(
                "authorization returned unexpected credential tickets",
            ));
        }
        if decision.credential_tickets.len() > 8 {
            return Err(Error::Protocol("credential ticket capacity exceeded"));
        }
        let mut aliases = std::collections::BTreeSet::new();
        for item in &decision.credential_tickets {
            if item.alias.is_empty()
                || item.alias.len() > 64
                || !item.alias.as_bytes()[0].is_ascii_alphabetic()
                || !item
                    .alias
                    .bytes()
                    .all(|c| c.is_ascii_alphanumeric() || matches!(c, b'_' | b'-'))
                || item.ticket.len() != 48
                || !item.ticket.bytes().all(|b| b.is_ascii_hexdigit())
                || !aliases.insert(&item.alias)
            {
                return Err(Error::Protocol("invalid credential ticket"));
            }
        }
        Ok(decision)
    }

    /// Ticket redemption is one-use and remains bound to the authorized
    /// operation and exact original canonical arguments.
    pub async fn vault_credential(
        &self,
        alias: &str,
        ticket: &str,
        operation: &str,
        target: &str,
        arguments: &[u8],
    ) -> Result<Zeroizing<Vec<u8>>, Error> {
        if !matches!(self.source.as_ref(), Source::Remote(_)) {
            return Err(Error::Denied("vault credentials require mutual TLS"));
        }
        if alias.is_empty()
            || alias.len() > 64
            || !alias.as_bytes()[0].is_ascii_alphabetic()
            || !alias
                .bytes()
                .all(|c| c.is_ascii_alphanumeric() || matches!(c, b'_' | b'-'))
            || ticket.len() != 48
            || !ticket.bytes().all(|c| c.is_ascii_hexdigit())
        {
            return Err(Error::Denied("invalid service credential capability"));
        }
        let canonical = crate::canonical::canonical_arguments(arguments)?;
        let args: serde_json::Value = serde_json::from_slice(&canonical)
            .map_err(|_| Error::Denied("invalid credential authorization context"))?;
        let body = serde_json::to_vec(&serde_json::json!({
            "ticket": ticket, "operation": operation, "target": target,
            "arguments": args
        }))
        .map_err(|_| Error::Protocol("invalid credential redemption request"))?;
        let (status, mime, result) = self
            .request(
                &format!("/v1/service-credentials/{alias}"),
                Method::POST,
                "",
                body,
                8192,
            )
            .await?;
        if status != StatusCode::OK
            || mime.as_deref() != Some("application/octet-stream")
            || result.is_empty()
        {
            return Err(Error::Denied("service credential capability denied"));
        }
        Ok(Zeroizing::new(result))
    }
}
