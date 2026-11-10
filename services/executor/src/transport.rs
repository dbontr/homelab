//! Separate gateway and mutually authenticated release-coordinator endpoints.
use std::ffi::CString;
use std::fs;
use std::net::{IpAddr, SocketAddr};
use std::os::unix::fs::{FileTypeExt, MetadataExt, PermissionsExt};
use std::path::{Path, PathBuf};
use std::sync::Arc;
use std::time::Duration;

use axum::body::Bytes;
use axum::extract::{DefaultBodyLimit, State};
use axum::http::{header, HeaderMap, HeaderValue, StatusCode};
use axum::middleware::{self, Next};
use axum::response::{IntoResponse, Response};
use axum::routing::{get, post};
use axum::{Json, Router};
use hyper::server::conn::http1;
use hyper_util::rt::{TokioIo, TokioExecutor};
use hyper_util::server::conn::auto::Builder;
use hyper_util::service::TowerToHyperService;
use serde::Deserialize;
use serde::Serialize;
use tokio::net::{TcpListener, UnixListener};
use tokio_rustls::rustls::server::WebPkiClientVerifier;
use tokio_rustls::rustls::{RootCertStore, ServerConfig};
use tokio_rustls::TlsAcceptor;
use x509_parser::extensions::GeneralName;
use x509_parser::prelude::parse_x509_certificate;
use zeroize::Zeroizing;

use crate::canonical::canonical_arguments;
use crate::executor::{ExecutionRequest, ExecutionResult, Executor, StageResult};
use crate::policy::{absolute, bounded_file, required};
use crate::Error;

const MAX_BODY: usize = 64 << 10;

fn json_input<T: serde::de::DeserializeOwned>(headers: &HeaderMap, raw: &[u8])
    -> Result<T, Response>
{
    if raw.len() > MAX_BODY || headers.get(header::CONTENT_TYPE)
        .and_then(|v| v.to_str().ok()) != Some("application/json") {
        return Err((StatusCode::BAD_REQUEST, "application/json body required").into_response());
    }
    let canonical = canonical_arguments(raw)
        .map_err(|_| (StatusCode::BAD_REQUEST, "invalid canonical JSON").into_response())?;
    serde_json::from_slice(&canonical)
        .map_err(|_| (StatusCode::BAD_REQUEST, "invalid request schema").into_response())
}
fn assertion(headers: &HeaderMap) -> Result<&str, Response> {
    headers.get("cf-access-jwt-assertion").and_then(|v| v.to_str().ok())
        .filter(|s| !s.is_empty() && s.len() <= (16 << 10))
        .ok_or_else(|| (StatusCode::FORBIDDEN, "signed owner assertion required").into_response())
}
fn result<T: Serialize>(value: T) -> Response {
    (StatusCode::OK, Json(value)).into_response()
}
fn refused(error: Error) -> Response {
    let status = match error {
        Error::Denied(_) => StatusCode::FORBIDDEN,
        Error::Configuration(_) | Error::Storage(_) | Error::Unavailable(_)
        | Error::Protocol(_) => StatusCode::SERVICE_UNAVAILABLE,
    };
    (status, "independent privileged operation unavailable").into_response()
}
async fn private_headers(request: axum::extract::Request, next: Next) -> Response {
    let mut response = next.run(request).await;
    let headers = response.headers_mut();
    headers.insert(header::CACHE_CONTROL, HeaderValue::from_static("no-store"));
    headers.insert(header::PRAGMA, HeaderValue::from_static("no-cache"));
    headers.insert("x-content-type-options", HeaderValue::from_static("nosniff"));
    headers.insert("referrer-policy", HeaderValue::from_static("no-referrer"));
    response
}
async fn execute(State(executor): State<Arc<Executor>>, headers: HeaderMap, raw: Bytes) -> Response {
    let Ok(owner) = assertion(&headers) else {
        return (StatusCode::FORBIDDEN, "signed owner assertion required").into_response();
    };
    let Ok(request) = json_input::<ExecutionRequest>(&headers, &raw) else {
        return (StatusCode::BAD_REQUEST, "invalid privileged request").into_response();
    };
    match executor.execute(request, owner).await {
        Ok(value) => result::<ExecutionResult>(value),
        Err(error) => refused(error),
    }
}

#[derive(Deserialize)]
#[serde(deny_unknown_fields)]
struct StageRequest {
    host: String,
    revision: String,
}
#[derive(Deserialize)]
#[serde(deny_unknown_fields)]
struct PreviewRequest {
    host: String,
    release_id: String,
    action: String,
}

#[derive(Clone)]
struct VerifiedCoordinator {
    dns: String,
}
async fn remote_execute(
    axum::Extension(peer): axum::Extension<VerifiedCoordinator>,
    state: State<Arc<Executor>>, headers: HeaderMap, raw: Bytes
) -> Response {
    if peer.dns.is_empty() { return (StatusCode::FORBIDDEN, "coordinator identity missing").into_response(); }
    execute(state, headers, raw).await
}
async fn remote_stage(
    axum::Extension(_peer): axum::Extension<VerifiedCoordinator>,
    State(executor): State<Arc<Executor>>, headers: HeaderMap, raw: Bytes
) -> Response {
    let Ok(owner) = assertion(&headers) else {
        return (StatusCode::FORBIDDEN, "signed owner assertion required").into_response();
    };
    if raw.len() > 1024 { return (StatusCode::BAD_REQUEST, "release staging request too large").into_response(); }
    let Ok(input) = json_input::<StageRequest>(&headers, &raw) else {
        return (StatusCode::BAD_REQUEST, "invalid staging request").into_response();
    };
    match executor.stage(&input.host, &input.revision, owner).await {
        Ok(value) => result::<StageResult>(value),
        Err(error) => refused(error),
    }
}
async fn remote_preview(
    axum::Extension(_peer): axum::Extension<VerifiedCoordinator>,
    State(executor): State<Arc<Executor>>, headers: HeaderMap, raw: Bytes
) -> Response {
    let Ok(owner) = assertion(&headers) else {
        return (StatusCode::FORBIDDEN, "signed owner assertion required").into_response();
    };
    if raw.len() > 1024 { return (StatusCode::BAD_REQUEST, "release preview request too large").into_response(); }
    let Ok(input) = json_input::<PreviewRequest>(&headers, &raw) else {
        return (StatusCode::BAD_REQUEST, "invalid release preview request").into_response();
    };
    match executor.preview(&input.host, &input.release_id, &input.action, owner).await {
        Ok(value) => result(value),
        Err(error) => refused(error),
    }
}
async fn remote_status(
    axum::Extension(_peer): axum::Extension<VerifiedCoordinator>,
    State(executor): State<Arc<Executor>>, headers: HeaderMap
) -> Response {
    let Ok(owner) = assertion(&headers) else {
        return (StatusCode::FORBIDDEN, "signed owner assertion required").into_response();
    };
    match executor.observe(owner).await {
        Ok(value) => result(value),
        Err(error) => refused(error),
    }
}

pub fn gateway_router(executor: Arc<Executor>) -> Router {
    Router::new().route("/v1/execute", post(execute))
        .layer(DefaultBodyLimit::max(MAX_BODY))
        .layer(middleware::from_fn(private_headers))
        .with_state(executor)
}
fn coordinator_router(executor: Arc<Executor>) -> Router {
    Router::new()
        .route("/v1/execute", post(remote_execute))
        .route("/v1/release/stage", post(remote_stage))
        .route("/v1/release/preview", post(remote_preview))
        .route("/v1/release/status", get(remote_status))
        .layer(DefaultBodyLimit::max(MAX_BODY))
        .layer(middleware::from_fn(private_headers))
        .with_state(executor)
}

struct SocketGuard {
    path: PathBuf, device: u64, inode: u64,
}
impl Drop for SocketGuard {
    fn drop(&mut self) {
        if let Ok(meta) = fs::symlink_metadata(&self.path) {
            if meta.file_type().is_socket() && meta.dev() == self.device && meta.ino() == self.inode {
                let _ = fs::remove_file(&self.path);
            }
        }
    }
}
pub fn client_group() -> Result<u32, Error> {
    let name = CString::new("homelab-executor-client").map_err(|_| Error::Configuration("invalid executor group"))?;
    let group = unsafe { libc::getgrnam(name.as_ptr()) };
    if group.is_null() { return Err(Error::Configuration("executor client group unavailable")); }
    let gid = unsafe { (*group).gr_gid };
    if gid == 0 { return Err(Error::Configuration("executor client group must not be root")); }
    Ok(gid)
}
pub async fn serve_unix(executor: Arc<Executor>, path: &Path, gid: u32)
    -> Result<(), Box<dyn std::error::Error + Send + Sync>>
{
    if !path.is_absolute() || gid == 0 { return Err("invalid executor socket path".into()); }
    let dir = path.parent().ok_or("missing executor runtime directory")?;
    let metadata = fs::symlink_metadata(dir)?;
    if !metadata.is_dir() || metadata.file_type().is_symlink() || metadata.mode() & 0o007 != 0 {
        return Err("executor runtime directory has unsafe permissions".into());
    }
    let owner = CString::new(dir.as_os_str().as_encoded_bytes())?;
    if unsafe { libc::chown(owner.as_ptr(), !0, gid) } != 0 {
        return Err(std::io::Error::last_os_error().into());
    }
    fs::set_permissions(dir, fs::Permissions::from_mode(0o750))?;
    if fs::symlink_metadata(path).is_ok() { return Err("executor socket already exists".into()); }
    let listener = UnixListener::bind(path)?;
    let socket = CString::new(path.as_os_str().as_encoded_bytes())?;
    if unsafe { libc::chown(socket.as_ptr(), !0, gid) } != 0 {
        return Err(std::io::Error::last_os_error().into());
    }
    fs::set_permissions(path, fs::Permissions::from_mode(0o660))?;
    let meta = fs::symlink_metadata(path)?;
    let _guard = SocketGuard { path: path.into(), device: meta.dev(), inode: meta.ino() };
    let router = gateway_router(executor);
    loop {
        let (stream, _) = listener.accept().await?;
        let service = router.clone();
        tokio::spawn(async move {
            let connection = Builder::new(TokioExecutor::new())
                .serve_connection(TokioIo::new(stream), TowerToHyperService::new(service));
            let _ = tokio::time::timeout(Duration::from_secs(3*60*60), connection).await;
        });
    }
}

pub struct Remote {
    address: SocketAddr,
    expected_peer: String,
    config: Arc<ServerConfig>,
}
fn private_ip(address: SocketAddr) -> bool {
    match address.ip() {
        IpAddr::V4(ip) => ip.is_private() || ip.is_loopback(),
        IpAddr::V6(ip) => ip.is_unique_local() || ip.is_loopback(),
    }
}
fn certificate_dns(certificate: &[u8]) -> Result<String, Error> {
    let (_, cert) = parse_x509_certificate(certificate)
        .map_err(|_| Error::Denied("invalid coordinator certificate"))?;
    let san = cert.subject_alternative_name()
        .map_err(|_| Error::Denied("invalid coordinator identity"))?
        .ok_or(Error::Denied("coordinator DNS SAN missing"))?;
    let names = san.value.general_names.iter().filter_map(|v|
        if let GeneralName::DNSName(name) = v { Some((*name).to_owned()) } else { None }
    ).collect::<Vec<_>>();
    if names.len() != 1 { return Err(Error::Denied("ambiguous coordinator DNS identity")); }
    Ok(names[0].clone())
}
impl Remote {
    pub fn from_environment() -> Result<Option<Self>, Error> {
        let Some(address) = std::env::var("HOMELAB_EXECUTOR_REMOTE_LISTEN")
            .ok().filter(|s| !s.is_empty()) else { return Ok(None); };
        let address: SocketAddr = address.parse()
            .map_err(|_| Error::Configuration("invalid remote executor listener"))?;
        if !private_ip(address) { return Err(Error::Configuration("remote executor must bind a private IP")); }
        let expected_peer = required("HOMELAB_EXECUTOR_REMOTE_COORDINATOR")?;
        if expected_peer.is_empty() || expected_peer.len() > 253 ||
            expected_peer.bytes().any(|b| b.is_ascii_whitespace() ||
                matches!(b, b'/' | b'\\' | b':' | b'@' | b';' | 0)) {
            return Err(Error::Configuration("invalid coordinator DNS identity"));
        }
        let server_cert = bounded_file(&absolute("HOMELAB_EXECUTOR_REMOTE_CERT")?, 1<<20,false)?;
        let private_key = Zeroizing::new(bounded_file(&absolute("HOMELAB_EXECUTOR_REMOTE_KEY")?, 1<<20,true)?);
        let roots = bounded_file(&absolute("HOMELAB_EXECUTOR_REMOTE_CA")?,1<<20,false)?;
        let certs = rustls_pemfile::certs(&mut server_cert.as_slice())
            .collect::<Result<Vec<_>,_>>()
            .map_err(|_| Error::Configuration("invalid executor server certificate"))?;
        let key = rustls_pemfile::private_key(&mut private_key.as_slice())
            .map_err(|_| Error::Configuration("invalid executor server key"))?
            .ok_or(Error::Configuration("server identity key missing"))?;
        let mut trusted = RootCertStore::empty();
        for cert in rustls_pemfile::certs(&mut roots.as_slice()) {
            trusted.add(cert.map_err(|_| Error::Configuration("invalid coordinator CA"))?)
                .map_err(|_| Error::Configuration("invalid coordinator trust anchor"))?;
        }
        if trusted.is_empty() || certs.is_empty() {
            return Err(Error::Configuration("executor TLS certificate policy incomplete"));
        }
        let verifier = WebPkiClientVerifier::builder(Arc::new(trusted))
            .build().map_err(|_| Error::Configuration("coordinator client authentication unavailable"))?;
        let mut config = ServerConfig::builder_with_protocol_versions(
            &[&tokio_rustls::rustls::version::TLS13])
            .with_client_cert_verifier(verifier)
            .with_single_cert(certs,key)
            .map_err(|_| Error::Configuration("invalid executor TLS server identity"))?;
        config.alpn_protocols = vec![b"http/1.1".to_vec()];
        Ok(Some(Self { address, expected_peer, config: Arc::new(config) }))
    }

    pub async fn serve(self, executor: Arc<Executor>)
        -> Result<(), Box<dyn std::error::Error + Send + Sync>>
    {
        let listener = TcpListener::bind(self.address).await?;
        let acceptor = TlsAcceptor::from(self.config);
        loop {
            let (stream, _) = listener.accept().await?;
            let acceptor = acceptor.clone();
            let expected = self.expected_peer.clone();
            let executor = executor.clone();
            tokio::spawn(async move {
                let Ok(Ok(stream)) = tokio::time::timeout(
                    Duration::from_secs(5), acceptor.accept(stream)).await else { return; };
                let Some(cert) = stream.get_ref().1.peer_certificates().and_then(|c| c.first()) else { return; };
                let Ok(dns) = certificate_dns(cert.as_ref()) else { return; };
                if dns != expected { return; }
                let router = coordinator_router(executor)
                    .layer(axum::Extension(VerifiedCoordinator { dns }));
                let connection = http1::Builder::new()
                    .serve_connection(TokioIo::new(stream), TowerToHyperService::new(router));
                let _ = tokio::time::timeout(Duration::from_secs(3*60*60),connection).await;
            });
        }
    }
}
