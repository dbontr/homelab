//! One owner session, exact policy grants and single-use confirmations.
use std::collections::{BTreeMap, BTreeSet};
use std::time::{SystemTime, UNIX_EPOCH};

use serde::Serialize;
use serde_json::Value;

use crate::canonical::canonical_arguments;
use sha2::{Digest, Sha256};
use crate::credentials::{AuthenticatorState, Factors, PassphraseRecord};
use crate::state::StateStore;
use crate::vault::{CredentialHandle, Vault};
use crate::Error;

const IDLE_SECONDS: i64 = 120 * 60;
const ABSOLUTE_SECONDS: i64 = 12 * 60 * 60;
const CONFIRMATION_SECONDS: i64 = 5 * 60;
const CONFIRMED_SECONDS: i64 = 2 * 60;
const MAX_PENDING: usize = 64;

#[derive(Clone, Debug)]
pub struct OperationPolicy {
    pub operation: String,
    pub targets: Vec<String>,
    pub destructive: bool,
}

#[derive(Clone)]
struct Session {
    principal: String,
    id: String,
    created: i64,
    last_activity: i64,
}

#[derive(Clone)]
struct Confirmation {
    session_id: String,
    operation: String,
    target: String,
    digest: [u8; 32],
    display_arguments: String,
    expires: i64,
    confirmed: bool,
}

#[derive(Clone, Debug, PartialEq, Eq, Serialize)]
pub struct SessionStatus {
    pub active: bool,
    pub owner: Option<String>,
    pub authenticated_at: Option<i64>,
    pub last_activity_at: Option<i64>,
    pub idle_expires_at: Option<i64>,
    pub absolute_expires_at: Option<i64>,
}

impl SessionStatus {
    fn inactive() -> Self {
        Self {
            active: false,
            owner: None,
            authenticated_at: None,
            last_activity_at: None,
            idle_expires_at: None,
            absolute_expires_at: None,
        }
    }
}

#[derive(Clone, Debug, PartialEq, Eq)]
pub enum Decision {
    Allowed,
    Denied(&'static str),
    Confirmation {
        id: String,
        expires_at: i64,
    },
}

#[derive(Clone, Debug, PartialEq, Eq, Serialize)]
pub struct ConfirmationPreview {
    pub id: String,
    pub operation: String,
    pub target: String,
    pub arguments_sha256: String,
    pub arguments: String,
    pub expires_at: i64,
    pub confirmed: bool,
}

pub struct Authority {
    owner: String,
    client: String,
    factors: Factors,
    replay: AuthenticatorState,
    store: StateStore,
    policy: BTreeMap<String, OperationPolicy>,
    session: Option<Session>,
    pending: BTreeMap<String, Confirmation>,
    vault: Option<Vault>,
}

fn valid_identifier(id: &str) -> bool {
    id.len() <= 128
        && id
            .bytes()
            .next()
            .is_some_and(|b| b.is_ascii_alphanumeric())
        && id.bytes().skip(1).all(|b| {
            b.is_ascii_alphanumeric() || matches!(b, b'_' | b'.' | b'/' | b':' | b'-')
        })
}

fn dangerous(name: &str) -> bool {
    name.to_ascii_lowercase()
        .split(|c: char| matches!(c, '.' | '_' | '-' | '/'))
        .any(|word| {
            matches!(
                word,
                "delete" | "destroy" | "purge" | "revoke" | "rotate" | "reset"
                    | "remove" | "wipe" | "drop" | "format" | "shutdown"
                    | "reboot" | "restart" | "start" | "stop" | "reload"
                    | "build" | "register" | "scale" | "activate" | "revert"
                    | "switch" | "prune" | "firewall" | "router" | "network"
                    | "credential" | "credentials" | "trust" | "rollback"
                    | "reinstall" | "merge" | "deploy" | "apply" | "force"
                    | "configure" | "cleanup" | "restore" | "clear" | "recover"
                    | "grant" | "update" | "patch"
            )
        })
}

fn unix_now() -> Result<i64, Error> {
    let elapsed = SystemTime::now()
        .duration_since(UNIX_EPOCH)
        .map_err(|_| Error::Storage)?;
    i64::try_from(elapsed.as_secs()).map_err(|_| Error::Storage)
}

fn random_id() -> Result<String, Error> {
    let mut id = [0u8; 24];
    getrandom::fill(&mut id).map_err(|_| Error::Storage)?;
    Ok(hex::encode(id))
}

fn redact(value: &mut Value) {
    match value {
        Value::Object(fields) => {
            for (name, item) in fields {
                let lower = name.to_ascii_lowercase();
                let private = [
                    "password", "passphrase", "token", "secret", "credential",
                    "api_key", "cookie", "private_key", "otp", "totp",
                    "authorization", "session",
                ]
                .iter()
                .any(|part| lower.contains(part));
                if private {
                    *item = Value::String("[REDACTED]".into());
                } else {
                    redact(item);
                }
            }
        }
        Value::Array(entries) => {
            for entry in entries {
                redact(entry);
            }
        }
        _ => (),
    }
}

fn redacted_arguments(canonical: &[u8]) -> Result<String, Error> {
    let mut value: Value = serde_json::from_slice(canonical)
        .map_err(|_| Error::InvalidArguments)?;
    redact(&mut value);
    serde_json::to_string(&value).map_err(|_| Error::InvalidArguments)
}

impl Authority {
    pub fn open(
        directory: &std::path::Path,
        owner: String,
        client: String,
        verifier: PassphraseRecord,
        totp_base32: &str,
        policies: Vec<OperationPolicy>,
    ) -> Result<Self, Error> {
        if !valid_identifier(&owner) || !valid_identifier(&client)
            || policies.is_empty() || policies.len() > 256
        {
            return Err(Error::InvalidConfiguration);
        }
        let mut policy = BTreeMap::new();
        for mut item in policies {
            if !valid_identifier(&item.operation)
                || item.targets.is_empty()
                || item.targets.len() > 64
                || item.targets.iter().any(|target| !valid_identifier(target))
                || item.targets.iter().collect::<BTreeSet<_>>().len() != item.targets.len()
            {
                return Err(Error::InvalidConfiguration);
            }
            item.destructive |= dangerous(&item.operation);
            if policy.insert(item.operation.clone(), item).is_some() {
                return Err(Error::InvalidConfiguration);
            }
        }
        let factors = Factors::new(verifier, totp_base32)?;
        let store = StateStore::acquire(directory)?;
        let replay = store.load_authenticator()?;
        Ok(Self {
            owner,
            client,
            factors,
            replay,
            store,
            policy,
            session: None,
            pending: BTreeMap::new(),
            vault: None,
        })
    }

    /// The vault and authenticator share a process-lifetime ownership lease.
    pub fn attach_vault(&mut self, key: [u8; 32]) -> Result<(), Error> {
        if self.vault.is_some() {
            return Err(Error::InvalidConfiguration);
        }
        self.vault = Some(Vault::open(&self.store, key)?);
        Ok(())
    }

    /// Owner-only credential enrollment is gated by an active owner session
    /// and a fresh password/TOTP verification.
    pub fn store_credential(
        &mut self,
        principal: &str,
        password: &str,
        otp: &str,
        provider: &str,
        secret: &[u8],
    ) -> Result<CredentialHandle, Error> {
        let now = unix_now()?;
        if !self.live(now)
            || self.session.as_ref().is_none_or(|s| s.principal != principal)
        {
            return Err(Error::Unauthorized);
        }
        self.authenticate_factors(password, otp, now)?;
        let vault = self.vault.as_mut().ok_or(Error::InvalidConfiguration)?;
        vault.store(&mut self.store, provider, secret)
    }

    pub fn rotate_credential(
        &mut self,
        principal: &str,
        password: &str,
        otp: &str,
        id: &str,
        provider: &str,
        secret: &[u8],
    ) -> Result<(), Error> {
        let now = unix_now()?;
        if !self.live(now)
            || self.session.as_ref().is_none_or(|s| s.principal != principal)
        {
            return Err(Error::Unauthorized);
        }
        self.authenticate_factors(password, otp, now)?;
        let vault = self.vault.as_mut().ok_or(Error::InvalidConfiguration)?;
        vault.rotate(&mut self.store, id, provider, secret)
    }

    pub fn revoke_credential(
        &mut self,
        principal: &str,
        password: &str,
        otp: &str,
        id: &str,
    ) -> Result<(), Error> {
        let now = unix_now()?;
        if !self.live(now)
            || self.session.as_ref().is_none_or(|s| s.principal != principal)
        {
            return Err(Error::Unauthorized);
        }
        self.authenticate_factors(password, otp, now)?;
        let vault = self.vault.as_mut().ok_or(Error::InvalidConfiguration)?;
        vault.delete(&mut self.store, id)
    }

    pub fn credential_handles(&mut self, principal: &str)
        -> Result<Vec<CredentialHandle>, Error>
    {
        if !self.live(unix_now()?)
            || self.session.as_ref().is_none_or(|s| s.principal != principal)
        {
            return Err(Error::Unauthorized);
        }
        Ok(self.vault.as_ref().ok_or(Error::InvalidConfiguration)?.handles())
    }

    fn authenticate_factors(
        &mut self,
        password: &str,
        otp: &str,
        now: i64,
    ) -> Result<(), Error> {
        self.factors.verify(&mut self.replay, password, otp, now, |next| {
            self.store.persist_authenticator(next)
        })
    }

    pub fn login(
        &mut self,
        principal: &str,
        password: &str,
        otp: &str,
    ) -> Result<SessionStatus, Error> {
        self.login_at(principal, password, otp, unix_now()?)
    }

    fn login_at(
        &mut self,
        principal: &str,
        password: &str,
        otp: &str,
        now: i64,
    ) -> Result<SessionStatus, Error> {
        if principal.is_empty() || principal.len() > 1024 {
            return Err(Error::Unauthorized);
        }
        self.authenticate_factors(password, otp, now)?;
        self.session = Some(Session {
            principal: principal.into(),
            id: random_id()?,
            created: now,
            last_activity: now,
        });
        self.pending.clear();
        Ok(self.status_at(principal, now))
    }

    fn live(&mut self, now: i64) -> bool {
        let Some(session) = &self.session else {
            return false;
        };
        if now < session.last_activity
            || now >= session.last_activity.saturating_add(IDLE_SECONDS)
            || now >= session.created.saturating_add(ABSOLUTE_SECONDS)
        {
            self.session = None;
            self.pending.clear();
            return false;
        }
        true
    }

    pub fn status(&mut self, principal: &str) -> Result<SessionStatus, Error> {
        Ok(self.status_at(principal, unix_now()?))
    }

    fn status_at(&mut self, principal: &str, now: i64) -> SessionStatus {
        if !self.live(now) {
            return SessionStatus::inactive();
        }
        let session = self.session.as_ref().expect("live session exists");
        if principal.is_empty() || principal != session.principal {
            return SessionStatus::inactive();
        }
        SessionStatus {
            active: true,
            owner: Some(self.owner.clone()),
            authenticated_at: Some(session.created),
            last_activity_at: Some(session.last_activity),
            idle_expires_at: Some(session.last_activity + IDLE_SECONDS),
            absolute_expires_at: Some(session.created + ABSOLUTE_SECONDS),
        }
    }

    pub fn revoke(&mut self, principal: &str) -> Result<bool, Error> {
        Ok(self.revoke_at(principal, unix_now()?))
    }

    fn revoke_at(&mut self, principal: &str, now: i64) -> bool {
        if !self.live(now)
            || self.session.as_ref().is_none_or(|s| s.principal != principal)
        {
            return false;
        }
        self.session = None;
        self.pending.clear();
        true
    }

    fn prune(&mut self, now: i64) {
        self.pending.retain(|_, item| now < item.expires);
    }

    pub fn pending(&mut self, principal: &str) -> Result<Vec<ConfirmationPreview>, Error> {
        Ok(self.pending_at(principal, unix_now()?))
    }

    fn pending_at(&mut self, principal: &str, now: i64) -> Vec<ConfirmationPreview> {
        if !self.live(now)
            || self.session.as_ref().is_none_or(|s| s.principal != principal)
        {
            return Vec::new();
        }
        self.prune(now);
        self.pending
            .iter()
            .map(|(id, p)| ConfirmationPreview {
                id: id.clone(),
                operation: p.operation.clone(),
                target: p.target.clone(),
                arguments_sha256: hex::encode(p.digest),
                arguments: p.display_arguments.clone(),
                expires_at: p.expires,
                confirmed: p.confirmed,
            })
            .collect()
    }

    pub fn preview(
        &mut self,
        principal: &str,
        client: &str,
        operation: &str,
        target: &str,
        args: &[u8],
    ) -> Result<Decision, Error> {
        self.authorize_at(principal, client, operation, target, args, false, unix_now()?)
    }

    pub fn authorize(
        &mut self,
        principal: &str,
        client: &str,
        operation: &str,
        target: &str,
        args: &[u8],
    ) -> Result<Decision, Error> {
        self.authorize_at(principal, client, operation, target, args, true, unix_now()?)
    }

    fn authorize_at(
        &mut self,
        principal: &str,
        client: &str,
        operation: &str,
        target: &str,
        args: &[u8],
        consume: bool,
        now: i64,
    ) -> Result<Decision, Error> {
        if client != self.client {
            return Ok(Decision::Denied("unauthorized client"));
        }
        if !self.live(now)
            || self.session.as_ref().is_none_or(|s| s.principal != principal)
        {
            return Ok(Decision::Denied("owner authentication required"));
        }
        let Some(rule) = self.policy.get(operation) else {
            return Ok(Decision::Denied("operation not permitted"));
        };
        if !rule.targets.iter().any(|permitted| permitted == target) {
            return Ok(Decision::Denied("resource not permitted"));
        }
        let canonical = match canonical_arguments(args) {
            Ok(value) => value,
            Err(_) => return Ok(Decision::Denied("invalid operation arguments")),
        };
        let digest: [u8; 32] = Sha256::digest(&canonical).into();
        if !rule.destructive {
            if consume {
                self.session.as_mut().expect("live session exists").last_activity = now;
            }
            return Ok(Decision::Allowed);
        }
        self.prune(now);
        let session = self.session.as_ref().expect("live session exists").clone();
        let matching = self.pending.iter().find_map(|(id, item)| {
            (item.session_id == session.id
                && item.operation == operation
                && item.target == target
                && item.digest == digest).then(|| (id.clone(), item.clone()))
        });
        if let Some((id, item)) = matching {
            if item.confirmed {
                if consume {
                    self.pending.remove(&id);
                    self.session.as_mut().expect("live session exists").last_activity = now;
                }
                return Ok(Decision::Allowed);
            }
            return Ok(Decision::Confirmation {
                id,
                expires_at: item.expires,
            });
        }
        if self.pending.len() >= MAX_PENDING {
            return Ok(Decision::Denied("confirmation capacity exhausted"));
        }
        let id = random_id()?;
        let expires = (now + CONFIRMATION_SECONDS)
            .min(session.created + ABSOLUTE_SECONDS);
        self.pending.insert(
            id.clone(),
            Confirmation {
                session_id: session.id,
                operation: operation.into(),
                target: target.into(),
                digest,
                display_arguments: redacted_arguments(&canonical)?,
                expires,
                confirmed: false,
            },
        );
        Ok(Decision::Confirmation {
            id,
            expires_at: expires,
        })
    }

    pub fn confirm(
        &mut self,
        principal: &str,
        id: &str,
        password: &str,
        otp: &str,
    ) -> Result<(), Error> {
        self.confirm_at(principal, id, password, otp, unix_now()?)
    }

    fn confirm_at(
        &mut self,
        principal: &str,
        id: &str,
        password: &str,
        otp: &str,
        now: i64,
    ) -> Result<(), Error> {
        if !self.live(now)
            || self.session.as_ref().is_none_or(|s| s.principal != principal)
        {
            return Err(Error::Unauthorized);
        }
        self.prune(now);
        let Some(challenge) = self.pending.get(id).cloned() else {
            return Err(Error::ConfirmationRequired);
        };
        if challenge.confirmed
            || self.session.as_ref().is_none_or(|s| s.id != challenge.session_id)
        {
            return Err(Error::ConfirmationRequired);
        }
        self.authenticate_factors(password, otp, now)?;
        let item = self.pending.get_mut(id).ok_or(Error::ConfirmationRequired)?;
        item.confirmed = true;
        item.expires = item.expires.min(now + CONFIRMED_SECONDS);
        self.session.as_mut().expect("live session exists").last_activity = now;
        Ok(())
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::credentials::PassphraseRecord;
    use std::fs;
    use std::os::unix::fs::PermissionsExt;

    const PASSWORD: &str = "correct horse battery staple";
    fn fixture() -> Authority {
        let mut random = [0u8; 16];
        getrandom::fill(&mut random).unwrap();
        let directory = std::env::temp_dir()
            .join(format!("authority-session-test-{}", hex::encode(random)));
        fs::create_dir(&directory).unwrap();
        fs::set_permissions(&directory, fs::Permissions::from_mode(0o700)).unwrap();
        Authority::open(
            &directory,
            "operator".into(),
            "platform-gateway".into(),
            PassphraseRecord {
                version: 1,
                iterations: 310_000,
                salt: "YWJjZGVmZ2hpamtsbW5vcHFyc3R1dnd4".into(),
                digest: "l9m+PJ5k/gZlE6OL9Ek9YfJ7L5ajP+9F+d5Z1mp4TXg".into(),
            },
            "GEZDGNBVGY3TQOJQGEZDGNBVGY3TQOJQ",
            vec![
                OperationPolicy {
                    operation: "workspace__status".into(),
                    targets: vec!["jupiter".into()],
                    destructive: false,
                },
                OperationPolicy {
                    operation: "infrastructure__service_restart".into(),
                    targets: vec!["jupiter".into()],
                    destructive: false,
                },
            ],
        )
        .unwrap()
    }

    fn otp(step: i64) -> String {
        let data = PassphraseRecord {
            version: 1,
            iterations: 310_000,
            salt: "YWJjZGVmZ2hpamtsbW5vcHFyc3R1dnd4".into(),
            digest: "l9m+PJ5k/gZlE6OL9Ek9YfJ7L5ajP+9F+d5Z1mp4TXg".into(),
        };
        Factors::new(data, "GEZDGNBVGY3TQOJQGEZDGNBVGY3TQOJQ")
            .unwrap()
            .totp_test_code(step)
    }

    #[test]
    fn owner_session_expires_and_denied_requests_do_not_refresh_it() {
        let mut a = fixture();
        a.login_at("principal-a", PASSWORD, &otp(100), 3_000).unwrap();
        let args = br#"{"host":"jupiter"}"#;
        assert_eq!(
            a.authorize_at("principal-b","platform-gateway","workspace__status","jupiter",args,true,3_010).unwrap(),
            Decision::Denied("owner authentication required")
        );
        assert_eq!(
            a.authorize_at("principal-a","untrusted","workspace__status","jupiter",args,true,3_010).unwrap(),
            Decision::Denied("unauthorized client")
        );
        assert!(!a.status_at("principal-a", 3_000 + IDLE_SECONDS).active);
    }

    #[test]
    fn destructive_confirmation_is_single_use_and_exact() {
        let mut a = fixture();
        a.login_at("principal-a", PASSWORD, &otp(200), 6_000).unwrap();
        let exact = br#"{"host":"jupiter","service":"homelab-platform.service"}"#;
        let changed = br#"{"host":"jupiter","service":"homelab-bridge.service"}"#;
        let pending = a.authorize_at("principal-a","platform-gateway",
            "infrastructure__service_restart","jupiter",exact,true,6_005).unwrap();
        let Decision::Confirmation { id, .. } = pending else { panic!("confirmation missing") };
        a.confirm_at("principal-a", &id, PASSWORD, &otp(201), 6_030).unwrap();
        assert!(matches!(
            a.authorize_at("principal-a","platform-gateway","infrastructure__service_restart","jupiter",changed,true,6_031).unwrap(),
            Decision::Confirmation { .. }
        ));
        assert_eq!(a.authorize_at("principal-a","platform-gateway",
            "infrastructure__service_restart","jupiter",exact,true,6_032).unwrap(),Decision::Allowed);
        assert!(matches!(
            a.authorize_at("principal-a","platform-gateway","infrastructure__service_restart","jupiter",exact,true,6_033).unwrap(),
            Decision::Confirmation { .. }
        ));
        assert!(a.revoke_at("principal-a", 6_034));
        assert_eq!(a.authorize_at("principal-a","platform-gateway",
            "infrastructure__service_restart","jupiter",exact,true,6_035).unwrap(),
            Decision::Denied("owner authentication required"));
    }

    #[test]
    fn absolute_limit_survives_repeated_valid_activity() {
        let mut authority = fixture();
        authority.login_at("principal-a", PASSWORD, &otp(300), 9_000).unwrap();
        let arguments = br#"{"host":"jupiter"}"#;
        for step in 1..=7 {
            let at = 9_000 + step * 6_000;
            assert_eq!(
                authority.authorize_at("principal-a", "platform-gateway",
                    "workspace__status", "jupiter", arguments, true, at).unwrap(),
                Decision::Allowed
            );
        }
        assert!(!authority.status_at("principal-a", 9_000 + ABSOLUTE_SECONDS).active);
    }

    #[test]
    fn pending_confirmation_never_exposes_secret_fields() {
        let mut authority = fixture();
        authority.login_at("principal-a", PASSWORD, &otp(400), 12_000).unwrap();
        let args = br#"{"host":"jupiter","password":"private-value","service":"homelab-platform.service"}"#;
        let result = authority.authorize_at(
            "principal-a", "platform-gateway", "infrastructure__service_restart",
            "jupiter", args, true, 12_001,
        ).unwrap();
        assert!(matches!(result, Decision::Confirmation { .. }));
        let pending = authority.pending_at("principal-a", 12_002);
        assert_eq!(pending.len(), 1);
        assert!(pending[0].arguments.contains("[REDACTED]"));
        assert!(!pending[0].arguments.contains("private-value"));
    }

}
