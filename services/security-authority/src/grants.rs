//! Grant one pinned mTLS service identity access to one credential handle.
use std::collections::BTreeMap;

use serde::{Deserialize, Serialize};

use crate::vault::Vault;
use crate::Error;

const MAX_GRANTS: usize = 128;

#[derive(Clone, Debug, Deserialize, Serialize)]
#[serde(deny_unknown_fields)]
pub struct ServiceCredentialGrant {
    pub client_dns: String,
    pub alias: String,
    pub provider: String,
    pub handle: String,
    pub operation: String,
    pub target: String,
    #[serde(default)]
    pub arguments_sha256: Option<String>,
    #[serde(default)]
    pub revision: Option<String>,
    #[serde(default)]
    pub expires_at: Option<String>,
}

fn valid_name(name: &str) -> bool {
    name.len() <= 64
        && name.bytes().next().is_some_and(|c| c.is_ascii_alphabetic())
        && name.bytes().skip(1).all(|c| c.is_ascii_alphanumeric() || matches!(c, b'_' | b'-'))
}

// A peer identity is a canonical ASCII DNS name, not a wildcard or URL.
// Keep certificate SAN and credential-grant validation identical.
pub(crate) fn valid_service_dns(name: &str) -> bool {
    if name.is_empty() || name.len() > 253
        || name.parse::<std::net::IpAddr>().is_ok() {
        return false;
    }
    name.split('.').all(|label| {
        let bytes = label.as_bytes();
        (1..=63).contains(&bytes.len())
            && bytes.first().is_some_and(|byte| byte.is_ascii_lowercase() || byte.is_ascii_digit())
            && bytes.last().is_some_and(|byte| byte.is_ascii_lowercase() || byte.is_ascii_digit())
            && bytes.iter().all(|byte| byte.is_ascii_lowercase()
                || byte.is_ascii_digit() || *byte == b'-')
    })
}

fn valid_operation(operation: &str) -> bool {
    operation.len() <= 128
        && operation.bytes().next().is_some_and(|b| b.is_ascii_alphanumeric())
        && operation.bytes().all(|b| b.is_ascii_alphanumeric()
            || matches!(b, b'_' | b'-' | b'.' | b'/' | b':'))
}

fn valid_target(target: &str) -> bool {
    valid_operation(target)
}

fn valid_revision(value: &str) -> bool {
    value.len() == 64 && value.bytes()
        .all(|c| c.is_ascii_digit() || (b'a'..=b'f').contains(&c))
}

fn valid_handle(handle: &str) -> bool {
    handle.len() == 32
        && handle.bytes().all(|c| c.is_ascii_alphanumeric() || matches!(c, b'-' | b'_'))
}

/// Constructed only by the transport after it validates the peer TLS chain,
/// hostname and exact single DNS SAN against the trusted service allowlist.
#[derive(Clone)]
pub(crate) struct VerifiedServiceIdentity {
    dns: String,
}

impl VerifiedServiceIdentity {
    pub(crate) fn from_authenticated_peer(dns: String) -> Result<Self, Error> {
        if !valid_service_dns(&dns) {
            return Err(Error::Unauthorized);
        }
        Ok(Self { dns })
    }
}

struct BoundGrant {
    provider: String,
    handle: String,
    revision: String,
    arguments_sha256: Option<String>,
    expires_at: Option<i64>,
}

#[derive(Clone)]
pub(crate) struct ScopedCredential {
    pub alias: String,
    pub revision: String,
}

pub(crate) struct ServiceCredentialPolicy {
    entries: BTreeMap<(String, String, String, String), BoundGrant>,
}

impl ServiceCredentialPolicy {
    pub(crate) fn new(grants: Vec<ServiceCredentialGrant>, vault: &Vault)
        -> Result<Self, Error>
    {
        if grants.len() > MAX_GRANTS {
            return Err(Error::InvalidConfiguration);
        }
        let mut entries = BTreeMap::new();
        for grant in grants {
            if !valid_service_dns(&grant.client_dns)
                || !valid_name(&grant.alias)
                || !valid_name(&grant.provider)
                || !valid_handle(&grant.handle)
                || !valid_operation(&grant.operation)
                || !valid_target(&grant.target)
                || grant.arguments_sha256.as_deref().is_some_and(|digest| !valid_revision(digest))
            {
                return Err(Error::InvalidConfiguration);
            }
            let revision = vault.revision(&grant.handle, &grant.provider)
                .map_err(|_| Error::InvalidConfiguration)?;
            if grant.revision.as_deref().is_some_and(|expected|
                !valid_revision(expected) || expected != revision)
            {
                return Err(Error::InvalidConfiguration);
            }
            let expires_at = grant.expires_at.as_deref().map(|value| {
                chrono::DateTime::parse_from_rfc3339(value)
                    .map(|time| time.timestamp())
                    .map_err(|_| Error::InvalidConfiguration)
            }).transpose()?;
            if expires_at.is_some_and(|deadline| deadline <= chrono::Utc::now().timestamp()) {
                return Err(Error::InvalidConfiguration);
            }
            let bound = BoundGrant {
                provider: grant.provider, handle: grant.handle, revision, expires_at,
                arguments_sha256: grant.arguments_sha256,
            };
            if entries.insert((grant.client_dns, grant.alias, grant.operation, grant.target), bound).is_some() {
                return Err(Error::InvalidConfiguration);
            }
        }
        Ok(Self { entries })
    }

    pub(crate) fn scoped(
        &self,
        peer: &VerifiedServiceIdentity,
        operation: &str,
        target: &str,
        arguments_sha256: &str,
        now: i64,
    ) -> Result<Vec<ScopedCredential>, Error> {
        let mut result = Vec::new();
        for ((dns, alias, allowed_operation, allowed_target), grant) in &self.entries {
            if dns == &peer.dns && allowed_operation == operation && allowed_target == target
                && grant.arguments_sha256.as_deref().is_none_or(|digest| digest == arguments_sha256)
                && grant.expires_at.is_none_or(|deadline| now < deadline)
            {
                result.push(ScopedCredential {
                    alias: alias.clone(),
                    revision: grant.revision.clone(),
                });
            }
        }
        if result.len() > 8 {
            return Err(Error::InvalidConfiguration);
        }
        Ok(result)
    }

    pub(crate) fn use_credential<T>(
        &self,
        peer: &VerifiedServiceIdentity,
        alias: &str,
        operation: &str,
        target: &str,
        arguments_sha256: &str,
        revision: &str,
        vault: &Vault,
        use_secret: impl FnOnce(&[u8]) -> T,
    ) -> Result<T, Error> {
        if !valid_name(alias) || !valid_revision(arguments_sha256)
            || !valid_revision(revision) {
            return Err(Error::Unauthorized);
        }
        let grant = self.entries
            .get(&(peer.dns.clone(), alias.into(), operation.into(), target.into()))
            .ok_or(Error::Unauthorized)?;
        if grant.revision != revision
            || grant.arguments_sha256.as_deref().is_some_and(|expected| expected != arguments_sha256)
            || grant.expires_at.is_some_and(|deadline| chrono::Utc::now().timestamp() >= deadline)
        {
            return Err(Error::Unauthorized);
        }
        vault.with_secret(&grant.handle, &grant.provider, Some(revision), use_secret)
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::state::StateStore;
    use std::{fs, os::unix::fs::PermissionsExt, path::PathBuf};

    fn private_dir() -> PathBuf {
        let mut id = [0u8; 12];
        getrandom::fill(&mut id).unwrap();
        let path = std::env::temp_dir().join(format!("security-grant-test-{}", hex::encode(id)));
        fs::create_dir(&path).unwrap();
        fs::set_permissions(&path, fs::Permissions::from_mode(0o700)).unwrap();
        path
    }

    #[test]
    fn service_dns_names_must_be_canonical() {
        for name in ["worker.internal", "executor-1.saturn.internal", "jupiter", "node09.lab"] {
            assert!(valid_service_dns(name), "valid peer identity rejected: {name}");
        }
        for name in ["", ".", ".worker", "worker.", "worker..internal", "-worker.internal",
            "worker-.internal", "worker.*", "*.internal", "WORKER.internal", "worker_internal",
            "https://worker.internal", "worker.internal:443", "127.0.0.1", "worker/internal",
            "white space.internal", "worker\\n.internal", "worker\\r.internal"] {
            assert!(!valid_service_dns(name), "invalid peer identity accepted: {name:?}");
        }
        assert!(!valid_service_dns(&format!("{}.internal", "a".repeat(64))));
        assert!(!valid_service_dns(&format!("{}.internal", "a".repeat(250))));
    }

    #[test]
    fn service_identity_and_alias_must_match_exactly() {
        let dir = private_dir();
        let mut state = StateStore::acquire(&dir).unwrap();
        let mut vault = Vault::open(&state, [b'k'; 32]).unwrap();
        let handle = vault.store(&mut state, "research", b"protected").unwrap();
        let policy = ServiceCredentialPolicy::new(vec![ServiceCredentialGrant {
            client_dns: "worker.internal".into(),
            alias: "provider_token".into(),
            provider: "research".into(),
            handle: handle.id.clone(),
            revision: None,
            expires_at: None,
        }], &vault).unwrap();
        let allowed = VerifiedServiceIdentity::from_authenticated_peer("worker.internal".into()).unwrap();
        let wrong = VerifiedServiceIdentity::from_authenticated_peer("other.internal".into()).unwrap();
        assert_eq!(
            policy.use_credential(&allowed, "provider_token", &vault, |s| s.to_vec()).unwrap(),
            b"protected"
        );
        assert!(matches!(
            policy.use_credential(&wrong, "provider_token", &vault, |_| ()),
            Err(Error::Unauthorized)
        ));
        assert!(matches!(
            policy.use_credential(&allowed, "provider_other", &vault, |_| ()),
            Err(Error::Unauthorized)
        ));
        assert!(matches!(
            ServiceCredentialPolicy::new(vec![ServiceCredentialGrant {
                client_dns: "worker.internal".into(),
                alias: "provider_token".into(),
                provider: "other".into(),
                handle: handle.id.clone(),
            revision: None,
            expires_at: None,
            }], &vault),
            Err(Error::InvalidConfiguration)
        ));
        let stale = ServiceCredentialGrant {
            client_dns: "worker.internal".into(),
            alias: "provider_token".into(),
            provider: "research".into(),
            handle: handle.id.clone(),
            revision: Some("0".repeat(64)),
            expires_at: None,
        };
        assert!(matches!(
            ServiceCredentialPolicy::new(vec![stale], &vault),
            Err(Error::InvalidConfiguration)
        ));
        let expired = ServiceCredentialGrant {
            client_dns: "worker.internal".into(),
            alias: "provider_token".into(),
            provider: "research".into(),
            handle: handle.id.clone(),
            revision: None,
            expires_at: Some("2020-01-01T00:00:00Z".into()),
        };
        assert!(matches!(
            ServiceCredentialPolicy::new(vec![expired], &vault),
            Err(Error::InvalidConfiguration)
        ));
        // Rotation under the same opaque handle invalidates the pinned grant.
        vault.rotate(&mut state, &handle.id, "research", b"replacement").unwrap();
        assert!(matches!(
            policy.use_credential(&allowed, "provider_token", &vault, |_| ()),
            Err(Error::Unauthorized)
        ));
        drop(vault);
        drop(state);
        fs::remove_dir_all(dir).unwrap();
    }
}
