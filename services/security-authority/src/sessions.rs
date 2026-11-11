//! One owner session, exact policy grants and single-use confirmations.
use std::collections::{BTreeMap, BTreeSet};
use std::time::{SystemTime, UNIX_EPOCH};

use chrono::{DateTime, SecondsFormat, Utc};
use serde::{Deserialize, Serialize};
use serde_json::Value;

use crate::access::AccessVerifier;
use crate::canonical::canonical_arguments;
use sha2::{Digest, Sha256};
use crate::credentials::{AuthenticatorState, Factors, PassphraseRecord};
use crate::state::StateStore;
use crate::vault::{CredentialHandle, Vault};
use crate::grants::{ServiceCredentialGrant, ServiceCredentialPolicy, VerifiedServiceIdentity};
use crate::Error;

const IDLE_SECONDS: i64 = 120 * 60;
const ABSOLUTE_SECONDS: i64 = 12 * 60 * 60;
const CONFIRMATION_SECONDS: i64 = 5 * 60;
const CONFIRMED_SECONDS: i64 = 2 * 60;
const MAX_PENDING: usize = 64;

#[derive(Clone, Debug, Deserialize, Serialize)]
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
    #[serde(skip_serializing_if = "Option::is_none")]
    pub owner: Option<String>,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub authenticated_at: Option<String>,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub last_activity_at: Option<String>,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub idle_expires_at: Option<String>,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub absolute_expires_at: Option<String>,
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
pub struct AuthorizationWire {
    pub allowed: bool,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub reason: Option<String>,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub confirmation_id: Option<String>,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub confirmation_expires_at: Option<String>,
}

impl From<Decision> for AuthorizationWire {
    fn from(decision: Decision) -> Self {
        match decision {
            Decision::Allowed => Self {
                allowed: true,
                reason: None,
                confirmation_id: None,
                confirmation_expires_at: None,
            },
            Decision::Denied(reason) => Self {
                allowed: false,
                reason: Some(reason.to_owned()),
                confirmation_id: None,
                confirmation_expires_at: None,
            },
            Decision::Confirmation { id, expires_at } => Self {
                allowed: false,
                reason: Some("confirmation required".into()),
                confirmation_id: Some(id),
                confirmation_expires_at: Some(wire_time(expires_at)),
            },
        }
    }
}

#[derive(Clone, Debug, PartialEq, Eq, Serialize)]
pub struct ConfirmationPreview {
    pub id: String,
    pub operation: String,
    pub target: String,
    pub arguments_sha256: String,
    pub arguments: String,
    pub expires_at: String,
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
    service_grants: Option<ServiceCredentialPolicy>,
    access: Option<AccessVerifier>,
    gateway_audience: String,
    browser_audience: Option<String>,
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
                    | "grant" | "create" | "update" | "patch"
            )
        })
}

fn wire_time(seconds: i64) -> String {
    DateTime::<Utc>::from_timestamp(seconds, 0)
        .expect("session timestamp must be representable")
        .to_rfc3339_opts(SecondsFormat::Secs, true)
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
            service_grants: None,
            access: None,
            gateway_audience: String::new(),
            browser_audience: None,
        })
    }

    /// Only the independent authority verifies signed gateway identity.
    /// The owner portal uses a distinct audience for password entry.
    pub fn configure_access(
        &mut self,
        verifier: AccessVerifier,
        gateway_audience: String,
        browser_audience: Option<String>,
    ) -> Result<(), Error> {
        if self.access.is_some()
            || gateway_audience.is_empty()
            || browser_audience.as_ref().is_some_and(|v| v.is_empty() || *v == gateway_audience)
        {
            return Err(Error::InvalidConfiguration);
        }
        self.access = Some(verifier);
        self.gateway_audience = gateway_audience;
        self.browser_audience = browser_audience;
        Ok(())
    }

    fn verified_principal(&self, assertion: &str, audience: &str)
        -> Result<String, Error>
    {
        self.access
            .as_ref()
            .ok_or(Error::Unauthorized)?
            .verify(assertion, audience)
    }

    pub(crate) fn local_status(&mut self) -> SessionStatus {
        let Ok(now) = unix_now() else {
            return SessionStatus::inactive();
        };
        if !self.live(now) {
            return SessionStatus::inactive();
        }
        let principal = self.session.as_ref()
            .map(|session| session.principal.clone()).unwrap_or_default();
        self.status_at(&principal, now)
    }

    pub(crate) fn local_pending(&mut self) -> Vec<ConfirmationPreview> {
        let Ok(now) = unix_now() else {
            return Vec::new();
        };
        if !self.live(now) {
            return Vec::new();
        }
        let principal = self.session.as_ref()
            .map(|session| session.principal.clone()).unwrap_or_default();
        self.pending_at(&principal, now)
    }

    pub(crate) fn local_revoke(&mut self) {
        self.session = None;
        self.pending.clear();
    }

    pub(crate) fn verify_owner_factors(
        &mut self, password: &str, otp: &str
    ) -> Result<(), Error> {
        self.authenticate_factors(password, otp, unix_now()?)
    }

    pub(crate) fn local_session_id(&mut self) -> Result<String, Error> {
        if !self.live(unix_now()?) {
            return Err(Error::Unauthorized);
        }
        let session = self.session.as_ref().ok_or(Error::Unauthorized)?;
        if session.principal != "local-operator" {
            return Err(Error::Unauthorized);
        }
        Ok(session.id.clone())
    }

    // A trusted OAuth broker may store a token only for a currently active
    // owner session that initiated the one-use provider authorization.
    pub(crate) fn store_broker_credential(
        &mut self, session_id: &str, provider: &str, token: &[u8]
    ) -> Result<CredentialHandle, Error> {
        if !self.live(unix_now()?) {
            return Err(Error::Unauthorized);
        }
        if self.session.as_ref().is_none_or(|s|
            s.principal != "local-operator" || s.id != session_id
        ) {
            return Err(Error::Unauthorized);
        }
        let vault = self.vault.as_mut().ok_or(Error::InvalidConfiguration)?;
        vault.store(&mut self.store, provider, token)
    }

    pub(crate) fn replace_broker_credential(
        &mut self, session_id: &str, id: &str, provider: &str, token: &[u8]
    ) -> Result<(), Error> {
        if !self.live(unix_now()?)
            || self.session.as_ref().is_none_or(|s|
                s.principal != "local-operator" || s.id != session_id
            )
        {
            return Err(Error::Unauthorized);
        }
        let vault = self.vault.as_mut().ok_or(Error::InvalidConfiguration)?;
        vault.rotate(&mut self.store, id, provider, token)
    }

    pub(crate) fn with_broker_credential<R>(
        &mut self,
        session_id: &str, id: &str, provider: &str,
        callback: impl FnOnce(&[u8]) -> R
    ) -> Result<R, Error> {
        if !self.live(unix_now()?)
            || self.session.as_ref().is_none_or(|s|
                s.principal != "local-operator" || s.id != session_id
            )
        {
            return Err(Error::Unauthorized);
        }
        let vault = self.vault.as_ref().ok_or(Error::InvalidConfiguration)?;
        vault.with_secret(id, provider, None, callback)
    }


    pub(crate) fn browser_principal(&self, assertion: &str) -> Result<String, Error> {
        let audience = self.browser_audience.as_ref().ok_or(Error::Unauthorized)?;
        self.verified_principal(assertion, audience)
    }

    pub(crate) fn browser_pending(&mut self, assertion: &str)
        -> Result<Vec<ConfirmationPreview>, Error>
    {
        let principal = self.browser_principal(assertion)?;
        self.pending(&principal)
    }

    pub(crate) fn browser_status(&mut self, assertion: &str)
        -> Result<SessionStatus, Error>
    {
        let principal = self.browser_principal(assertion)?;
        self.status(&principal)
    }

    pub(crate) fn browser_confirm(
        &mut self, assertion: &str, id: &str, password: &str, otp: &str
    ) -> Result<(), Error> {
        let principal = self.browser_principal(assertion)?;
        self.confirm(&principal, id, password, otp)
    }

    pub(crate) fn browser_revoke(&mut self, assertion: &str)
        -> Result<bool, Error>
    {
        let principal = self.browser_principal(assertion)?;
        self.revoke(&principal)
    }

    pub fn login_browser(
        &mut self,
        assertion: &str,
        password: &str,
        otp: &str,
    ) -> Result<SessionStatus, Error> {
        let audience = self.browser_audience.as_deref().ok_or(Error::Unauthorized)?;
        let principal = self.verified_principal(assertion, audience)?;
        self.login(&principal, password, otp)
    }

    pub fn authorize_gateway(
        &mut self,
        assertion: &str,
        operation: &str,
        target: &str,
        args: &[u8],
    ) -> Result<Decision, Error> {
        let principal = self.verified_principal(assertion, &self.gateway_audience)?;
        let client = self.client.clone();
        self.authorize(&principal, &client, operation, target, args)
    }

    pub fn preview_gateway(
        &mut self,
        assertion: &str,
        operation: &str,
        target: &str,
        args: &[u8],
    ) -> Result<Decision, Error> {
        let principal = self.verified_principal(assertion, &self.gateway_audience)?;
        let client = self.client.clone();
        self.preview(&principal, &client, operation, target, args)
    }

    pub fn status_gateway(&mut self, assertion: &str)
        -> Result<SessionStatus, Error>
    {
        let principal = self.verified_principal(assertion, &self.gateway_audience)?;
        self.status(&principal)
    }

    pub fn revoke_gateway(&mut self, assertion: &str) -> Result<bool, Error> {
        let principal = self.verified_principal(assertion, &self.gateway_audience)?;
        self.revoke(&principal)
    }

    /// Validate all persisted credential authentication tags before activation.
    pub fn validate_vault(&self) -> Result<(), Error> {
        self.vault.as_ref().ok_or(Error::InvalidConfiguration)?.validate_all()
    }

    /// The vault and authenticator share a process-lifetime ownership lease.
    pub fn attach_vault(&mut self, key: [u8; 32]) -> Result<(), Error> {
        if self.vault.is_some() {
            return Err(Error::InvalidConfiguration);
        }
        let vault = Vault::open(&self.store, key)?;
        vault.validate_all()?;
        self.vault = Some(vault);
        Ok(())
    }

    /// Service grants must bind a validated TLS identity, alias and vault handle.
    pub fn configure_service_grants(
        &mut self,
        grants: Vec<ServiceCredentialGrant>,
    ) -> Result<(), Error> {
        if self.service_grants.is_some() {
            return Err(Error::InvalidConfiguration);
        }
        let vault = self.vault.as_ref().ok_or(Error::InvalidConfiguration)?;
        self.service_grants = Some(ServiceCredentialPolicy::new(grants, vault)?);
        Ok(())
    }

    pub(crate) fn use_service_credential<T>(
        &self,
        peer: &VerifiedServiceIdentity,
        alias: &str,
        use_secret: impl FnOnce(&[u8]) -> T,
    ) -> Result<T, Error> {
        // A failed vault mutation invalidates credential access until
        // persistent state has been reconciled by the exclusive owner.
        if !self.store.healthy() {
            return Err(Error::Storage);
        }
        let policy = self.service_grants.as_ref().ok_or(Error::Unauthorized)?;
        let vault = self.vault.as_ref().ok_or(Error::Unauthorized)?;
        policy.use_credential(peer, alias, vault, use_secret)
    }

    /// Owner-only credential enrollment is gated by an active owner session
    /// and a fresh password/TOTP verification.
    pub(crate) fn store_credential(
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

    pub(crate) fn rotate_credential(
        &mut self,
        principal: &str,
        password: &str,
        otp: &str,
        id: &str,
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
        vault.rotate(&mut self.store, id, provider, secret)?;
        vault.handle(id, provider)
    }

    pub(crate) fn revoke_credential(
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

    pub(crate) fn credential_handles(&mut self, principal: &str)
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
        let result = self.factors.verify(&mut self.replay, password, otp, now, |next| {
            self.store.persist_authenticator(next)
        });
        if !self.store.healthy() {
            self.session = None;
            self.pending.clear();
            return Err(Error::Storage);
        }
        result
    }

    pub(crate) fn login(
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
        if !self.store.healthy() {
            self.session = None;
            self.pending.clear();
            return false;
        }
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

    pub(crate) fn status(&mut self, principal: &str) -> Result<SessionStatus, Error> {
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
            authenticated_at: Some(wire_time(session.created)),
            last_activity_at: Some(wire_time(session.last_activity)),
            idle_expires_at: Some(wire_time(session.last_activity + IDLE_SECONDS)),
            absolute_expires_at: Some(wire_time(session.created + ABSOLUTE_SECONDS)),
        }
    }

    pub(crate) fn revoke(&mut self, principal: &str) -> Result<bool, Error> {
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

    pub(crate) fn pending(&mut self, principal: &str) -> Result<Vec<ConfirmationPreview>, Error> {
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
                expires_at: wire_time(p.expires),
                confirmed: p.confirmed,
            })
            .collect()
    }

    pub(crate) fn preview(
        &mut self,
        principal: &str,
        client: &str,
        operation: &str,
        target: &str,
        args: &[u8],
    ) -> Result<Decision, Error> {
        self.authorize_at(principal, client, operation, target, args, false, unix_now()?)
    }

    pub(crate) fn authorize(
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

    pub(crate) fn confirm(
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
    fn concurrent_requests_consume_one_destructive_confirmation() {
        use std::sync::{Arc, Mutex};
        use std::sync::atomic::{AtomicUsize, Ordering};
        let mut authority = fixture();
        authority.login_at("principal-a", PASSWORD, &otp(500), 15_000).unwrap();
        let args = br#"{"host":"jupiter","service":"homelab-platform.service"}"#;
        let operation = "infrastructure__service_restart";
        let challenge = authority.authorize_at(
            "principal-a", "platform-gateway", operation, "jupiter",
            args, true, 15_001
        ).unwrap();
        let Decision::Confirmation { id, .. } = challenge else {
            panic!("destructive policy did not require confirmation")
        };
        authority.confirm_at("principal-a", &id, PASSWORD, &otp(501), 15_030).unwrap();
        let authority = Arc::new(Mutex::new(authority));
        let accepted = Arc::new(AtomicUsize::new(0));
        std::thread::scope(|scope| {
            for _ in 0..24 {
                let authority = Arc::clone(&authority);
                let accepted = Arc::clone(&accepted);
                scope.spawn(move || {
                    let decision = authority.lock().unwrap().authorize_at(
                        "principal-a", "platform-gateway", operation, "jupiter",
                        args, true, 15_031
                    ).unwrap();
                    if decision == Decision::Allowed {
                        accepted.fetch_add(1, Ordering::SeqCst);
                    }
                });
            }
        });
        assert_eq!(accepted.load(Ordering::SeqCst), 1);
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
    fn serialization_matches_client_authorization_contract() {
        let approved: AuthorizationWire = Decision::Allowed.into();
        assert_eq!(serde_json::to_string(&approved).unwrap(), r#"{"allowed":true}"#);
        let rejected: AuthorizationWire =
            Decision::Denied("owner authentication required").into();
        assert_eq!(
            serde_json::to_string(&rejected).unwrap(),
            r#"{"allowed":false,"reason":"owner authentication required"}"#
        );
        let pending: AuthorizationWire = Decision::Confirmation {
            id: "abc".into(),
            expires_at: 1_770_000_000,
        }.into();
        let response = serde_json::to_value(&pending).unwrap();
        assert_eq!(response["allowed"], false);
        assert_eq!(response["confirmation_id"], "abc");
        assert_eq!(response["confirmation_expires_at"], "2026-02-02T02:40:00Z");
    }

    #[test]
    fn credential_grants_fail_closed_after_persistence_fault() {
        let mut authority = fixture();
        authority.attach_vault([b'k'; 32]).unwrap();
        let handle = authority.vault.as_mut().unwrap()
            .store(&mut authority.store, "research", b"protected").unwrap();
        authority.configure_service_grants(vec![ServiceCredentialGrant {
            client_dns: "worker.internal".into(),
            alias: "service_token".into(),
            provider: "research".into(),
            handle: handle.id,
            revision: None,
            expires_at: None,
        }]).unwrap();
        let peer = VerifiedServiceIdentity::from_authenticated_peer(
            "worker.internal".into()
        ).unwrap();
        assert_eq!(
            authority.use_service_credential(&peer, "service_token",
                |secret| secret.to_vec()).unwrap(),
            b"protected"
        );
        authority.store.fault_for_test();
        assert_eq!(
            authority.use_service_credential(&peer, "service_token",
                |secret| secret.to_vec()),
            Err(Error::Storage)
        );
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
