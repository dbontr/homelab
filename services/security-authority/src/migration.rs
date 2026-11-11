//! Explicit, single-writer security authority selection and state snapshots.
//!
//! The Go and Rust authorities retain the same version-one disk formats.
//! Rollback selects a previous executable against the current state; an
//! archived snapshot is never copied over active replay or vault records.

use std::fs::{self, DirBuilder, File, OpenOptions};
use std::io::{Read, Write};
use std::os::unix::fs::{DirBuilderExt, MetadataExt, OpenOptionsExt};
use std::path::{Component, Path};
use std::process::Command;

use serde::{Deserialize, Serialize};
use sha2::{Digest, Sha256};

use crate::state::StateStore;
use crate::Error;

const AUTH_NAME: &str = "authenticator.json";
const VAULT_NAME: &str = "credentials.json";
const MARKER_NAME: &str = ".authority-selection.json";
const RECEIPT_NAME: &str = "authority-cutover.json";
const AUTH_LIMIT: usize = (16 << 10) - 1;
const VAULT_LIMIT: usize = 8 << 20;
const RECEIPT_LIMIT: usize = 4096;

#[derive(Clone, Deserialize, Serialize)]
#[serde(deny_unknown_fields)]
struct Receipt {
    version: u8,
    target: String,
    authenticator_sha256: String,
    vault_sha256: Option<String>,
    parent_marker_sha256: Option<String>,
    nonce: String,
}

#[derive(Deserialize, Serialize)]
#[serde(deny_unknown_fields)]
struct Selection {
    version: u8,
    target: String,
    receipt_sha256: String,
}

fn sha(bytes: &[u8]) -> String {
    hex::encode(Sha256::digest(bytes))
}

fn digest(value: &str, bytes: usize) -> bool {
    value.len() == bytes * 2 && value.bytes()
        .all(|c| c.is_ascii_digit() || (b'a'..=b'f').contains(&c))
}

fn target_ok(target: &str) -> bool {
    matches!(target, "go" | "rust")
}

fn protected(path: &Path, limit: usize) -> Result<Option<Vec<u8>>, Error> {
    let mut file = match OpenOptions::new().read(true)
        .custom_flags(libc::O_NOFOLLOW | libc::O_CLOEXEC).open(path)
    {
        Ok(file) => file,
        Err(error) if error.kind() == std::io::ErrorKind::NotFound => return Ok(None),
        Err(_) => return Err(Error::Storage),
    };
    let metadata = file.metadata().map_err(|_| Error::Storage)?;
    if !metadata.is_file() || metadata.nlink() != 1
        || metadata.mode() & 0o077 != 0
        || metadata.uid() != unsafe { libc::geteuid() }
    {
        return Err(Error::Storage);
    }
    let mut bytes = Vec::new();
    file.take((limit + 1) as u64).read_to_end(&mut bytes)
        .map_err(|_| Error::Storage)?;
    if bytes.is_empty() || bytes.len() > limit {
        return Err(Error::Storage);
    }
    Ok(Some(bytes))
}

fn required_file(dir: &Path, name: &str, limit: usize) -> Result<Vec<u8>, Error> {
    protected(&dir.join(name), limit)?.ok_or(Error::Storage)
}

fn read_receipt(raw: &[u8], target: &str) -> Result<Receipt, Error> {
    if raw.is_empty() || raw.len() > RECEIPT_LIMIT || !target_ok(target) {
        return Err(Error::InvalidConfiguration);
    }
    let parsed: Receipt = serde_json::from_slice(raw)
        .map_err(|_| Error::InvalidConfiguration)?;
    if parsed.version != 1 || parsed.target != target
        || !digest(&parsed.authenticator_sha256, 32)
        || parsed.vault_sha256.as_deref()
            .is_some_and(|v| !digest(v, 32))
        || parsed.parent_marker_sha256.as_deref()
            .is_some_and(|v| !digest(v, 32))
        || !digest(&parsed.nonce, 16)
    {
        return Err(Error::InvalidConfiguration);
    }
    Ok(parsed)
}

fn current_hashes(state: &Path) -> Result<(String, Option<String>), Error> {
    let replay = required_file(state, AUTH_NAME, AUTH_LIMIT)?;
    let vault = protected(&state.join(VAULT_NAME), VAULT_LIMIT)?;
    Ok((sha(&replay), vault.map(|bytes| sha(&bytes))))
}

fn selection(state: &Path) -> Result<Option<(Selection, String)>, Error> {
    let Some(bytes) = protected(&state.join(MARKER_NAME), RECEIPT_LIMIT)? else {
        return Ok(None);
    };
    let marker: Selection = serde_json::from_slice(&bytes).map_err(|_| Error::Storage)?;
    if marker.version != 1 || !target_ok(&marker.target)
        || !digest(&marker.receipt_sha256, 32)
    {
        return Err(Error::Storage);
    }
    Ok(Some((marker, sha(&bytes))))
}

fn verify_live(state: &Path, receipt: &Receipt) -> Result<(), Error> {
    let (auth, vault) = current_hashes(state)?;
    if receipt.authenticator_sha256 != auth || receipt.vault_sha256 != vault {
        return Err(Error::Storage);
    }
    Ok(())
}

fn write_exclusive(path: &Path, bytes: &[u8]) -> Result<(), Error> {
    let mut file = OpenOptions::new().write(true).create_new(true)
        .mode(0o600)
        .custom_flags(libc::O_NOFOLLOW | libc::O_CLOEXEC)
        .open(path).map_err(|_| Error::Storage)?;
    file.write_all(bytes).and_then(|()| file.sync_all())
        .map_err(|_| Error::Storage)
}

fn publish_selection(state: &Path, target: &str, receipt_hash: &str) -> Result<(), Error> {
    let marker = serde_json::to_vec(&Selection {
        version: 1, target: target.into(), receipt_sha256: receipt_hash.into(),
    }).map_err(|_| Error::Storage)?;
    let mut nonce = [0u8; 16];
    getrandom::fill(&mut nonce).map_err(|_| Error::Storage)?;
    let temporary = state.join(format!(".selection-{}", hex::encode(nonce)));
    let result = (|| {
        write_exclusive(&temporary, &marker)?;
        fs::rename(&temporary, state.join(MARKER_NAME)).map_err(|_| Error::Storage)?;
        File::open(state).and_then(|dir| dir.sync_all()).map_err(|_| Error::Storage)
    })();
    let _ = fs::remove_file(temporary);
    result
}

/// Called only after the selected authority holds the shared state lease and
/// has parsed both current authentication state and encrypted vault contents.
/// A receipt remains usable across restarts of the same selected authority,
/// but an implementation transition requires a newly prepared parent-bound
/// receipt matching the exact current state.
pub fn admit(state: &Path, target: &str, raw: Option<&[u8]>) -> Result<(), Error> {
    if !target_ok(target) {
        return Err(Error::InvalidConfiguration);
    }
    let previous = selection(state)?;
    let Some(raw) = raw else {
        // An untouched Go installation has no migration state. Once a
        // selection marker exists, neither authority can bypass the gate.
        return if target == "go" && previous.is_none() {
            Ok(())
        } else {
            Err(Error::InvalidConfiguration)
        };
    };
    let receipt = read_receipt(raw, target)?;
    let receipt_hash = sha(raw);
    if let Some((marker, _)) = &previous {
        if marker.target == target && marker.receipt_sha256 == receipt_hash {
            // The persisted state may legitimately advance after selection.
            // Runtime parsers still reject incompatible state versions.
            return Ok(());
        }
    }
    let parent = previous.as_ref().map(|(_, marker_hash)| marker_hash.clone());
    if parent != receipt.parent_marker_sha256 {
        return Err(Error::InvalidConfiguration);
    }
    verify_live(state, &receipt)?;
    publish_selection(state, target, &receipt_hash)
}

/// Capture a protected, independently owned snapshot under the same flock as
/// both authority implementations. No source-state contents are modified.
/// The sibling security-preflight binary must accept the copied state and
/// selected runtime credentials before an approval receipt is emitted.
pub fn prepare(state: &Path, destination: &Path, target: &str) -> Result<(), Error> {
    if !target_ok(target) || !state.is_absolute() || !destination.is_absolute()
        || state.components().any(|c| c == Component::ParentDir)
        || destination.components().any(|c| c == Component::ParentDir)
        || state == destination || state.starts_with(destination)
        || destination.starts_with(state)
    {
        return Err(Error::InvalidConfiguration);
    }
    let store = StateStore::acquire(state)?;
    store.load_authenticator()?;
    let auth = required_file(state, AUTH_NAME, AUTH_LIMIT)?;
    let vault = protected(&state.join(VAULT_NAME), VAULT_LIMIT)?;
    let parent = selection(state)?.map(|(_, fingerprint)| fingerprint);
    let parent_dir = destination.parent().ok_or(Error::InvalidConfiguration)?;
    let parent_md = fs::symlink_metadata(parent_dir).map_err(|_| Error::Storage)?;
    if !parent_md.is_dir() || parent_md.file_type().is_symlink()
        || fs::canonicalize(parent_dir).map_err(|_| Error::Storage)? != parent_dir
        || parent_md.mode() & 0o022 != 0
    {
        return Err(Error::Storage);
    }
    DirBuilder::new().mode(0o700).create(destination).map_err(|_| Error::Storage)?;
    let result = (|| {
        write_exclusive(&destination.join(AUTH_NAME), &auth)?;
        if let Some(vault) = &vault {
            write_exclusive(&destination.join(VAULT_NAME), vault)?;
        }
        File::open(destination).and_then(|dir| dir.sync_all())
            .map_err(|_| Error::Storage)?;
        let executable = std::env::current_exe().map_err(|_| Error::Storage)?;
        let preflight = executable.with_file_name("security-preflight");
        let status = Command::new(preflight).arg("--state-dir").arg(destination)
            .status().map_err(|_| Error::Storage)?;
        if !status.success() {
            return Err(Error::InvalidConfiguration);
        }
        if target == "go" {
            // A rollback receipt is issued only after both independent
            // implementations accept the exact copied replay and vault state.
            let binary = std::env::var("HOMELAB_SECURITY_GO_PREFLIGHT")
                .map_err(|_| Error::InvalidConfiguration)?;
            let binary = Path::new(&binary);
            if !binary.is_absolute()
                || binary.components().any(|c| c == Component::ParentDir)
            {
                return Err(Error::InvalidConfiguration);
            }
            let md = fs::symlink_metadata(binary).map_err(|_| Error::Storage)?;
            if !md.is_file() || md.file_type().is_symlink()
                || md.mode() & 0o022 != 0
                || !matches!(md.uid(), 0 if unsafe { libc::geteuid() } != 0)
                    && md.uid() != unsafe { libc::geteuid() }
            {
                return Err(Error::Storage);
            }
            let outcome = Command::new(binary)
                .arg("--state-dir").arg(destination)
                .status().map_err(|_| Error::Storage)?;
            if !outcome.success() {
                return Err(Error::InvalidConfiguration);
            }
        }
        let mut nonce = [0u8; 16];
        getrandom::fill(&mut nonce).map_err(|_| Error::Storage)?;
        let receipt = serde_json::to_vec(&Receipt {
            version: 1,
            target: target.into(),
            authenticator_sha256: sha(&auth),
            vault_sha256: vault.as_ref().map(|bytes| sha(bytes)),
            parent_marker_sha256: parent,
            nonce: hex::encode(nonce),
        }).map_err(|_| Error::Storage)?;
        if receipt.len() > RECEIPT_LIMIT {
            return Err(Error::Storage);
        }
        write_exclusive(&destination.join(RECEIPT_NAME), &receipt)?;
        File::open(destination).and_then(|dir| dir.sync_all())
            .map_err(|_| Error::Storage)?;
        Ok(())
    })();
    if result.is_err() {
        // Only this process could have created the previously absent path.
        // Remove incomplete copies containing protected material.
        let _ = fs::remove_dir_all(destination);
    }
    result
}

#[cfg(test)]
mod tests {
    use super::*;
    use std::os::unix::fs::PermissionsExt;

    fn state() -> std::path::PathBuf {
        let mut id = [0u8; 12];
        getrandom::fill(&mut id).unwrap();
        let path = std::env::temp_dir().join(format!("authority-migration-{}", hex::encode(id)));
        fs::create_dir(&path).unwrap();
        fs::set_permissions(&path, fs::Permissions::from_mode(0o700)).unwrap();
        let auth = br#"{"version":1,"last_totp_step":12,"failures":0,"locked_until":"0001-01-01T00:00:00Z"}"#;
        fs::write(path.join(AUTH_NAME), auth).unwrap();
        fs::set_permissions(path.join(AUTH_NAME), fs::Permissions::from_mode(0o600)).unwrap();
        path
    }

    fn receipt(state: &Path, target: &str, parent: Option<String>) -> Vec<u8> {
        let (auth, vault) = current_hashes(state).unwrap();
        serde_json::to_vec(&Receipt {
            version: 1, target: target.into(), authenticator_sha256: auth,
            vault_sha256: vault, parent_marker_sha256: parent, nonce: "a".repeat(32),
        }).unwrap()
    }

    #[test]
    fn selection_requires_parent_bound_receipts_and_preserves_live_state() {
        let dir = state();
        assert!(admit(&dir, "rust", None).is_err());
        assert!(admit(&dir, "go", None).is_ok());
        let initial = receipt(&dir, "rust", None);
        admit(&dir, "rust", Some(&initial)).unwrap();
        let fingerprint = selection(&dir).unwrap().unwrap().1;
        let auth = required_file(&dir, AUTH_NAME, AUTH_LIMIT).unwrap();
        assert!(admit(&dir, "go", Some(&initial)).is_err());
        let rollback = receipt(&dir, "go", Some(fingerprint.clone()));
        admit(&dir, "go", Some(&rollback)).unwrap();
        assert!(admit(&dir, "go", Some(&rollback)).is_ok());
        assert!(admit(&dir, "rust", Some(&initial)).is_err());
        assert_eq!(required_file(&dir, AUTH_NAME, AUTH_LIMIT).unwrap(), auth);
        assert!(admit(&dir, "rust", Some(&receipt(&dir, "rust", None))).is_err());
        let next = receipt(&dir, "rust", selection(&dir).unwrap().map(|(_,hash)|hash));
        admit(&dir, "rust", Some(&next)).unwrap();
        fs::remove_dir_all(dir).unwrap();
    }

    #[test]
    fn state_drift_invalidates_transition_but_not_selected_restart() {
        let dir = state();
        let initial = receipt(&dir, "rust", None);
        fs::write(dir.join(AUTH_NAME), b"{}").unwrap();
        assert!(admit(&dir, "rust", Some(&initial)).is_err());
        fs::write(dir.join(AUTH_NAME), br#"{"version":1}"#).unwrap();
        let current = receipt(&dir, "rust", None);
        admit(&dir, "rust", Some(&current)).unwrap();
        assert!(admit(&dir, "rust", Some(&current)).is_ok());
        assert!(admit(&dir, "go", Some(&receipt(&dir, "go", None))).is_err());
        fs::remove_dir_all(dir).unwrap();
    }

    #[test]
    fn rejects_unsafe_receipts_and_symlinked_markers() {
        let dir = state();
        let receipt = receipt(&dir, "rust", None);
        let mut tampered: Receipt = serde_json::from_slice(&receipt).unwrap();
        tampered.nonce = "INVALID".into();
        let bytes = serde_json::to_vec(&tampered).unwrap();
        assert!(admit(&dir, "rust", Some(&bytes)).is_err());
        let other = dir.join("untrusted");
        fs::write(&other, b"{}").unwrap();
        std::os::unix::fs::symlink(&other, dir.join(MARKER_NAME)).unwrap();
        assert!(admit(&dir, "rust", Some(&receipt)).is_err());
        fs::remove_dir_all(dir).unwrap();
    }
}
