//! Encrypted credentials in the existing AES-256-GCM vault representation.
use std::collections::BTreeMap;
use aes_gcm::aead::{Aead, KeyInit, Payload};
use aes_gcm::{Aes256Gcm, Nonce};
use base64::{engine::general_purpose::{STANDARD_NO_PAD, URL_SAFE_NO_PAD}, Engine};
use serde::{Deserialize, Serialize};
use sha2::{Digest, Sha256};
use zeroize::{Zeroize, Zeroizing};

use crate::state::StateStore;
use crate::Error;

const MAX_CREDENTIALS: usize = 1024;
const MAX_SECRET_BYTES: usize = 32768;

#[derive(Clone, Debug, PartialEq, Eq, Serialize)]
pub struct CredentialHandle {
    pub id: String,
    pub provider: String,
    pub created_at: String,
}

#[derive(Clone, Deserialize, Serialize)]
#[serde(deny_unknown_fields)]
struct EncryptedCredential {
    id: String,
    provider: String,
    created_at: String,
    nonce: String,
    data: String,
}

pub struct Vault {
    key: [u8; 32],
    records: BTreeMap<String, EncryptedCredential>,
}

impl Drop for Vault {
    fn drop(&mut self) {
        self.key.zeroize();
    }
}

fn valid_provider(provider: &str) -> bool {
    !provider.is_empty()
        && provider.len() <= 64
        && !provider.chars().any(|c| matches!(c, '\r' | '\n' | '\t' | '/' | ':' | '\\'))
}

fn checked_secret(secret: &[u8]) -> Result<(), Error> {
    if secret.is_empty() || secret.len() > MAX_SECRET_BYTES {
        return Err(Error::InvalidArguments);
    }
    Ok(())
}

fn record_revision(record: &EncryptedCredential) -> String {
    hex::encode(Sha256::digest(format!(
        "{}:{}:{}:{}",
        record.provider, record.id, record.nonce, record.data
    )))
}

fn decrypt(key: &[u8; 32], record: &EncryptedCredential) -> Result<Vec<u8>, Error> {
    let nonce = STANDARD_NO_PAD
        .decode(record.nonce.as_bytes())
        .map_err(|_| Error::Storage)?;
    if nonce.len() != 12 {
        return Err(Error::Storage);
    }
    let data = STANDARD_NO_PAD
        .decode(record.data.as_bytes())
        .map_err(|_| Error::Storage)?;
    let cipher = Aes256Gcm::new_from_slice(key).map_err(|_| Error::Storage)?;
    cipher
        .decrypt(
            Nonce::from_slice(&nonce),
            Payload {
                msg: &data,
                aad: format!("{}:{}", record.provider, record.id).as_bytes(),
            },
        )
        .map_err(|_| Error::Storage)
}

impl Vault {
    pub(crate) fn open(store: &StateStore, key: [u8; 32]) -> Result<Self, Error> {
        let protected_key = Zeroizing::new(key);
        let Some(bytes) = store.load_vault()? else {
            return Ok(Self {
                key: *protected_key,
                records: BTreeMap::new(),
            });
        };
        let records: BTreeMap<String, EncryptedCredential> =
            serde_json::from_slice(&bytes).map_err(|_| Error::Storage)?;
        if records.len() > MAX_CREDENTIALS {
            return Err(Error::Storage);
        }
        for (id, record) in &records {
            if id != &record.id
                || id.len() != 32
                || URL_SAFE_NO_PAD.decode(id).map_or(true, |raw| raw.len() != 24)
                || !valid_provider(&record.provider)
                || chrono::DateTime::parse_from_rfc3339(&record.created_at).is_err()
                || record.nonce.is_empty()
                || record.data.is_empty()
            {
                return Err(Error::Storage);
            }
            // Authentication tags are checked when the credential is used.
        }
        Ok(Self { key: *protected_key, records })
    }

    fn seal(&self, provider: &str, id: &str, secret: &[u8], created_at: String)
        -> Result<EncryptedCredential, Error>
    {
        checked_secret(secret)?;
        let mut nonce = [0u8; 12];
        getrandom::fill(&mut nonce).map_err(|_| Error::Storage)?;
        let cipher = Aes256Gcm::new_from_slice(&self.key).map_err(|_| Error::Storage)?;
        let aad = format!("{provider}:{id}");
        let ciphertext = cipher
            .encrypt(Nonce::from_slice(&nonce), Payload { msg: secret, aad: aad.as_bytes() })
            .map_err(|_| Error::Storage)?;
        Ok(EncryptedCredential {
            id: id.into(),
            provider: provider.into(),
            created_at,
            nonce: STANDARD_NO_PAD.encode(nonce),
            data: STANDARD_NO_PAD.encode(ciphertext),
        })
    }

    fn persist(&mut self, store: &mut StateStore, records: BTreeMap<String, EncryptedCredential>)
        -> Result<(), Error>
    {
        let bytes = serde_json::to_vec(&records).map_err(|_| Error::Storage)?;
        store.persist_vault(&bytes)?;
        self.records = records;
        Ok(())
    }

    pub(crate) fn store(&mut self, storage: &mut StateStore, provider: &str, secret: &[u8])
        -> Result<CredentialHandle, Error>
    {
        if !valid_provider(provider) {
            return Err(Error::InvalidArguments);
        }
        checked_secret(secret)?;
        if self.records.len() >= MAX_CREDENTIALS {
            return Err(Error::Storage);
        }
        let mut random = [0u8; 24];
        getrandom::fill(&mut random).map_err(|_| Error::Storage)?;
        let id = URL_SAFE_NO_PAD.encode(random);
        let created_at = chrono::Utc::now().to_rfc3339_opts(chrono::SecondsFormat::Nanos, true);
        let record = self.seal(provider, &id, secret, created_at)?;
        let handle = CredentialHandle {
            id: id.clone(),
            provider: provider.into(),
            created_at: record.created_at.clone(),
        };
        let mut records = self.records.clone();
        records.insert(id, record);
        self.persist(storage, records)?;
        Ok(handle)
    }

    pub(crate) fn rotate(&mut self, storage: &mut StateStore, id: &str, provider: &str, secret: &[u8])
        -> Result<(), Error>
    {
        self.rotate_checked(storage, id, provider, None, secret)
    }

    /// A provider response cannot overwrite a credential that was revoked or
    /// rotated while the remote request was in flight.
    pub(crate) fn rotate_if_revision(
        &mut self, storage: &mut StateStore, id: &str, provider: &str,
        expected_revision: &str, secret: &[u8]
    ) -> Result<(), Error> {
        self.rotate_checked(storage, id, provider, Some(expected_revision), secret)
    }

    fn rotate_checked(
        &mut self, storage: &mut StateStore, id: &str, provider: &str,
        expected_revision: Option<&str>, secret: &[u8]
    ) -> Result<(), Error> {
        checked_secret(secret)?;
        let existing = self.records.get(id).ok_or(Error::Unauthorized)?;
        if existing.provider != provider
            || expected_revision.is_some_and(|revision| revision != record_revision(existing))
        {
            return Err(Error::Unauthorized);
        }
        let replacement = self.seal(provider, id, secret, existing.created_at.clone())?;
        let mut records = self.records.clone();
        records.insert(id.into(), replacement);
        self.persist(storage, records)
    }

    pub(crate) fn handle(&self, id: &str, provider: &str) -> Result<CredentialHandle, Error> {
        let record = self.records.get(id).ok_or(Error::Unauthorized)?;
        if record.provider != provider { return Err(Error::Unauthorized); }
        Ok(CredentialHandle {
            id: record.id.clone(),
            provider: record.provider.clone(),
            created_at: record.created_at.clone(),
        })
    }

    pub(crate) fn delete(&mut self, storage: &mut StateStore, id: &str) -> Result<(), Error> {
        if !self.records.contains_key(id) {
            return Err(Error::Unauthorized);
        }
        let mut records = self.records.clone();
        records.remove(id);
        self.persist(storage, records)
    }

    pub(crate) fn revision(&self, id: &str, provider: &str) -> Result<String, Error> {
        let record = self.records.get(id).ok_or(Error::Unauthorized)?;
        if record.provider != provider {
            return Err(Error::Unauthorized);
        }
        Ok(record_revision(record))
    }

    pub(crate) fn with_secret<R>(
        &self,
        id: &str,
        provider: &str,
        revision: Option<&str>,
        use_secret: impl FnOnce(&[u8]) -> R,
    ) -> Result<R, Error> {
        let record = self.records.get(id).ok_or(Error::Unauthorized)?;
        if record.provider != provider
            || revision.is_some_and(|expected| expected != record_revision(record))
        {
            return Err(Error::Unauthorized);
        }
        // Drop zeroizes even if the trusted callback unwinds.
        let secret = Zeroizing::new(decrypt(&self.key, record)?);
        Ok(use_secret(&secret))
    }

    /// Validate every encrypted record before admitting a migrated vault.
    pub(crate) fn validate_all(&self) -> Result<(), Error> {
        for record in self.records.values() {
            let secret = Zeroizing::new(decrypt(&self.key, record)?);
            checked_secret(&secret).map_err(|_| Error::Storage)?;
        }
        Ok(())
    }

    pub(crate) fn handles(&self) -> Vec<CredentialHandle> {
        self.records.values().map(|record| CredentialHandle {
            id: record.id.clone(),
            provider: record.provider.clone(),
            created_at: record.created_at.clone(),
        }).collect()
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use std::{fs, os::unix::fs::PermissionsExt, path::PathBuf};
    fn private_dir() -> PathBuf {
        let mut id = [0u8; 12];
        getrandom::fill(&mut id).unwrap();
        let path = std::env::temp_dir().join(format!("security-vault-test-{}", hex::encode(id)));
        fs::create_dir(&path).unwrap();
        fs::set_permissions(&path, fs::Permissions::from_mode(0o700)).unwrap();
        path
    }

    #[test]
    fn decrypts_go_aes_gcm_record() {
        let dir = private_dir();
        let record = include_str!("../tests/fixtures/vault-records.json");
        fs::write(dir.join("credentials.json"), record).unwrap();
        let mut permissions = fs::metadata(dir.join("credentials.json")).unwrap().permissions();
        permissions.set_mode(0o600);
        fs::set_permissions(dir.join("credentials.json"), permissions).unwrap();
        let storage = StateStore::acquire(&dir).unwrap();
        let vault = Vault::open(&storage, [b'k'; 32]).unwrap();
        let got = vault.with_secret("abcdefghijklmnopqrstuvwxABCDEFGH", "research", None,
            |secret| secret.to_vec()).unwrap();
        assert_eq!(got, b"fixture-credential");
        drop(vault);
        drop(storage);
        fs::remove_dir_all(dir).unwrap();
    }

    #[test]
    fn stale_refresh_cannot_replace_rotated_or_revoked_credential() {
        let dir = private_dir();
        let mut storage = StateStore::acquire(&dir).unwrap();
        let mut vault = Vault::open(&storage, [b'k'; 32]).unwrap();
        let handle = vault.store(&mut storage, "vendor", b"initial").unwrap();
        let snapshot = vault.revision(&handle.id, "vendor").unwrap();
        vault.rotate(&mut storage, &handle.id, "vendor", b"rotated").unwrap();
        assert!(matches!(
            vault.rotate_if_revision(&mut storage, &handle.id, "vendor", &snapshot, b"stale"),
            Err(Error::Unauthorized)
        ));
        let read = vault.with_secret(&handle.id, "vendor", None, |v| v.to_vec()).unwrap();
        assert_eq!(read, b"rotated");
        let current = vault.revision(&handle.id, "vendor").unwrap();
        vault.rotate_if_revision(&mut storage, &handle.id, "vendor", &current, b"fresh").unwrap();
        assert_eq!(vault.with_secret(&handle.id, "vendor", None, |v| v.to_vec()).unwrap(), b"fresh");
        let last = vault.revision(&handle.id, "vendor").unwrap();
        vault.delete(&mut storage, &handle.id).unwrap();
        assert!(matches!(
            vault.rotate_if_revision(&mut storage, &handle.id, "vendor", &last, b"resurrected"),
            Err(Error::Unauthorized)
        ));
        drop(vault);
        drop(storage);
        fs::remove_dir_all(dir).unwrap();
    }

    #[test]
    fn rotation_invalidates_previous_credential_revision() {
        let dir = private_dir();
        let mut storage = StateStore::acquire(&dir).unwrap();
        let mut vault = Vault::open(&storage, [b'k'; 32]).unwrap();
        let handle = vault.store(&mut storage, "research", b"first").unwrap();
        let old_revision = vault.revision(&handle.id, "research").unwrap();
        vault.rotate(&mut storage, &handle.id, "research", b"second").unwrap();
        assert!(matches!(
            vault.with_secret(&handle.id, "research", Some(&old_revision), |_| ()),
            Err(Error::Unauthorized)
        ));
        let secret = vault.with_secret(&handle.id, "research", None, |value| value.to_vec()).unwrap();
        assert_eq!(secret, b"second");
        drop(vault);
        let reopened = Vault::open(&storage, [b'k'; 32]).unwrap();
        assert_eq!(reopened.handles().len(), 1);
        drop(reopened);
        drop(storage);
        fs::remove_dir_all(dir).unwrap();
    }
}
