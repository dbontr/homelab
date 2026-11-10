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
    if !md.is_file()
        || (md.mode() & if private { 0o077 } else { 0o022 }) != 0
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
    if arguments.next().is_some()
        || !directory.is_absolute()
        || directory == Path::new("/var/lib/homelab-securityd")
        || directory.components().any(|v| matches!(v, std::path::Component::ParentDir))
    {
        return Err("preflight requires an independent copied state directory");
    }
    let copy = fs::symlink_metadata(&directory).map_err(|_| "state copy missing")?;
    if !copy.is_dir() || copy.file_type().is_symlink() {
        return Err("state copy must be a real directory");
    }
    // A bind mount of the live state directory is not an independent copy.
    if let Ok(live) = fs::metadata("/var/lib/homelab-securityd") {
        if copy.dev() == live.dev() && copy.ino() == live.ino() {
            return Err("preflight cannot open the live state inode");
        }
    }
    for name in ["authenticator.json"] {
        let metadata = fs::symlink_metadata(directory.join(name))
            .map_err(|_| "copied authentication replay state missing")?;
        if !metadata.is_file() || metadata.file_type().is_symlink() {
            return Err("copied authentication replay state invalid");
        }
    }
    Ok(directory)
}

fn run() -> Result<(), &'static str> {
    let directory = copied_state_directory()?;
    let verifier = read_file("HOMELAB_SECURITY_PASSWORD_FILE", true)?;
    let verifier: PassphraseRecord =
        serde_json::from_slice(&verifier).map_err(|_| "invalid passphrase verifier")?;
    let mut totp = read_file("HOMELAB_SECURITY_TOTP_FILE", true)?;
    let totp_code = std::str::from_utf8(&totp)
        .map_err(|_| "invalid TOTP configuration")?
        .trim()
        .to_owned();
    totp.zeroize();
    let policies = read_file("HOMELAB_SECURITY_POLICY_FILE", false)?;
    let policies: Vec<OperationPolicy> =
        serde_json::from_slice(&policies).map_err(|_| "invalid operation policy")?;
    let mut authority = Authority::open(
        &directory,
        required("HOMELAB_SECURITY_OWNER")?,
        "platform-gateway".into(),
        verifier,
        &totp_code,
        policies,
    )
    .map_err(|_| "authentication state validation failed")?;

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
        authority.attach_vault(secret)
            .map_err(|_| "encrypted vault structure invalid")?;
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
    // Preflight cannot certify interfaces that this kernel does not yet serve.
    for unsupported in [
        "HOMELAB_SECURITY_REMOTE_LISTEN",
        "HOMELAB_SECURITY_OAUTH_PROVIDERS",
        "HOMELAB_SECURITY_BROWSER_ORIGIN",
    ] {
        if env::var(unsupported).ok().is_some_and(|v| !v.is_empty()) {
            return Err("requested security interface has no Rust runtime acceptance");
        }
    }
    println!("Copied security state and supported policy contracts validated.");
    Ok(())
}

fn main() {
    if let Err(message) = run() {
        eprintln!("{message}");
        std::process::exit(1);
    }
}
