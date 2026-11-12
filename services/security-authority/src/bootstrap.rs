//! Trusted security-daemon configuration and exclusive state initialization.
use std::env;
use std::fs::{self, OpenOptions};
use std::io::Read;
use std::os::unix::fs::{MetadataExt, OpenOptionsExt};
use std::path::{Path, PathBuf};

use zeroize::Zeroize;

use crate::access::AccessVerifier;
use crate::credentials::PassphraseRecord;
use crate::grants::ServiceCredentialGrant;
use crate::sessions::{Authority, OperationPolicy};
use crate::Error;

const MAX_CREDENTIAL_BYTES: usize = 1 << 20;

// systemd LoadCredential may provide root-owned, group-readable files.
// That exception is limited to the exact manager-provided credential directory.
fn protected_credential_custody(
    path: &Path,
    metadata: &fs::Metadata,
    credentials_directory: Option<&Path>,
) -> bool {
    let uid = metadata.uid();
    if uid != 0 && uid != unsafe { libc::geteuid() } {
        return false;
    }
    if metadata.mode() & 0o077 == 0 {
        return true;
    }
    credentials_directory.is_some_and(|directory| {
        !directory.as_os_str().is_empty()
            && path.parent() == Some(directory)
            && metadata.mode() & 0o037 == 0
    })
}

pub fn required(name: &str) -> Result<String, Error> {
    env::var(name)
        .ok()
        .filter(|s| !s.is_empty())
        .ok_or(Error::InvalidConfiguration)
}

pub fn configured_file(name: &str, private: bool) -> Result<Vec<u8>, Error> {
    let path = PathBuf::from(required(name)?);
    if !path.is_absolute()
        || path
            .components()
            .any(|c| matches!(c, std::path::Component::ParentDir))
    {
        return Err(Error::InvalidConfiguration);
    }
    let file = OpenOptions::new()
        .read(true)
        .custom_flags(libc::O_NOFOLLOW | libc::O_CLOEXEC)
        .open(&path)
        .map_err(|_| Error::Storage)?;
    let md = file.metadata().map_err(|_| Error::Storage)?;
    let credentials_directory = env::var("CREDENTIALS_DIRECTORY").ok();
    if !md.is_file()
        || md.nlink() != 1
        || (private
            && !protected_credential_custody(
                &path,
                &md,
                credentials_directory.as_deref().map(Path::new),
            ))
        || (!private && md.mode() & 0o022 != 0)
    {
        return Err(Error::Storage);
    }
    let mut raw = Vec::new();
    file.take((MAX_CREDENTIAL_BYTES + 1) as u64)
        .read_to_end(&mut raw)
        .map_err(|_| Error::Storage)?;
    if raw.len() > MAX_CREDENTIAL_BYTES {
        raw.zeroize();
        return Err(Error::Storage);
    }
    Ok(raw)
}

fn require_authenticator_record(directory: &Path) -> Result<(), Error> {
    let metadata =
        fs::symlink_metadata(directory.join("authenticator.json")).map_err(|_| Error::Storage)?;
    if !metadata.is_file()
        || metadata.file_type().is_symlink()
        || metadata.nlink() != 1
        || metadata.mode() & 0o077 != 0
        || metadata.uid() != unsafe { libc::geteuid() }
    {
        return Err(Error::Storage);
    }
    Ok(())
}

pub struct Settings {
    pub authority: Authority,
    pub browser_origin: Option<String>,
}

pub fn initialize(directory: &Path) -> Result<Settings, Error> {
    // Switching the authority must not reset replay or lockout history.
    require_authenticator_record(directory)
        .inspect_err(|_| eprintln!("securityd startup: replay file custody"))?;
    let verifier = configured_file("HOMELAB_SECURITY_PASSWORD_FILE", true)
        .inspect_err(|_| eprintln!("securityd startup: password verifier custody"))?;
    let verifier: PassphraseRecord =
        serde_json::from_slice(&verifier).map_err(|_| Error::InvalidConfiguration)?;
    let mut otp = configured_file("HOMELAB_SECURITY_TOTP_FILE", true)
        .inspect_err(|_| eprintln!("securityd startup: TOTP credential custody"))?;
    let secret = std::str::from_utf8(&otp)
        .map_err(|_| Error::InvalidConfiguration)?
        .trim()
        .to_owned();
    otp.zeroize();
    let policies = configured_file("HOMELAB_SECURITY_POLICY_FILE", false)
        .inspect_err(|_| eprintln!("securityd startup: operation policy custody"))?;
    let policies: Vec<OperationPolicy> =
        serde_json::from_slice(&policies).map_err(|_| Error::InvalidConfiguration)?;
    let opened = Authority::open(
        directory,
        required("HOMELAB_SECURITY_OWNER")?,
        "platform-gateway".into(),
        verifier,
        &secret,
        policies,
    );
    let mut secret = zeroize::Zeroizing::new(secret);
    secret.zeroize();
    let mut authority = opened
        .inspect_err(|_| eprintln!("securityd startup: state ownership or replay validation"))?;

    if env::var("HOMELAB_SECURITY_VAULT_KEY_FILE").is_ok() {
        let mut raw = configured_file("HOMELAB_SECURITY_VAULT_KEY_FILE", true)
            .inspect_err(|_| eprintln!("securityd startup: vault key custody"))?;
        if raw.len() != 32 {
            raw.zeroize();
            return Err(Error::InvalidConfiguration);
        }
        let mut key = [0u8; 32];
        key.copy_from_slice(&raw);
        raw.zeroize();
        let attached = authority.attach_vault(key);
        key.zeroize();
        attached.inspect_err(|_| eprintln!("securityd startup: vault state validation"))?;
    } else if directory.join("credentials.json").exists() {
        return Err(Error::InvalidConfiguration);
    }

    if let Ok(config) = env::var("HOMELAB_SECURITY_SERVICE_CREDENTIAL_GRANTS") {
        if !config.is_empty() {
            if config.len() > (128 << 10) {
                return Err(Error::InvalidConfiguration);
            }
            let grants: Vec<ServiceCredentialGrant> =
                serde_json::from_str(&config).map_err(|_| Error::InvalidConfiguration)?;
            authority.configure_service_grants(grants)?;
        }
    }

    let browser_origin = env::var("HOMELAB_SECURITY_BROWSER_ORIGIN")
        .ok()
        .filter(|origin| !origin.is_empty());

    if let Ok(audience) = env::var("HOMELAB_SECURITY_ACCESS_GATEWAY_AUD") {
        if !audience.is_empty() {
            let jwks = configured_file("HOMELAB_SECURITY_ACCESS_JWKS_FILE", false)
                .inspect_err(|_| eprintln!("securityd startup: Access signing keys custody"))?;
            let mut email = configured_file("HOMELAB_SECURITY_ACCESS_OWNER_FILE", true)
                .inspect_err(|_| eprintln!("securityd startup: owner identity custody"))?;
            let owner = std::str::from_utf8(&email)
                .map_err(|_| Error::InvalidConfiguration)?
                .trim()
                .to_owned();
            email.zeroize();
            authority.configure_access(
                AccessVerifier::new(&required("HOMELAB_SECURITY_ACCESS_ISSUER")?, &owner, &jwks)?,
                audience,
                env::var("HOMELAB_SECURITY_ACCESS_BROWSER_AUD").ok(),
            )?;
        }
    }
    if browser_origin.is_some() && env::var("HOMELAB_SECURITY_ACCESS_BROWSER_AUD").is_err() {
        return Err(Error::InvalidConfiguration);
    }

    Ok(Settings {
        authority,
        browser_origin,
    })
}

#[cfg(test)]
mod tests {
    use super::*;
    #[test]
    fn startup_requires_a_protected_authenticator_record() {
        use std::os::unix::fs::PermissionsExt;
        let mut id = [0u8; 12];
        getrandom::fill(&mut id).unwrap();
        let dir = std::env::temp_dir().join(format!("security-bootstrap-test-{}", hex::encode(id)));
        fs::create_dir(&dir).unwrap();
        fs::set_permissions(&dir, fs::Permissions::from_mode(0o700)).unwrap();
        assert_eq!(require_authenticator_record(&dir), Err(Error::Storage));
        let record = dir.join("authenticator.json");
        fs::write(&record, br#"{"version":1}"#).unwrap();
        fs::set_permissions(&record, fs::Permissions::from_mode(0o600)).unwrap();
        assert_eq!(require_authenticator_record(&dir), Ok(()));
        fs::remove_file(&record).unwrap();
        let shared = dir.join("shared.json");
        fs::write(&shared, b"{}").unwrap();
        fs::set_permissions(&shared, fs::Permissions::from_mode(0o600)).unwrap();
        fs::hard_link(&shared, &record).unwrap();
        assert_eq!(require_authenticator_record(&dir), Err(Error::Storage));
        fs::remove_dir_all(dir).unwrap();
    }

    #[test]
    fn credential_group_read_is_restricted_to_systemd_directory() {
        use std::os::unix::fs::PermissionsExt;
        let mut id = [0u8; 12];
        getrandom::fill(&mut id).unwrap();
        let dir = std::env::temp_dir().join(format!("systemd-credential-test-{}", hex::encode(id)));
        fs::create_dir(&dir).unwrap();
        let credential = dir.join("password");
        fs::write(&credential, b"not-a-real-credential").unwrap();
        fs::set_permissions(&credential, fs::Permissions::from_mode(0o440)).unwrap();
        let metadata = fs::metadata(&credential).unwrap();
        assert!(!protected_credential_custody(&credential, &metadata, None));
        assert!(protected_credential_custody(
            &credential,
            &metadata,
            Some(&dir)
        ));
        let other = dir.join("unrelated");
        assert!(!protected_credential_custody(
            &credential,
            &metadata,
            Some(&other)
        ));
        fs::set_permissions(&credential, fs::Permissions::from_mode(0o444)).unwrap();
        let metadata = fs::metadata(&credential).unwrap();
        assert!(!protected_credential_custody(
            &credential,
            &metadata,
            Some(&dir)
        ));
        fs::remove_dir_all(dir).unwrap();
    }

    #[test]
    fn credential_paths_must_be_absolute() {
        let missing = configured_file("HOMELAB_SECURITY_TEST_UNSET", true);
        assert!(missing.is_err());
    }
}
