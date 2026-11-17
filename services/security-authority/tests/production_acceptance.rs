//! Isolated acceptance of owner-state custody, credential replay, and migration.
//! This suite never connects to production security transports or reads live state.
use std::fs;
use std::os::unix::fs::PermissionsExt;
use std::path::{Path, PathBuf};

use hmac::{Hmac, Mac};
use homelab_security_authority::credentials::{AuthenticatorState, Factors, PassphraseRecord};
use homelab_security_authority::migration;
use homelab_security_authority::sessions::{Authority, OperationPolicy};
use homelab_security_authority::state::StateStore;
use homelab_security_authority::Error;
use serde_json::json;
use sha1::Sha1;
use sha2::{Digest, Sha256};

struct PrivateState(PathBuf);

impl PrivateState {
    fn new() -> Self {
        let mut nonce = [0u8; 16];
        getrandom::fill(&mut nonce).unwrap();
        let path = std::env::temp_dir().join(format!(
            "security-acceptance-{}",
            hex::encode(nonce)
        ));
        fs::create_dir(&path).unwrap();
        fs::set_permissions(&path, fs::Permissions::from_mode(0o700)).unwrap();
        Self(path)
    }

    fn path(&self) -> &Path {
        &self.0
    }
}

impl Drop for PrivateState {
    fn drop(&mut self) {
        let _ = fs::remove_dir_all(&self.0);
    }
}

// Public, synthetic test vector. No deployment secret is used.
fn verifier() -> PassphraseRecord {
    PassphraseRecord {
        version: 1,
        iterations: 310_000,
        salt: "YWJjZGVmZ2hpamtsbW5vcHFyc3R1dnd4".into(),
        digest: "l9m+PJ5k/gZlE6OL9Ek9YfJ7L5ajP+9F+d5Z1mp4TXg".into(),
    }
}

const PASSWORD: &str = "correct horse battery staple";
const TOTP: &str = "GEZDGNBVGY3TQOJQGEZDGNBVGY3TQOJQ";

fn totp_at(step: u64) -> String {
    let secret = data_encoding::BASE32_NOPAD.decode(TOTP.as_bytes()).unwrap();
    let mut mac = Hmac::<Sha1>::new_from_slice(&secret).unwrap();
    mac.update(&step.to_be_bytes());
    let digest = mac.finalize().into_bytes();
    let offset = usize::from(digest[19] & 0x0f);
    let value = u32::from_be_bytes(digest[offset..offset + 4].try_into().unwrap()) & 0x7fff_ffff;
    format!("{:06}", value % 1_000_000)
}

#[test]
fn synthetic_password_totp_replay_and_lockout_survive_restart() {
    let state = PrivateState::new();
    let factors = Factors::new(verifier(), TOTP).unwrap();
    let mut store = StateStore::acquire(state.path()).unwrap();
    let mut replay = store.load_authenticator().unwrap();
    assert_eq!(totp_at(1), "287082");
    factors
        .verify(&mut replay, PASSWORD, &totp_at(1), 59, |next| {
            store.persist_authenticator(next)
        })
        .unwrap();
    assert_eq!(replay.last_totp_step, 1);
    assert_eq!(
        factors.verify(&mut replay, PASSWORD, &totp_at(1), 59, |next| {
            store.persist_authenticator(next)
        }),
        Err(Error::AuthenticationDenied)
    );
    for _ in 0..4 {
        assert_eq!(
            factors.verify(&mut replay, "invalid password", &totp_at(100), 3_000, |next| {
                store.persist_authenticator(next)
            }),
            Err(Error::AuthenticationDenied)
        );
    }
    assert_eq!(replay.locked_until, 3_300);
    drop(store);
    let mut restarted = StateStore::acquire(state.path()).unwrap();
    replay = restarted.load_authenticator().unwrap();
    assert_eq!(replay.last_totp_step, 1);
    assert_eq!(replay.locked_until, 3_300);
    assert_eq!(
        factors.verify(&mut replay, PASSWORD, &totp_at(109), 3_299, |next| {
            restarted.persist_authenticator(next)
        }),
        Err(Error::AuthenticationDenied)
    );
    factors
        .verify(&mut replay, PASSWORD, &totp_at(110), 3_300, |next| {
            restarted.persist_authenticator(next)
        })
        .unwrap();
    assert_eq!(replay.last_totp_step, 110);
    assert_eq!(replay.locked_until, 0);
}

#[test]
fn independent_authority_enforces_exclusive_writer_and_unsigned_denial() {
    let state = PrivateState::new();
    let policies = vec![OperationPolicy {
        operation: "workspace__status".into(),
        targets: vec!["jupiter".into()],
        destructive: false,
    }];
    let mut authority = Authority::open(
        state.path(),
        "operator".into(),
        "platform-gateway".into(),
        verifier(),
        TOTP,
        policies.clone(),
    )
    .unwrap();
    assert!(StateStore::acquire(state.path()).is_err());
    let mut inspect = StateStore::inspect(state.path()).unwrap();
    assert_eq!(
        inspect.persist_authenticator(&AuthenticatorState::default()),
        Err(Error::Storage)
    );
    assert_eq!(
        authority.authorize_gateway("", "workspace__status", "jupiter", b"{}"),
        Err(Error::Unauthorized)
    );
    assert!(matches!(
        authority.login_browser("unsigned-assertion", PASSWORD, "000000"),
        Err(Error::Unauthorized)
    ));
    drop(authority);
    let restarted = Authority::open(
        state.path(),
        "operator".into(),
        "platform-gateway".into(),
        verifier(),
        TOTP,
        policies,
    )
    .unwrap();
    assert!(StateStore::acquire(state.path()).is_err());
    drop(restarted);
    assert!(StateStore::acquire(state.path()).is_ok());
}

fn receipt(path: &Path, target: &str, parent: Option<String>) -> Vec<u8> {
    let state = fs::read(path.join("authenticator.json")).unwrap();
    serde_json::to_vec(&json!({
        "version": 1,
        "target": target,
        "authenticator_sha256": hex::encode(Sha256::digest(state)),
        "vault_sha256": null,
        "parent_marker_sha256": parent,
        "nonce": "a".repeat(32)
    }))
    .unwrap()
}

#[test]
fn copy_only_rust_go_rollback_preserves_replay_state_and_rejects_stale_receipts() {
    let state = PrivateState::new();
    let mut writer = StateStore::acquire(state.path()).unwrap();
    writer
        .persist_authenticator(&AuthenticatorState {
            last_totp_step: 42,
            failures: 2,
            locked_until: 0,
        })
        .unwrap();
    drop(writer);
    let original = fs::read(state.path().join("authenticator.json")).unwrap();
    assert!(migration::admit(state.path(), "go", None).is_ok());
    assert!(migration::admit(state.path(), "rust", None).is_err());
    let rust = receipt(state.path(), "rust", None);
    migration::admit(state.path(), "rust", Some(&rust)).unwrap();
    assert!(migration::admit(state.path(), "rust", Some(&rust)).is_ok());
    assert!(migration::admit(state.path(), "go", None).is_err());
    assert!(migration::admit(state.path(), "go", Some(&receipt(state.path(), "go", None))).is_err());
    let marker = fs::read(state.path().join(".authority-selection.json")).unwrap();
    let parent = hex::encode(Sha256::digest(marker));
    let go = receipt(state.path(), "go", Some(parent));
    migration::admit(state.path(), "go", Some(&go)).unwrap();
    assert!(migration::admit(state.path(), "rust", Some(&rust)).is_err());
    assert_eq!(fs::read(state.path().join("authenticator.json")).unwrap(), original);
    let readback = StateStore::inspect(state.path()).unwrap().load_authenticator().unwrap();
    assert_eq!(readback.last_totp_step, 42);
    assert_eq!(readback.failures, 2);
}

#[test]
fn canonical_approval_arguments_reject_ambiguity() {
    use homelab_security_authority::canonical::canonical_arguments;
    let ordered = br#"{"host":"jupiter","service":"research-runner.service"}"#;
    let reordered = br#"{"service":"research-runner.service","host":"jupiter"}"#;
    assert_eq!(canonical_arguments(ordered).unwrap(), canonical_arguments(reordered).unwrap());
    for ambiguous in [
        br#"{"host":"jupiter","host":"venus"}"#.as_slice(),
        br#"[]"#.as_slice(),
        br#"{"host":"jupiter","service":"research-runner.service"}garbage"#.as_slice(),
    ] {
        assert!(canonical_arguments(ambiguous).is_err());
    }
}
