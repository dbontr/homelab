//! Single-writer, crash-resistant authentication replay state.
use std::fs::{self, File, OpenOptions};
use std::io::{Read, Write};
use std::os::unix::fs::{MetadataExt, OpenOptionsExt, PermissionsExt};
use std::path::{Path, PathBuf};

use chrono::{DateTime, SecondsFormat, Utc};
use fs2::FileExt;
use serde::{Deserialize, Serialize};

use crate::credentials::AuthenticatorState;
use crate::Error;

const MAX_STATE_BYTES: usize = 16 << 10;
const MAX_VAULT_BYTES: usize = 8 << 20;
const VAULT_NAME: &str = "credentials.json";
const LOCK_NAME: &str = ".securityd.lock";
const STATE_NAME: &str = "authenticator.json";

#[derive(Serialize, Deserialize)]
#[serde(deny_unknown_fields)]
struct PersistedAuthenticator {
    version: u8,
    last_totp_step: i64,
    failures: u8,
    locked_until: String,
}

fn owner_only(metadata: &fs::Metadata) -> bool {
    metadata.is_file()
        && metadata.mode() & 0o077 == 0
        && metadata.uid() == unsafe { libc::geteuid() }
}

fn protected_read(path: &Path, limit: usize) -> Result<Option<Vec<u8>>, Error> {
    let mut file = match OpenOptions::new()
        .read(true)
        .custom_flags(libc::O_NOFOLLOW | libc::O_CLOEXEC)
        .open(path)
    {
        Ok(file) => file,
        Err(e) if e.kind() == std::io::ErrorKind::NotFound => return Ok(None),
        Err(_) => return Err(Error::Storage),
    };
    if !owner_only(&file.metadata().map_err(|_| Error::Storage)?) {
        return Err(Error::Storage);
    }
    let mut bytes = Vec::new();
    file.take((limit + 1) as u64)
        .read_to_end(&mut bytes)
        .map_err(|_| Error::Storage)?;
    if bytes.len() > limit {
        return Err(Error::Storage);
    }
    Ok(Some(bytes))
}

fn decode_state(raw: Option<Vec<u8>>) -> Result<AuthenticatorState, Error> {
    let Some(raw) = raw else {
        return Ok(AuthenticatorState::default());
    };
    let parsed: PersistedAuthenticator =
        serde_json::from_slice(&raw).map_err(|_| Error::Storage)?;
    if parsed.version != 1 || parsed.last_totp_step < -1 || parsed.failures > 4 {
        return Err(Error::Storage);
    }
    let locked = DateTime::parse_from_rfc3339(&parsed.locked_until)
        .map_err(|_| Error::Storage)?
        .timestamp()
        .max(0);
    Ok(AuthenticatorState {
        last_totp_step: parsed.last_totp_step,
        failures: parsed.failures,
        locked_until: locked,
    })
}

fn encode_state(state: &AuthenticatorState) -> Result<Vec<u8>, Error> {
    if state.last_totp_step < -1 || state.failures > 4 {
        return Err(Error::Storage);
    }
    let locked_until = if state.locked_until == 0 {
        "0001-01-01T00:00:00Z".into()
    } else {
        DateTime::<Utc>::from_timestamp(state.locked_until, 0)
            .ok_or(Error::Storage)?
            .to_rfc3339_opts(SecondsFormat::Secs, true)
    };
    serde_json::to_vec(&PersistedAuthenticator {
        version: 1,
        last_totp_step: state.last_totp_step,
        failures: state.failures,
        locked_until,
    })
    .map_err(|_| Error::Storage)
}

pub struct StateStore {
    directory: PathBuf,
    _lease: File,
    faulted: bool,
}

impl StateStore {
    pub fn acquire(directory: &Path) -> Result<Self, Error> {
        if !directory.is_absolute()
            || directory.components().any(|c| matches!(c, std::path::Component::ParentDir))
        {
            return Err(Error::InvalidConfiguration);
        }
        let md = fs::symlink_metadata(directory).map_err(|_| Error::Storage)?;
        if !md.is_dir() || md.file_type().is_symlink()
            || md.mode() & 0o077 != 0
            || md.uid() != unsafe { libc::geteuid() }
        {
            return Err(Error::Storage);
        }
        let lease = OpenOptions::new()
            .read(true)
            .write(true)
            .create(true)
            .mode(0o600)
            .custom_flags(libc::O_NOFOLLOW | libc::O_CLOEXEC)
            .open(directory.join(LOCK_NAME))
            .map_err(|_| Error::Storage)?;
        if !owner_only(&lease.metadata().map_err(|_| Error::Storage)?) {
            return Err(Error::Storage);
        }
        FileExt::try_lock_exclusive(&lease).map_err(|_| Error::Storage)?;
        Ok(Self {
            directory: directory.to_owned(),
            _lease: lease,
            faulted: false,
        })
    }

    pub fn load_authenticator(&self) -> Result<AuthenticatorState, Error> {
        if self.faulted {
            return Err(Error::Storage);
        }
        decode_state(protected_read(&self.directory.join(STATE_NAME), MAX_STATE_BYTES)?)
    }

    pub fn persist_authenticator(&mut self, state: &AuthenticatorState) -> Result<(), Error> {
        if self.faulted {
            return Err(Error::Storage);
        }
        let payload = encode_state(state)?;
        if self.write_atomically(STATE_NAME, &payload, MAX_STATE_BYTES).is_err() {
            self.faulted = true;
            return Err(Error::Storage);
        }
        Ok(())
    }

    pub(crate) fn load_vault(&self) -> Result<Option<Vec<u8>>, Error> {
        if self.faulted {
            return Err(Error::Storage);
        }
        protected_read(&self.directory.join(VAULT_NAME), MAX_VAULT_BYTES)
    }

    pub(crate) fn persist_vault(&mut self, payload: &[u8]) -> Result<(), Error> {
        if self.faulted {
            return Err(Error::Storage);
        }
        if self.write_atomically(VAULT_NAME, payload, MAX_VAULT_BYTES).is_err() {
            self.faulted = true;
            return Err(Error::Storage);
        }
        Ok(())
    }

    fn write_atomically(&self, name: &str, payload: &[u8], limit: usize) -> Result<(), Error> {
        if payload.len() > limit || name.contains('/') {
            return Err(Error::Storage);
        }
        let mut nonce = [0u8; 16];
        getrandom::fill(&mut nonce).map_err(|_| Error::Storage)?;
        let temporary = self.directory.join(format!(".auth-state-{}", hex::encode(nonce)));
        let result = (|| {
            let mut file = OpenOptions::new()
                .write(true)
                .create_new(true)
                .mode(0o600)
                .custom_flags(libc::O_NOFOLLOW | libc::O_CLOEXEC)
                .open(&temporary)
                .map_err(|_| Error::Storage)?;
            file.write_all(payload).map_err(|_| Error::Storage)?;
            file.sync_all().map_err(|_| Error::Storage)?;
            fs::rename(&temporary, self.directory.join(name)).map_err(|_| Error::Storage)?;
            File::open(&self.directory)
                .and_then(|dir| dir.sync_all())
                .map_err(|_| Error::Storage)?;
            Ok(())
        })();
        let _ = fs::remove_file(temporary);
        result
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    fn dir() -> PathBuf {
        let mut id = [0u8; 16];
        getrandom::fill(&mut id).unwrap();
        let path = std::env::temp_dir().join(format!("security-authority-test-{}", hex::encode(id)));
        fs::create_dir(&path).unwrap();
        fs::set_permissions(&path, fs::Permissions::from_mode(0o700)).unwrap();
        path
    }

    #[test]
    fn exclusive_lease_and_go_authenticator_format() {
        let directory = dir();
        let mut store = StateStore::acquire(&directory).unwrap();
        assert!(matches!(StateStore::acquire(&directory), Err(Error::Storage)));
        let original = store.load_authenticator().unwrap();
        assert_eq!(original, AuthenticatorState::default());
        store.persist_authenticator(&AuthenticatorState {
            last_totp_step: 123,
            failures: 2,
            locked_until: 1_700_000_000,
        }).unwrap();
        let raw = fs::read(directory.join(STATE_NAME)).unwrap();
        assert!(String::from_utf8_lossy(&raw).contains("last_totp_step"));
        drop(store);
        let next = StateStore::acquire(&directory).unwrap();
        assert_eq!(next.load_authenticator().unwrap().last_totp_step, 123);
        drop(next);
        fs::remove_dir_all(directory).unwrap();
    }

    #[test]
    fn rejects_state_symlinks_and_insecure_modes() {
        let directory = dir();
        let source = directory.join("other");
        fs::write(&source, b"{}").unwrap();
        std::os::unix::fs::symlink(&source, directory.join(STATE_NAME)).unwrap();
        let store = StateStore::acquire(&directory).unwrap();
        assert!(matches!(store.load_authenticator(), Err(Error::Storage)));
        drop(store);
        fs::remove_dir_all(directory).unwrap();
    }

    #[test]
    fn reads_go_zero_time() {
        let raw = br#"{"version":1,"last_totp_step":-1,"failures":0,"locked_until":"0001-01-01T00:00:00Z"}"#;
        assert_eq!(decode_state(Some(raw.to_vec())).unwrap(), AuthenticatorState::default());
    }
}
