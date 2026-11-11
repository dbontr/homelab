//! Offline validation of a copied security authority state directory.
use std::env;
use std::fs::{self, OpenOptions};
use std::io::Read;
use std::os::unix::fs::{MetadataExt, OpenOptionsExt};
use std::path::{Path, PathBuf};

use homelab_security_authority::access::AccessVerifier;
use homelab_security_authority::credentials::PassphraseRecord;
use homelab_security_authority::grants::ServiceCredentialGrant;
use homelab_security_authority::sessions::{Authority, OperationPolicy};
use homelab_security_authority::oauth::Broker;
use homelab_security_authority::remote::Remote;
use homelab_security_authority::transport::Shared;
use zeroize::Zeroize;

const MAX_CREDENTIAL_BYTES: usize = 1 << 20;

fn required(name: &str) -> Result<String, &'static str> {
    env::var(name).ok().filter(|s| !s.is_empty()).ok_or("required configuration missing")
}

fn read_file(name: &str, private: bool) -> Result<Vec<u8>, &'static str> {
    let path = PathBuf::from(required(name)?);
    if !path.is_absolute() {
        return Err("configured file path must be absolute");
    }
    let mut file = OpenOptions::new()
        .read(true)
        .custom_flags(libc::O_NOFOLLOW | libc::O_CLOEXEC)
        .open(path)
        .map_err(|_| "configured file unavailable")?;
    let md = file.metadata().map_err(|_| "cannot inspect configured file")?;
    let forbidden_permissions = if private { 0o077 } else { 0o022 };
    if !md.is_file()
        || md.mode() & forbidden_permissions != 0
        || (private && md.uid() != unsafe { libc::geteuid() })
    {
        return Err("configured file has unsafe permissions");
    }
    let mut data = Vec::new();
    file.take((MAX_CREDENTIAL_BYTES + 1) as u64)
        .read_to_end(&mut data)
        .map_err(|_| "cannot read configured file")?;
    if data.len() > MAX_CREDENTIAL_BYTES {
        return Err("configured file exceeds size limit");
    }
    Ok(data)
}

fn copied_state_directory() -> Result<PathBuf, &'static str> {
    let mut arguments = env::args().skip(1);
    if arguments.next().as_deref() != Some("--state-dir") {
        return Err("usage: security-preflight --state-dir ABSOLUTE_COPY_DIRECTORY");
    }
    let directory = PathBuf::from(arguments.next().ok_or("state copy required")?);
    if arguments.next().is_some() {
        return Err("preflight requires one independent copied state directory");
    }
    validate_copy_directory(directory)
}

// A preflight copy must not share writable inodes with the source state.
fn validate_copied_file(path: &Path) -> Result<(), &'static str> {
    let metadata = fs::symlink_metadata(path).map_err(|_| "copied state file missing")?;
    if !metadata.is_file() || metadata.file_type().is_symlink()
        || metadata.nlink() != 1
        || metadata.mode() & 0o077 != 0
        || metadata.uid() != unsafe { libc::geteuid() }
    {
        return Err("copied state file is not independently owned");
    }
    Ok(())
}

// A bind-mounted source file may have one hard link; compare live file inodes.
fn reject_live_inode(copy: &Path, live: &Path) -> Result<(), &'static str> {
    let copy_md = fs::symlink_metadata(copy).map_err(|_| "copied state file missing")?;
    match fs::metadata(live) {
        Ok(live_md) if copy_md.dev() == live_md.dev() && copy_md.ino() == live_md.ino() =>
            Err("copied state file references the live authority inode"),
        Ok(_) => Ok(()),
        Err(error) if error.kind() == std::io::ErrorKind::NotFound => Ok(()),
        Err(_) => Err("cannot inspect live authority inode"),
    }
}

fn validate_copy_directory(directory: PathBuf) -> Result<PathBuf, &'static str> {
    if !directory.is_absolute()
        || directory == Path::new("/var/lib/homelab-securityd")
        || directory.components().any(|v| matches!(v, std::path::Component::ParentDir))
    {
        return Err("preflight requires an independent copied state directory");
    }
    let copy = fs::symlink_metadata(&directory).map_err(|_| "state copy missing")?;
    if !copy.is_dir() || copy.file_type().is_symlink()
        || copy.mode() & 0o077 != 0
        || copy.uid() != unsafe { libc::geteuid() }
    {
        return Err("state copy must be an owner-only directory");
    }
    // The same inode would allow a bind mount to modify the live state.
    if let Ok(live) = fs::metadata("/var/lib/homelab-securityd") {
        if copy.dev() == live.dev() && copy.ino() == live.ino() {
            return Err("preflight cannot open the live state inode");
        }
    }
    validate_copied_file(&directory.join("authenticator.json"))
        .map_err(|_| "copied authentication replay state invalid")?;
    reject_live_inode(
        &directory.join("authenticator.json"),
        Path::new("/var/lib/homelab-securityd/authenticator.json"),
    )?;
    match fs::symlink_metadata(directory.join("credentials.json")) {
        Ok(_) => {
            validate_copied_file(&directory.join("credentials.json"))
                .map_err(|_| "copied encrypted credential store invalid")?;
            reject_live_inode(
                &directory.join("credentials.json"),
                Path::new("/var/lib/homelab-securityd/credentials.json"),
            )?;
        },
        Err(error) if error.kind() == std::io::ErrorKind::NotFound => (),
        Err(_) => return Err("cannot inspect copied encrypted credential store"),
    }
    Ok(directory)
}

fn run() -> Result<(), &'static str> {
    let directory = copied_state_directory()?;
    let verifier = read_file("HOMELAB_SECURITY_PASSWORD_FILE", true)?;
    let verifier: PassphraseRecord =
        serde_json::from_slice(&verifier).map_err(|_| "invalid passphrase verifier")?;
    let mut totp = read_file("HOMELAB_SECURITY_TOTP_FILE", true)?;
    let mut totp_code = std::str::from_utf8(&totp)
        .map_err(|_| "invalid TOTP configuration")?
        .trim()
        .to_owned();
    totp.zeroize();
    let policies = read_file("HOMELAB_SECURITY_POLICY_FILE", false)?;
    let policies: Vec<OperationPolicy> =
        serde_json::from_slice(&policies).map_err(|_| "invalid operation policy")?;
    let opened = Authority::open(
        &directory,
        required("HOMELAB_SECURITY_OWNER")?,
        "platform-gateway".into(),
        verifier,
        &totp_code,
        policies,
    );
    totp_code.zeroize();
    let mut authority = opened.map_err(|_| "authentication state validation failed")?;

    if env::var("HOMELAB_SECURITY_VAULT_KEY_FILE").is_ok() {
        let credential_path = directory.join("credentials.json");
        let md = fs::symlink_metadata(&credential_path)
            .map_err(|_| "copied encrypted credential store missing")?;
        if !md.is_file() || md.file_type().is_symlink() {
            return Err("copied encrypted credential store invalid");
        }
        let mut key = read_file("HOMELAB_SECURITY_VAULT_KEY_FILE", true)?;
        if key.len() != 32 {
            key.zeroize();
            return Err("vault key must contain 32 bytes");
        }
        let mut secret = [0u8; 32];
        secret.copy_from_slice(&key);
        key.zeroize();
        let attached = authority.attach_vault(secret);
        secret.zeroize();
        attached.map_err(|_| "encrypted vault structure invalid")?;
        authority.validate_vault()
            .map_err(|_| "encrypted vault authentication failed")?;
    } else if directory.join("credentials.json").exists() {
        return Err("encrypted vault requires its owner key");
    }

    if let Ok(raw_grants) = env::var("HOMELAB_SECURITY_SERVICE_CREDENTIAL_GRANTS") {
        if raw_grants.len() > (128 << 10) {
            return Err("service credential policy exceeds size limit");
        }
        let grants: Vec<ServiceCredentialGrant> =
            serde_json::from_str(&raw_grants).map_err(|_| "invalid service grants")?;
        authority.configure_service_grants(grants)
            .map_err(|_| "invalid service credential scope")?;
    }

    if env::var("HOMELAB_SECURITY_ACCESS_GATEWAY_AUD").is_ok() {
        let jwks = read_file("HOMELAB_SECURITY_ACCESS_JWKS_FILE", false)?;
        let mut email = read_file("HOMELAB_SECURITY_ACCESS_OWNER_FILE", true)?;
        let owner_email = std::str::from_utf8(&email)
            .map_err(|_| "invalid owner email")?
            .trim()
            .to_owned();
        email.zeroize();
        let verifier = AccessVerifier::new(
            &required("HOMELAB_SECURITY_ACCESS_ISSUER")?,
            &owner_email,
            &jwks,
        )
        .map_err(|_| "invalid pinned owner-signing keys")?;
        let browser = env::var("HOMELAB_SECURITY_ACCESS_BROWSER_AUD").ok();
        authority.configure_access(
            verifier,
            required("HOMELAB_SECURITY_ACCESS_GATEWAY_AUD")?,
            browser,
        ).map_err(|_| "invalid Access audience configuration")?;
    }
    // Parse the same optional runtime inputs without binding sockets or
    // exchanging tokens. A migration preview must cover the actual selected
    // remote, browser, and provider configuration rather than silently
    // dropping any configured interface.
    let remote = Remote::from_environment()
        .map_err(|_| "invalid remote TLS authorization configuration")?;
    let broker = Broker::from_environment()
        .map_err(|_| "invalid configured OAuth provider")?;
    if broker.is_some() {
        authority.validate_vault()
            .map_err(|_| "OAuth provider requires a verified credential vault")?;
    }
    let browser_origin = env::var("HOMELAB_SECURITY_BROWSER_ORIGIN").ok()
        .filter(|origin| !origin.is_empty());
    if browser_origin.is_some()
        && env::var("HOMELAB_SECURITY_ACCESS_BROWSER_AUD")
            .ok().is_none_or(|audience| audience.is_empty())
    {
        return Err("browser origin requires a pinned browser assertion audience");
    }
    let _shared = Shared::new(authority, browser_origin, None)
        .map_err(|_| "invalid browser origin or owner authority")?;
    let _remote = remote;
    let _broker = broker;
    println!("Copied security state and selected runtime policy contracts validated.");
    Ok(())
}

fn main() {
    if let Err(message) = run() {
        eprintln!("{message}");
        std::process::exit(1);
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use std::os::unix::fs::PermissionsExt;

    #[test]
    fn rejects_state_file_that_is_the_live_inode() {
        let mut id = [0u8; 12];
        getrandom::fill(&mut id).unwrap();
        let parent = env::temp_dir().join(format!("authority-inode-test-{}", hex::encode(id)));
        fs::create_dir(&parent).unwrap();
        let live = parent.join("live.json");
        fs::write(&live, b"{}").unwrap();
        assert!(reject_live_inode(&live, &live).is_err());
        let copy = parent.join("copy.json");
        fs::write(&copy, b"{}").unwrap();
        assert!(reject_live_inode(&copy, &live).is_ok());
        fs::remove_dir_all(parent).unwrap();
    }

    #[test]
    fn rejects_hardlinked_and_unprotected_copied_state() {
        let mut id = [0u8; 12];
        getrandom::fill(&mut id).unwrap();
        let parent = env::temp_dir().join(format!("authority-hardlink-test-{}", hex::encode(id)));
        let directory = parent.join("copy");
        fs::create_dir_all(&directory).unwrap();
        fs::set_permissions(&directory, fs::Permissions::from_mode(0o700)).unwrap();
        let source = parent.join("source.json");
        fs::write(&source, br#"{"version":1}"#).unwrap();
        fs::set_permissions(&source, fs::Permissions::from_mode(0o600)).unwrap();
        fs::hard_link(&source, directory.join("authenticator.json")).unwrap();
        assert!(validate_copy_directory(directory.clone()).is_err());
        fs::remove_file(directory.join("authenticator.json")).unwrap();
        fs::write(directory.join("authenticator.json"), br#"{"version":1}"#).unwrap();
        fs::set_permissions(directory.join("authenticator.json"), fs::Permissions::from_mode(0o600)).unwrap();
        assert!(validate_copy_directory(directory.clone()).is_ok());
        fs::write(directory.join("credentials.json"), b"{}").unwrap();
        fs::set_permissions(directory.join("credentials.json"), fs::Permissions::from_mode(0o644)).unwrap();
        assert!(validate_copy_directory(directory.clone()).is_err());
        fs::set_permissions(directory.join("credentials.json"), fs::Permissions::from_mode(0o600)).unwrap();
        assert!(validate_copy_directory(directory.clone()).is_ok());
        fs::set_permissions(&directory, fs::Permissions::from_mode(0o755)).unwrap();
        assert!(validate_copy_directory(directory).is_err());
        fs::remove_dir_all(parent).unwrap();
    }

    #[test]
    fn requires_complete_independent_replay_state() {
        let mut id = [0u8; 12];
        getrandom::fill(&mut id).unwrap();
        let path = env::temp_dir().join(format!("authority-copy-test-{}", hex::encode(id)));
        fs::create_dir(&path).unwrap();
        fs::set_permissions(&path, fs::Permissions::from_mode(0o700)).unwrap();
        assert!(validate_copy_directory(path.clone()).is_err());
        fs::write(path.join("authenticator.json"),
            br#"{"version":1,"last_totp_step":-1,"failures":0,"locked_until":"0001-01-01T00:00:00Z"}"#).unwrap();
        fs::set_permissions(path.join("authenticator.json"), fs::Permissions::from_mode(0o600)).unwrap();
        assert_eq!(validate_copy_directory(path.clone()).unwrap(), path);
        let link = path.with_extension("link");
        std::os::unix::fs::symlink(&path, &link).unwrap();
        assert!(validate_copy_directory(link.clone()).is_err());
        fs::remove_file(link).unwrap();
        fs::remove_file(path.join("authenticator.json")).unwrap();
        fs::remove_dir(path).unwrap();
    }
}
