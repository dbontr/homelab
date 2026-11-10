//! Grant one pinned mTLS service identity access to one credential handle.
use std::collections::BTreeMap;

use serde::{Deserialize, Serialize};

use crate::vault::Vault;
use crate::Error;

const MAX_GRANTS: usize = 128;

#[derive(Clone, Debug, Deserialize, Serialize)]
pub struct ServiceCredentialGrant {
    pub client_dns: String,
    pub alias: String,
    pub provider: String,
    pub handle: String,
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

fn valid_dns(name: &str) -> bool {
    !name.is_empty()
        && name.len() <= 253
        && !name.bytes().any(|c| {
            matches!(c, b'/' | b'?' | b'#' | b':' | b'@' | b'\\' | b' ' | b'\n' | b'\r' | b'\t' | 0)
        })
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
        if !valid_dns(&dns) {
            return Err(Error::Unauthorized);
        }
        Ok(Self { dns })
    }
}

struct BoundGrant {
    provider: String,
    handle: String,
    revision: String,
    expires_at: Option<i64>,
}

pub(crate) struct ServiceCredentialPolicy {
    entries: BTreeMap<(String, String), BoundGrant>,
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
            if !valid_dns(&grant.client_dns)
                || !valid_name(&grant.alias)
                || !valid_name(&grant.provider)
                || !valid_handle(&grant.handle)
            {
                return Err(Error::InvalidConfiguration);
            }
            let revision = vault.revision(&grant.handle, &grant.provider)
                .map_err(|_| Error::InvalidConfiguration)?;
            if grant.revision.as_deref().is_some_and(|expected| expected != revision)
                || (grant.revision.is_some() && !valid_revision(&revision))
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
            };
            if entries.insert((grant.client_dns, grant.alias), bound).is_some() {
                return Err(Error::InvalidConfiguration);
            }
        }
        Ok(Self { entries })
    }

    pub(crate) fn use_credential<T>(
        &self,
        peer: &VerifiedServiceIdentity,
        alias: &str,
        vault: &Vault,
        use_secret: impl FnOnce(&[u8]) -> T,
    ) -> Result<T, Error> {
        if !valid_name(alias) {
            return Err(Error::Unauthorized);
        }
        let grant = self
            .entries
            .get(&(peer.dns.clone(), alias.into()))
            .ok_or(Error::Unauthorized)?;
        if grant.expires_at.is_some_and(|deadline| chrono::Utc::now().timestamp() >= deadline) {
            return Err(Error::Unauthorized);
        }
        vault.with_secret(&grant.handle, &grant.provider, Some(&grant.revision), use_secret)
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
        drop(vault);
        drop(state);
        fs::remove_dir_all(dir).unwrap();
    }
}
