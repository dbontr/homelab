//! Mutually authenticated authorization and scoped vault access.
use std::collections::BTreeSet;
use std::net::{IpAddr, SocketAddr};
use std::sync::Arc;
use std::time::Duration;

use axum::Router;
use hyper::server::conn::http1;
use hyper_util::rt::TokioIo;
use hyper_util::service::TowerToHyperService;
use tokio::net::TcpListener;
use tokio_rustls::rustls::pki_types::CertificateDer;
use tokio_rustls::rustls::server::WebPkiClientVerifier;
use tokio_rustls::rustls::{RootCertStore, ServerConfig};
use tokio_rustls::TlsAcceptor;
use x509_parser::extensions::GeneralName;
use x509_parser::prelude::parse_x509_certificate;

use crate::bootstrap::{configured_file, required};
use crate::grants::{valid_service_dns, VerifiedServiceIdentity};
use crate::transport::{remote_router, Shared};
use crate::Error;

#[derive(Clone)]
pub struct VerifiedPeer {
    pub(crate) identity: VerifiedServiceIdentity,
    pub(crate) may_authorize: bool,
}

pub struct Remote {
    address: SocketAddr,
    config: Arc<ServerConfig>,
    allowed: BTreeSet<String>,
    authorizers: BTreeSet<String>,
}

fn private_address(address: SocketAddr) -> bool {
    match address.ip() {
        IpAddr::V4(ip) => ip.is_private() || ip.is_loopback(),
        IpAddr::V6(ip) => ip.is_unique_local() || ip.is_loopback(),
    }
}

fn single_dns_identity(names: &[GeneralName<'_>]) -> Result<String, Error> {
    match names {
        [GeneralName::DNSName(name)] if valid_service_dns(name) => Ok((*name).to_owned()),
        _ => Err(Error::Unauthorized),
    }
}

fn certificate_identity(certificate: &CertificateDer<'_>) -> Result<String, Error> {
    let (_, certificate) =
        parse_x509_certificate(certificate.as_ref()).map_err(|_| Error::Unauthorized)?;
    let san = certificate
        .subject_alternative_name()
        .map_err(|_| Error::Unauthorized)?
        .ok_or(Error::Unauthorized)?;
    single_dns_identity(&san.value.general_names)
}

fn peer_roles(
    authorization_clients: Vec<String>,
    credential_clients: Vec<String>,
) -> Result<(BTreeSet<String>, BTreeSet<String>), Error> {
    if authorization_clients.is_empty()
        || authorization_clients.len() + credential_clients.len() > 16
    {
        return Err(Error::InvalidConfiguration);
    }
    let mut allowed = BTreeSet::new();
    let mut authorizers = BTreeSet::new();
    for client in authorization_clients {
        if !valid_service_dns(&client) || !allowed.insert(client.clone()) {
            return Err(Error::InvalidConfiguration);
        }
        authorizers.insert(client);
    }
    for client in credential_clients {
        if !valid_service_dns(&client) || !allowed.insert(client) {
            return Err(Error::InvalidConfiguration);
        }
    }
    Ok((allowed, authorizers))
}

impl Remote {
    pub fn from_environment() -> Result<Option<Self>, Error> {
        let Ok(listen) = std::env::var("HOMELAB_SECURITY_REMOTE_LISTEN") else {
            return Ok(None);
        };
        if listen.is_empty() {
            return Ok(None);
        }
        let address: SocketAddr = listen.parse().map_err(|_| Error::InvalidConfiguration)?;
        if !private_address(address) {
            return Err(Error::InvalidConfiguration);
        }
        let identities = required("HOMELAB_SECURITY_REMOTE_CLIENT_IDENTITIES")?;
        let clients: Vec<String> =
            serde_json::from_str(&identities).map_err(|_| Error::InvalidConfiguration)?;
        let credential_clients: Vec<String> =
            match std::env::var("HOMELAB_SECURITY_REMOTE_CREDENTIAL_CLIENT_IDENTITIES") {
                Ok(raw) => serde_json::from_str(&raw).map_err(|_| Error::InvalidConfiguration)?,
                Err(std::env::VarError::NotPresent) => Vec::new(),
                Err(_) => return Err(Error::InvalidConfiguration),
            };
        let (allowed, authorizers) = peer_roles(clients, credential_clients)?;

        let certificate = configured_file("HOMELAB_SECURITY_REMOTE_CERT", false)?;
        let private_key =
            zeroize::Zeroizing::new(configured_file("HOMELAB_SECURITY_REMOTE_KEY", true)?);
        let roots = configured_file("HOMELAB_SECURITY_REMOTE_CLIENT_CA", false)?;
        let server_chain = rustls_pemfile::certs(&mut certificate.as_slice())
            .collect::<Result<Vec<_>, _>>()
            .map_err(|_| Error::InvalidConfiguration)?;
        if server_chain.is_empty() || server_chain.len() > 8 {
            return Err(Error::InvalidConfiguration);
        }
        let server_key = rustls_pemfile::private_key(&mut private_key.as_slice())
            .map_err(|_| Error::InvalidConfiguration)?
            .ok_or(Error::InvalidConfiguration)?;
        let mut trusted = RootCertStore::empty();
        let certificates = rustls_pemfile::certs(&mut roots.as_slice())
            .collect::<Result<Vec<_>, _>>()
            .map_err(|_| Error::InvalidConfiguration)?;
        if certificates.is_empty() || certificates.len() > 16 {
            return Err(Error::InvalidConfiguration);
        }
        for cert in certificates {
            trusted.add(cert).map_err(|_| Error::InvalidConfiguration)?;
        }
        let verifier = WebPkiClientVerifier::builder(Arc::new(trusted))
            .build()
            .map_err(|_| Error::InvalidConfiguration)?;
        let mut config =
            ServerConfig::builder_with_protocol_versions(&[&tokio_rustls::rustls::version::TLS13])
                .with_client_cert_verifier(verifier)
                .with_single_cert(server_chain, server_key)
                .map_err(|_| Error::InvalidConfiguration)?;
        config.alpn_protocols = vec![b"http/1.1".to_vec()];
        Ok(Some(Self {
            address,
            config: Arc::new(config),
            allowed,
            authorizers,
        }))
    }

    pub async fn serve(
        self,
        shared: Shared,
    ) -> Result<(), Box<dyn std::error::Error + Send + Sync>> {
        let listener = TcpListener::bind(self.address).await?;
        let acceptor = TlsAcceptor::from(self.config);
        let allowed = Arc::new(self.allowed);
        let authorizers = Arc::new(self.authorizers);
        loop {
            let (socket, _) = listener.accept().await?;
            let acceptor = acceptor.clone();
            let allowed = allowed.clone();
            let authorizers = authorizers.clone();
            let shared = shared.clone();
            tokio::spawn(async move {
                let Ok(Ok(stream)) =
                    tokio::time::timeout(Duration::from_secs(5), acceptor.accept(socket)).await
                else {
                    return;
                };
                let Some(peer) = stream
                    .get_ref()
                    .1
                    .peer_certificates()
                    .and_then(|certs| certs.first())
                else {
                    return;
                };
                let Ok(identity) = certificate_identity(peer) else {
                    return;
                };
                if !allowed.contains(&identity) {
                    return;
                }
                let may_authorize = authorizers.contains(&identity);
                let Ok(identity) = VerifiedServiceIdentity::from_authenticated_peer(identity)
                else {
                    return;
                };
                // The verified peer is injected by the TLS listener, never an HTTP header.
                let router: Router = remote_router(shared).layer(axum::Extension(VerifiedPeer {
                    identity,
                    may_authorize,
                }));
                let connection = http1::Builder::new()
                    .serve_connection(TokioIo::new(stream), TowerToHyperService::new(router));
                let _ = tokio::time::timeout(Duration::from_secs(15), connection).await;
            });
        }
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn identity_rejects_mixed_subject_alternative_names() {
        assert_eq!(
            single_dns_identity(&[GeneralName::DNSName("executor.internal")]).unwrap(),
            "executor.internal"
        );
        assert!(single_dns_identity(&[
            GeneralName::DNSName("executor.internal"),
            GeneralName::IPAddress(&[10, 77, 30, 1]),
        ])
        .is_err());
        assert!(single_dns_identity(&[
            GeneralName::DNSName("executor.internal"),
            GeneralName::URI("spiffe://cluster/executor"),
        ])
        .is_err());
        assert!(single_dns_identity(&[
            GeneralName::DNSName("executor.internal"),
            GeneralName::DNSName("other.internal"),
        ])
        .is_err());
        assert!(single_dns_identity(&[GeneralName::DNSName("*.internal")]).is_err());
    }
    #[test]
    fn credential_only_peers_cannot_authorize_operations() {
        let (allowed, authorizers) = peer_roles(
            vec!["executor.internal".into()],
            vec!["media.internal".into(), "compute.internal".into()],
        )
        .unwrap();
        assert!(allowed.contains("executor.internal"));
        assert!(allowed.contains("media.internal"));
        assert!(authorizers.contains("executor.internal"));
        assert!(!authorizers.contains("media.internal"));

        for (auth, credentials) in [
            (vec![], vec!["media.internal".into()]),
            (
                vec!["executor.internal".into()],
                vec!["executor.internal".into()],
            ),
            (
                vec!["executor.internal".into(), "executor.internal".into()],
                vec![],
            ),
            (
                vec!["executor.internal".into()],
                vec!["media.internal".into(), "media.internal".into()],
            ),
            (vec!["executor.internal".into()], vec!["*.internal".into()]),
        ] {
            assert!(peer_roles(auth, credentials).is_err());
        }
    }

    #[test]
    fn only_explicit_private_bind_addresses_are_valid() {
        assert!(private_address("10.77.30.1:8443".parse().unwrap()));
        assert!(private_address("127.0.0.1:8443".parse().unwrap()));
        assert!(private_address("[fd00::1]:8443".parse().unwrap()));
        assert!(!private_address("0.0.0.0:8443".parse().unwrap()));
        assert!(!private_address("[::]:8443".parse().unwrap()));
        assert!(!private_address("8.8.8.8:8443".parse().unwrap()));
    }
}
