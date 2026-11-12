//! Owner-only and authenticated-gateway HTTP transports over Unix sockets.
use std::collections::BTreeMap;
use std::ffi::CString;
use std::fs::{self, OpenOptions};
use std::io::Write;
use std::os::unix::fs::{FileTypeExt, OpenOptionsExt, PermissionsExt};
use std::path::{Path, PathBuf};
use std::sync::{Arc, Mutex};
use std::time::{Duration, Instant};

use axum::body::Bytes;
use axum::extract::{DefaultBodyLimit, Path as RoutePath, State};
use axum::http::{header, HeaderMap, HeaderValue, StatusCode};
use axum::middleware::{self, Next};
use axum::response::{Html, IntoResponse, Redirect, Response};
use axum::routing::{get, post};
use axum::{Json, Router};
use hyper_util::rt::{TokioExecutor, TokioIo};
use hyper_util::server::conn::auto::Builder;
use hyper_util::service::TowerToHyperService;
use serde::{Deserialize, Serialize};
use serde_json::json;
use serde_json::value::RawValue;
use tokio::net::UnixListener;
use zeroize::Zeroizing;

use crate::credentials::PassphraseRecord;
use crate::sessions::{Authority, AuthorizationWire};
use crate::Error;

const MAX_JSON_BYTES: usize = 1 << 20;
const MAX_CSRF: usize = 256;
const CSRF_LIFETIME: Duration = Duration::from_secs(600);
const LOCAL_OWNER: &str = "local-operator";

// Keep a remote credential protected until its HTTP body releases the buffer.
struct ProtectedBody(Zeroizing<Vec<u8>>);

impl AsRef<[u8]> for ProtectedBody {
    fn as_ref(&self) -> &[u8] {
        self.0.as_slice()
    }
}

#[derive(Clone)]
pub struct Shared {
    pub(crate) oauth: Option<crate::oauth::SharedBroker>,
    authority: Arc<Mutex<Authority>>,
    csrf: Arc<Mutex<BTreeMap<String, (String, Instant)>>>,
    browser_origin: Option<String>,
}

impl Shared {
    pub fn new(
        authority: Authority,
        browser_origin: Option<String>,
        oauth: Option<crate::oauth::SharedBroker>,
    ) -> Result<Self, Error> {
        if browser_origin
            .as_ref()
            .is_some_and(|origin| !valid_origin(origin))
        {
            return Err(Error::InvalidConfiguration);
        }
        Ok(Self {
            oauth,
            authority: Arc::new(Mutex::new(authority)),
            csrf: Arc::new(Mutex::new(BTreeMap::new())),
            browser_origin,
        })
    }

    pub(crate) fn with_authority<T>(&self, action: impl FnOnce(&mut Authority) -> T) -> T {
        let mut authority = self
            .authority
            .lock()
            .unwrap_or_else(|_| std::process::abort());
        action(&mut authority)
    }

    fn new_csrf(&self, principal: &str) -> Option<String> {
        let mut values = self.csrf.lock().ok()?;
        let now = Instant::now();
        values.retain(|_, (_, expiry)| *expiry > now);
        if values.len() >= MAX_CSRF {
            return None;
        }
        let mut bytes = [0; 24];
        getrandom::fill(&mut bytes).ok()?;
        let token = hex::encode(bytes);
        values.insert(token.clone(), (principal.into(), now + CSRF_LIFETIME));
        Some(token)
    }

    fn take_csrf(&self, principal: &str, token: &str) -> bool {
        if token.len() != 48 {
            return false;
        }
        let Ok(mut values) = self.csrf.lock() else {
            return false;
        };
        values
            .remove(token)
            .is_some_and(|(owner, expiry)| owner == principal && expiry > Instant::now())
    }
}

fn valid_origin(origin: &str) -> bool {
    let Some(host) = origin.strip_prefix("https://") else {
        return false;
    };
    !host.is_empty()
        && host.len() < 254
        && !host
            .chars()
            .any(|c| matches!(c, '/' | '?' | '#' | '@' | '\\' | ' '))
        && host
            .bytes()
            .all(|b| b.is_ascii_alphanumeric() || matches!(b, b'.' | b'-'))
        && !host.starts_with('.')
        && !host.ends_with('.')
        && host.parse::<std::net::IpAddr>().is_err()
        && host != "localhost"
}

fn protected_status(message: &'static str, status: StatusCode) -> Response {
    (status, message).into_response()
}

fn audit(event: &'static str) {
    // Only fixed event categories enter the service journal; no secrets or IDs.
    eprintln!("security audit {event}");
}

fn json_reply<T: Serialize>(status: StatusCode, value: T) -> Response {
    (status, Json(value)).into_response()
}

#[allow(clippy::result_large_err)]
fn owner_assertion(headers: &HeaderMap) -> Result<&str, Response> {
    let assertion = headers
        .get("cf-access-jwt-assertion")
        .and_then(|header| header.to_str().ok())
        .filter(|value| !value.is_empty() && value.len() <= (16 << 10));
    assertion
        .ok_or_else(|| protected_status("verified owner identity required", StatusCode::FORBIDDEN))
}

#[allow(clippy::result_large_err)]
pub(crate) fn json_input<T: serde::de::DeserializeOwned>(
    headers: &HeaderMap,
    raw: &[u8],
) -> Result<T, Response> {
    if headers
        .get(header::CONTENT_TYPE)
        .and_then(|value| value.to_str().ok())
        != Some("application/json")
        || raw.len() > MAX_JSON_BYTES
    {
        return Err(protected_status(
            "invalid JSON request",
            StatusCode::BAD_REQUEST,
        ));
    }
    let mut deserializer = serde_json::Deserializer::from_slice(raw);
    let value = T::deserialize(&mut deserializer)
        .map_err(|_| protected_status("invalid JSON request", StatusCode::BAD_REQUEST))?;
    deserializer
        .end()
        .map_err(|_| protected_status("trailing JSON request", StatusCode::BAD_REQUEST))?;
    Ok(value)
}

#[derive(Deserialize)]
#[serde(deny_unknown_fields)]
struct OwnerFactors {
    password: String,
    totp: String,
}

#[derive(Deserialize)]
#[serde(deny_unknown_fields)]
struct CredentialInput {
    provider: String,
    secret: String,
    password: String,
    totp: String,
}

#[derive(Deserialize)]
#[serde(deny_unknown_fields)]
struct CredentialDelete {
    password: String,
    totp: String,
}

#[derive(Deserialize)]
#[serde(deny_unknown_fields)]
struct OperationInput {
    operation: String,
    target: String,
    arguments: Option<Box<RawValue>>,
}

fn arguments(input: &OperationInput) -> &[u8] {
    input
        .arguments
        .as_ref()
        .map_or(b"{}" as &[u8], |raw| raw.get().as_bytes())
}

#[derive(Deserialize)]
#[serde(deny_unknown_fields)]
struct CredentialRedeemInput {
    ticket: String,
    operation: String,
    target: String,
    arguments: Box<RawValue>,
}

async fn session_owner_get(State(shared): State<Shared>) -> Response {
    json_reply(
        StatusCode::OK,
        shared.with_authority(Authority::local_status),
    )
}

async fn session_owner_post(
    State(shared): State<Shared>,
    headers: HeaderMap,
    raw: Bytes,
) -> Response {
    let Ok(factors) = json_input::<OwnerFactors>(&headers, &raw) else {
        return protected_status("invalid request", StatusCode::BAD_REQUEST);
    };
    let password = Zeroizing::new(factors.password);
    let totp = Zeroizing::new(factors.totp);
    match shared.with_authority(|auth| auth.login(LOCAL_OWNER, &password, &totp)) {
        Ok(status) => {
            audit("owner_session_opened");
            json_reply(StatusCode::OK, status)
        }
        Err(_) => {
            audit("owner_authentication_denied");
            protected_status("authentication denied", StatusCode::UNAUTHORIZED)
        }
    }
}

async fn session_owner_delete(State(shared): State<Shared>) -> Response {
    shared.with_authority(Authority::local_revoke);
    audit("owner_session_revoked");
    json_reply(StatusCode::OK, json!({"active":false}))
}

async fn confirmations_owner_get(State(shared): State<Shared>) -> Response {
    json_reply(
        StatusCode::OK,
        shared.with_authority(Authority::local_pending),
    )
}

async fn confirmations_owner_post(
    State(shared): State<Shared>,
    RoutePath(id): RoutePath<String>,
    headers: HeaderMap,
    raw: Bytes,
) -> Response {
    let Ok(factors) = json_input::<OwnerFactors>(&headers, &raw) else {
        return protected_status("invalid confirmation request", StatusCode::BAD_REQUEST);
    };
    let password = Zeroizing::new(factors.password);
    let totp = Zeroizing::new(factors.totp);
    match shared.with_authority(|auth| auth.confirm(LOCAL_OWNER, &id, &password, &totp)) {
        Ok(()) => {
            audit("owner_confirmation_accepted");
            json_reply(StatusCode::OK, json!({"confirmed":true}))
        }
        Err(_) => {
            audit("owner_confirmation_denied");
            protected_status("confirmation denied", StatusCode::FORBIDDEN)
        }
    }
}

async fn credentials_owner_get(State(shared): State<Shared>) -> Response {
    match shared.with_authority(|auth| auth.credential_handles(LOCAL_OWNER)) {
        Ok(handles) => json_reply(StatusCode::OK, handles),
        Err(_) => protected_status("credential vault unavailable", StatusCode::FORBIDDEN),
    }
}

async fn credentials_owner_post(
    State(shared): State<Shared>,
    headers: HeaderMap,
    raw: Bytes,
) -> Response {
    let Ok(input) = json_input::<CredentialInput>(&headers, &raw) else {
        return protected_status("invalid credential request", StatusCode::BAD_REQUEST);
    };
    let password = Zeroizing::new(input.password);
    let totp = Zeroizing::new(input.totp);
    let secret = Zeroizing::new(input.secret);
    let result = shared.with_authority(|auth| {
        auth.store_credential(
            LOCAL_OWNER,
            &password,
            &totp,
            &input.provider,
            secret.as_bytes(),
        )
    });
    match result {
        Ok(handle) => {
            audit("credential_stored");
            json_reply(StatusCode::OK, handle)
        }
        Err(_) => {
            audit("credential_store_denied");
            protected_status("credential request denied", StatusCode::FORBIDDEN)
        }
    }
}

async fn credentials_owner_put(
    State(shared): State<Shared>,
    RoutePath(id): RoutePath<String>,
    headers: HeaderMap,
    raw: Bytes,
) -> Response {
    let Ok(input) = json_input::<CredentialInput>(&headers, &raw) else {
        return protected_status("invalid credential request", StatusCode::BAD_REQUEST);
    };
    let password = Zeroizing::new(input.password);
    let totp = Zeroizing::new(input.totp);
    let secret = Zeroizing::new(input.secret);
    let result = shared.with_authority(|auth| {
        auth.rotate_credential(
            LOCAL_OWNER,
            &password,
            &totp,
            &id,
            &input.provider,
            secret.as_bytes(),
        )
    });
    match result {
        Ok(handle) => {
            audit("credential_rotated");
            json_reply(StatusCode::OK, handle)
        }
        Err(_) => {
            audit("credential_rotation_denied");
            protected_status("credential rotation denied", StatusCode::FORBIDDEN)
        }
    }
}

async fn credentials_owner_delete(
    State(shared): State<Shared>,
    RoutePath(id): RoutePath<String>,
    headers: HeaderMap,
    raw: Bytes,
) -> Response {
    let Ok(input) = json_input::<CredentialDelete>(&headers, &raw) else {
        return protected_status("invalid credential request", StatusCode::BAD_REQUEST);
    };
    let password = Zeroizing::new(input.password);
    let totp = Zeroizing::new(input.totp);
    match shared.with_authority(|auth| auth.revoke_credential(LOCAL_OWNER, &password, &totp, &id)) {
        Ok(()) => {
            audit("credential_revoked");
            json_reply(StatusCode::OK, json!({"revoked":true}))
        }
        Err(_) => {
            audit("credential_revocation_denied");
            protected_status("credential revocation denied", StatusCode::FORBIDDEN)
        }
    }
}

async fn session_client_get(State(shared): State<Shared>, headers: HeaderMap) -> Response {
    let Ok(assertion) = owner_assertion(&headers) else {
        return protected_status("verified owner identity required", StatusCode::FORBIDDEN);
    };
    match shared.with_authority(|auth| auth.status_gateway(assertion)) {
        Ok(status) => json_reply(StatusCode::OK, status),
        Err(_) => protected_status("independent owner identity required", StatusCode::FORBIDDEN),
    }
}

async fn session_client_delete(State(shared): State<Shared>, headers: HeaderMap) -> Response {
    let Ok(assertion) = owner_assertion(&headers) else {
        return protected_status("verified owner identity required", StatusCode::FORBIDDEN);
    };
    match shared.with_authority(|auth| auth.revoke_gateway(assertion)) {
        Ok(true) => json_reply(StatusCode::OK, json!({"active":false})),
        _ => protected_status("owner session unavailable", StatusCode::FORBIDDEN),
    }
}

async fn operation_response(
    shared: Shared,
    headers: HeaderMap,
    raw: Bytes,
    consume: bool,
    peer: Option<&crate::grants::VerifiedServiceIdentity>,
    replay: bool,
) -> Response {
    let Ok(assertion) = owner_assertion(&headers) else {
        return protected_status("independent owner identity required", StatusCode::FORBIDDEN);
    };
    let Ok(input) = json_input::<OperationInput>(&headers, &raw) else {
        return protected_status("invalid operation request", StatusCode::BAD_REQUEST);
    };
    let args = arguments(&input);
    let result = shared.with_authority(|auth| {
        if replay {
            auth.replay_gateway(assertion, &input.operation, &input.target, args)
                .map(AuthorizationWire::from)
        } else if consume {
            match peer {
                Some(peer) => auth.authorize_remote_gateway(
                    assertion,
                    peer,
                    &input.operation,
                    &input.target,
                    args,
                ),
                None => auth
                    .authorize_gateway(assertion, &input.operation, &input.target, args)
                    .map(AuthorizationWire::from),
            }
        } else {
            auth.preview_gateway(assertion, &input.operation, &input.target, args)
                .map(AuthorizationWire::from)
        }
    });
    match result {
        Ok(wire) => {
            if consume {
                audit(if wire.allowed {
                    "operation_authorized"
                } else if wire.confirmation_id.is_some() {
                    "operation_confirmation_required"
                } else {
                    "operation_denied"
                });
            }
            json_reply(StatusCode::OK, wire)
        }
        Err(_) => protected_status("independent owner identity required", StatusCode::FORBIDDEN),
    }
}

async fn operation_client_preview(
    State(shared): State<Shared>,
    headers: HeaderMap,
    raw: Bytes,
) -> Response {
    operation_response(shared, headers, raw, false, None, false).await
}

async fn operation_client_authorize(
    State(shared): State<Shared>,
    headers: HeaderMap,
    raw: Bytes,
) -> Response {
    operation_response(shared, headers, raw, true, None, false).await
}

async fn operation_client_replay(
    State(shared): State<Shared>,
    headers: HeaderMap,
    raw: Bytes,
) -> Response {
    operation_response(shared, headers, raw, false, None, true).await
}

pub fn owner_router(shared: Shared) -> Router {
    let owner = crate::oauth::register_owner_routes(Router::new());
    owner
        .route(
            "/v1/session",
            get(session_owner_get)
                .post(session_owner_post)
                .delete(session_owner_delete),
        )
        .route("/v1/confirmations", get(confirmations_owner_get))
        .route("/v1/confirmations/{id}", post(confirmations_owner_post))
        .route(
            "/v1/credentials",
            get(credentials_owner_get).post(credentials_owner_post),
        )
        .route(
            "/v1/credentials/{id}",
            axum::routing::put(credentials_owner_put).delete(credentials_owner_delete),
        )
        .layer(DefaultBodyLimit::max(MAX_JSON_BYTES))
        .layer(middleware::from_fn(private_headers))
        .with_state(shared)
}

async fn remote_authorize(
    axum::Extension(peer): axum::Extension<crate::remote::VerifiedPeer>,
    State(shared): State<Shared>,
    headers: HeaderMap,
    raw: Bytes,
) -> Response {
    if !peer.may_authorize {
        return protected_status(
            "remote operation authorization denied",
            StatusCode::FORBIDDEN,
        );
    }
    operation_response(shared, headers, raw, true, Some(&peer.identity), false).await
}

async fn remote_preview(
    axum::Extension(peer): axum::Extension<crate::remote::VerifiedPeer>,
    State(shared): State<Shared>,
    headers: HeaderMap,
    raw: Bytes,
) -> Response {
    if !peer.may_authorize {
        return protected_status(
            "remote operation authorization denied",
            StatusCode::FORBIDDEN,
        );
    }
    operation_response(shared, headers, raw, false, Some(&peer.identity), false).await
}

async fn remote_replay(
    axum::Extension(peer): axum::Extension<crate::remote::VerifiedPeer>,
    State(shared): State<Shared>,
    headers: HeaderMap,
    raw: Bytes,
) -> Response {
    if !peer.may_authorize {
        return protected_status("remote replay authorization denied", StatusCode::FORBIDDEN);
    }
    operation_response(shared, headers, raw, false, Some(&peer.identity), true).await
}

async fn remote_service_credential(
    axum::Extension(peer): axum::Extension<crate::remote::VerifiedPeer>,
    State(shared): State<Shared>,
    RoutePath(alias): RoutePath<String>,
    headers: HeaderMap,
    raw: Bytes,
) -> Response {
    let Ok(input) = json_input::<CredentialRedeemInput>(&headers, &raw) else {
        audit("credential_redemption_malformed");
        return protected_status("invalid credential redemption", StatusCode::BAD_REQUEST);
    };
    let credential = shared.with_authority(|authority| {
        authority.use_service_credential(
            &peer.identity,
            &alias,
            &input.ticket,
            &input.operation,
            &input.target,
            input.arguments.get().as_bytes(),
            |secret| Zeroizing::new(secret.to_vec()),
        )
    });
    let Ok(bytes) = credential else {
        audit("credential_redemption_denied");
        return protected_status("service credential access denied", StatusCode::FORBIDDEN);
    };
    if bytes.is_empty() || bytes.len() > 8192 {
        audit("credential_redemption_denied");
        return protected_status("service credential unavailable", StatusCode::FORBIDDEN);
    }
    audit("credential_redemption_allowed");
    let mut response =
        axum::body::Body::from(Bytes::from_owner(ProtectedBody(bytes))).into_response();
    response.headers_mut().insert(
        header::CONTENT_TYPE,
        HeaderValue::from_static("application/octet-stream"),
    );
    response
}

// No session, owner-factor or browser handler is available over remote TLS.
pub fn remote_router(shared: Shared) -> Router {
    Router::new()
        .route("/v1/authorize", post(remote_authorize))
        .route("/v1/preview", post(remote_preview))
        .route("/v1/replay", post(remote_replay))
        .route(
            "/v1/service-credentials/{alias}",
            post(remote_service_credential),
        )
        .layer(DefaultBodyLimit::max(MAX_JSON_BYTES))
        .layer(middleware::from_fn(private_headers))
        .with_state(shared)
}

pub fn client_router(shared: Shared) -> Router {
    Router::new()
        .route(
            "/v1/session",
            get(session_client_get).delete(session_client_delete),
        )
        .route("/v1/preview", post(operation_client_preview))
        .route("/v1/replay", post(operation_client_replay))
        .route("/v1/authorize", post(operation_client_authorize))
        .layer(DefaultBodyLimit::max(MAX_JSON_BYTES))
        .layer(middleware::from_fn(private_headers))
        .with_state(shared)
}

async fn private_headers(request: axum::extract::Request, next: Next) -> Response {
    let mut response = next.run(request).await;
    let headers = response.headers_mut();
    headers.insert(header::CACHE_CONTROL, HeaderValue::from_static("no-store"));
    headers.insert(header::PRAGMA, HeaderValue::from_static("no-cache"));
    headers.insert(
        "x-content-type-options",
        HeaderValue::from_static("nosniff"),
    );
    if !headers.contains_key("referrer-policy") {
        headers.insert("referrer-policy", HeaderValue::from_static("no-referrer"));
    }
    headers.insert("x-frame-options", HeaderValue::from_static("DENY"));
    response
}

/// Paths must be owned by the security service and absent before bind.
struct OwnedSocket(PathBuf);

impl Drop for OwnedSocket {
    fn drop(&mut self) {
        if fs::symlink_metadata(&self.0).is_ok_and(|meta| meta.file_type().is_socket()) {
            let _ = fs::remove_file(&self.0);
        }
    }
}

pub async fn serve_unix(
    socket: &Path,
    mode: u32,
    group: Option<u32>,
    router: Router,
) -> Result<(), Box<dyn std::error::Error + Send + Sync>> {
    if !socket.is_absolute() || !matches!(mode, 0o600 | 0o660) {
        return Err("invalid private security socket configuration".into());
    }
    let parent = socket
        .parent()
        .ok_or("missing security runtime directory")?;
    let metadata = fs::symlink_metadata(parent)?;
    if !metadata.is_dir()
        || metadata.file_type().is_symlink()
        || metadata.permissions().mode() & 0o007 != 0
    {
        return Err("security runtime directory is accessible by other users".into());
    }
    if fs::symlink_metadata(socket).is_ok() {
        return Err("security socket path already exists".into());
    }
    let listener = UnixListener::bind(socket)?;
    let _owned_socket = OwnedSocket(socket.to_path_buf());
    if let Some(gid) = group {
        let c_path = CString::new(socket.as_os_str().as_encoded_bytes())?;
        if unsafe { libc::chown(c_path.as_ptr(), !0, gid) } != 0 {
            return Err(std::io::Error::last_os_error().into());
        }
    }
    fs::set_permissions(socket, fs::Permissions::from_mode(mode))?;

    loop {
        let (stream, _) = listener.accept().await?;
        let router = router.clone();
        tokio::spawn(async move {
            let io = TokioIo::new(stream);
            let service = TowerToHyperService::new(router);
            let builder = Builder::new(TokioExecutor::new());
            let served = tokio::time::timeout(
                Duration::from_secs(35),
                builder.serve_connection(io, service),
            )
            .await;
            if let Ok(Err(error)) = served {
                eprintln!("security request failed: {error}");
            }
        });
    }
}

// Browser routes are deliberately separate from the owner and gateway sockets.
fn escape_html(value: &str) -> String {
    value
        .replace('&', "&amp;")
        .replace('<', "&lt;")
        .replace('>', "&gt;")
        .replace('"', "&quot;")
        .replace('\'', "&#39;")
}

#[allow(clippy::result_large_err)]
fn checked_browser_assertion<'a>(
    shared: &Shared,
    headers: &'a HeaderMap,
) -> Result<&'a str, Response> {
    let Some(origin) = &shared.browser_origin else {
        return Err(protected_status(
            "owner browser unavailable",
            StatusCode::FORBIDDEN,
        ));
    };
    if headers.get(header::HOST).and_then(|v| v.to_str().ok()) != origin.strip_prefix("https://") {
        return Err(protected_status(
            "wrong owner host",
            StatusCode::MISDIRECTED_REQUEST,
        ));
    }
    owner_assertion(headers)
}

fn browser_page(shared: &Shared, assertion: &str, message: &str) -> Response {
    let (principal, session, pending) = shared.with_authority(|auth| {
        (
            auth.browser_principal(assertion),
            auth.browser_status(assertion),
            auth.browser_pending(assertion),
        )
    });
    let Ok(principal) = principal else {
        return protected_status("verified owner identity required", StatusCode::FORBIDDEN);
    };
    let Some(csrf) = shared.new_csrf(&principal) else {
        return protected_status("owner browser unavailable", StatusCode::SERVICE_UNAVAILABLE);
    };
    let Ok(session) = session else {
        return protected_status("verified owner identity required", StatusCode::FORBIDDEN);
    };
    let pending = pending.unwrap_or_default();
    let mut html = String::from("<!doctype html><html lang=\"en\"><head><meta charset=\"utf-8\"><meta name=\"viewport\" content=\"width=device-width,initial-scale=1\"><title>Homelab administration</title><style>:root{color-scheme:light;--radius:.625rem;--background:oklch(1 0 0);--foreground:oklch(0% 0 0);--card:oklch(1 0 0);--border:oklch(.922 0 0);--primary:oklch(0% 0 0);--primary-foreground:oklch(.985 0 0);--ring:oklch(.708 0 0)}@media(prefers-color-scheme:dark){:root{color-scheme:dark;--background:oklch(.145 0 0);--foreground:oklch(.985 0 0);--card:oklch(.205 0 0);--border:oklch(1 0 0 / 10%);--primary:oklch(.922 0 0);--primary-foreground:oklch(.205 0 0);--ring:oklch(.556 0 0)}}*{box-sizing:border-box}body{font:.875rem/1.5 Geist,system-ui,sans-serif;margin:0;background:var(--background);color:var(--foreground)}main{max-width:46rem;margin:4rem auto;padding:0 1.5rem}h1{font-size:1.875rem;letter-spacing:-.04em}h2{font-size:1.125rem}section{border:1px solid var(--border);border-radius:calc(var(--radius)*1.4);background:var(--card);box-shadow:0 1px 2px rgb(0 0 0 / .05);padding:1.5rem;margin:1rem 0}label{display:block;font-weight:500}input{display:block;width:100%;margin:.375rem 0 1rem;padding:.25rem .75rem;height:2.25rem;border:1px solid var(--border);border-radius:calc(var(--radius)*.8);background:transparent;color:var(--foreground);font:inherit}button{border:0;background:var(--primary);color:var(--primary-foreground);padding:.5rem 1rem;height:2.25rem;border-radius:calc(var(--radius)*.8);font:inherit;font-weight:500;cursor:pointer}button:hover{opacity:.9}input:focus-visible,button:focus-visible{outline:2px solid var(--ring);outline-offset:2px}code{overflow-wrap:anywhere}</style></head><body><main><h1>Homelab administration</h1><p>Enter credentials only on this protected owner page.</p>");
    if !message.is_empty() {
        html.push_str(&format!("<p role=\"status\">{}</p>", escape_html(message)));
    }
    if session.active {
        html.push_str("<section><h2>Administrative session active</h2>");
        html.push_str(&format!("<form method=\"post\" action=\"/close\"><input type=\"hidden\" name=\"csrf\" value=\"{}\"><button type=\"submit\">Close session</button></form></section>", csrf));
        html.push_str("<section><h2>Pending destructive operations</h2>");
        for item in pending {
            html.push_str(&format!(
                "<div><strong>{}</strong> on <strong>{}</strong><p>Arguments: <code>{}</code></p><p>SHA-256: <code>{}</code></p><p>Expires: {}</p>",
                escape_html(&item.operation), escape_html(&item.target), escape_html(&item.arguments),
                escape_html(&item.arguments_sha256), escape_html(&item.expires_at)
            ));
            if !item.confirmed {
                html.push_str(&format!("<form method=\"post\" action=\"/confirm/{}\"><input type=\"hidden\" name=\"csrf\" value=\"{}\"><label>Password<input name=\"password\" type=\"password\" required></label><label>Authenticator code<input name=\"totp\" pattern=\"[0-9]{{6}}\" inputmode=\"numeric\" required></label><button type=\"submit\">Confirm exact operation</button></form>", escape_html(&item.id), csrf));
            } else {
                html.push_str("<p>Confirmed. Retry the unchanged operation.</p>");
            }
            html.push_str("</div>");
        }
        html.push_str("</section>");

    } else {
        html.push_str(&format!("<section><h2>Sign in</h2><form method=\"post\" action=\"/login\"><input type=\"hidden\" name=\"csrf\" value=\"{}\"><label>Password<input name=\"password\" type=\"password\" required></label><label>Authenticator code<input name=\"totp\" pattern=\"[0-9]{{6}}\" inputmode=\"numeric\" required></label><button type=\"submit\">Unlock administration</button></form></section>", csrf));
    }
    html.push_str("</main></body></html>");
    let mut response = Html(html).into_response();
    response.headers_mut().insert("content-security-policy",
        HeaderValue::from_static("default-src 'none'; style-src 'unsafe-inline'; form-action 'self'; frame-ancestors 'none'; base-uri 'none'"));
    response
        .headers_mut()
        .insert("referrer-policy", HeaderValue::from_static("same-origin"));
    response
}

async fn browser_get(State(shared): State<Shared>, headers: HeaderMap) -> Response {
    let Ok(assertion) = checked_browser_assertion(&shared, &headers) else {
        return protected_status("verified owner identity required", StatusCode::FORBIDDEN);
    };
    if shared.with_authority(|auth| auth.browser_principal(assertion).is_err()) {
        return protected_status("verified owner identity required", StatusCode::FORBIDDEN);
    }
    browser_page(&shared, assertion, "")
}

async fn browser_form(
    shared: Shared,
    headers: HeaderMap,
    raw: Bytes,
    action: &str,
    confirmation_id: Option<&str>,
) -> Response {
    let Ok(assertion) = checked_browser_assertion(&shared, &headers) else {
        return protected_status("verified owner identity required", StatusCode::FORBIDDEN);
    };
    let Some(origin) = shared.browser_origin.as_deref() else {
        return protected_status("owner browser unavailable", StatusCode::FORBIDDEN);
    };
    if headers.get(header::ORIGIN).and_then(|h| h.to_str().ok()) != Some(origin)
        || headers
            .get("sec-fetch-site")
            .and_then(|h| h.to_str().ok())
            .is_some_and(|v| v != "same-origin")
        || headers
            .get(header::CONTENT_TYPE)
            .and_then(|h| h.to_str().ok())
            != Some("application/x-www-form-urlencoded")
        || raw.len() > 4096
    {
        return protected_status("untrusted owner request", StatusCode::FORBIDDEN);
    }
    let Ok(principal) = shared.with_authority(|auth| auth.browser_principal(assertion)) else {
        return protected_status("verified owner identity required", StatusCode::FORBIDDEN);
    };
    let Ok(form) = serde_urlencoded::from_bytes::<BTreeMap<String, String>>(&raw) else {
        return protected_status("invalid owner form", StatusCode::BAD_REQUEST);
    };
    let csrf = form.get("csrf").map(String::as_str).unwrap_or("");
    if !shared.take_csrf(&principal, csrf) {
        return protected_status("invalid owner form", StatusCode::FORBIDDEN);
    }
    let password = Zeroizing::new(form.get("password").cloned().unwrap_or_default());
    let otp = form.get("totp").map(String::as_str).unwrap_or("");
    let result = shared.with_authority(|auth| match action {
        "login" => auth.login_browser(assertion, &password, otp).map(|_| ()),
        "close" => auth.browser_revoke(assertion).and_then(|closed| {
            if closed {
                Ok(())
            } else {
                Err(Error::Unauthorized)
            }
        }),
        "confirm" => auth.browser_confirm(assertion, confirmation_id.unwrap_or(""), &password, otp),
        _ => Err(Error::Unauthorized),
    });
    if result.is_err() {
        return browser_page(
            &shared,
            assertion,
            "Owner authentication or confirmation denied",
        );
    }
    Redirect::to("/").into_response()
}

async fn browser_login(State(shared): State<Shared>, headers: HeaderMap, raw: Bytes) -> Response {
    browser_form(shared, headers, raw, "login", None).await
}
async fn browser_close(State(shared): State<Shared>, headers: HeaderMap, raw: Bytes) -> Response {
    browser_form(shared, headers, raw, "close", None).await
}
async fn browser_confirm(
    State(shared): State<Shared>,
    RoutePath(id): RoutePath<String>,
    headers: HeaderMap,
    raw: Bytes,
) -> Response {
    if id.len() != 48 || !id.bytes().all(|b| b.is_ascii_hexdigit()) {
        return protected_status("invalid confirmation", StatusCode::NOT_FOUND);
    }
    browser_form(shared, headers, raw, "confirm", Some(&id)).await
}

fn save_console_verifier(record: &PassphraseRecord) -> Result<(), Error> {
    let directory = Path::new("/var/lib/homelab-console");
    let metadata = fs::symlink_metadata(directory).map_err(|_| Error::Storage)?;
    if !metadata.is_dir()
        || metadata.file_type().is_symlink()
        || metadata.permissions().mode() & 0o077 != 0
    {
        return Err(Error::Storage);
    }
    let mut suffix = [0u8; 12];
    getrandom::fill(&mut suffix).map_err(|_| Error::Storage)?;
    let temporary = directory.join(format!(".verifier-{}", hex::encode(suffix)));
    let destination = directory.join("verifier.json");
    let payload = serde_json::to_vec(record).map_err(|_| Error::Storage)?;
    let result = (|| -> Result<(), Error> {
        let mut file = OpenOptions::new()
            .write(true)
            .create_new(true)
            .mode(0o600)
            .open(&temporary)
            .map_err(|_| Error::Storage)?;
        file.write_all(&payload).map_err(|_| Error::Storage)?;
        file.sync_all().map_err(|_| Error::Storage)?;
        fs::rename(&temporary, &destination).map_err(|_| Error::Storage)?;
        fs::File::open(directory)
            .and_then(|dir| dir.sync_all())
            .map_err(|_| Error::Storage)?;
        let mut refresh = OpenOptions::new()
            .write(true)
            .truncate(true)
            .open(directory.join("refresh.trigger"))
            .map_err(|_| Error::Storage)?;
        refresh.write_all(b"1").map_err(|_| Error::Storage)?;
        refresh.sync_all().map_err(|_| Error::Storage)
    })();
    if result.is_err() {
        let _ = fs::remove_file(&temporary);
    }
    result
}

async fn browser_console_credential(
    State(shared): State<Shared>,
    headers: HeaderMap,
    raw: Bytes,
) -> Response {
    let Ok(assertion) = checked_browser_assertion(&shared, &headers) else {
        return protected_status("verified owner identity required", StatusCode::FORBIDDEN);
    };
    let Some(origin) = shared.browser_origin.as_deref() else {
        return protected_status("owner browser unavailable", StatusCode::FORBIDDEN);
    };
    if headers
        .get(header::ORIGIN)
        .and_then(|value| value.to_str().ok())
        != Some(origin)
        || headers
            .get("sec-fetch-site")
            .and_then(|value| value.to_str().ok())
            .is_some_and(|value| value != "same-origin")
        || headers
            .get(header::CONTENT_TYPE)
            .and_then(|value| value.to_str().ok())
            != Some("application/x-www-form-urlencoded")
        || raw.len() > 4096
    {
        return protected_status("untrusted owner request", StatusCode::FORBIDDEN);
    }
    let Ok(principal) = shared.with_authority(|auth| auth.browser_principal(assertion)) else {
        return protected_status("verified owner identity required", StatusCode::FORBIDDEN);
    };
    let Ok(form) = serde_urlencoded::from_bytes::<BTreeMap<String, String>>(&raw) else {
        return protected_status("invalid owner form", StatusCode::BAD_REQUEST);
    };
    if !shared.take_csrf(
        &principal,
        form.get("csrf").map(String::as_str).unwrap_or(""),
    ) {
        return protected_status("invalid owner form", StatusCode::FORBIDDEN);
    }
    let password = Zeroizing::new(form.get("password").cloned().unwrap_or_default());
    let totp = Zeroizing::new(form.get("totp").cloned().unwrap_or_default());
    let approved = shared.with_authority(|authority| {
        if !authority.browser_status(assertion)?.active {
            return Err(Error::Unauthorized);
        }
        authority
            .login_browser(assertion, &password, &totp)
            .map(|_| ())
    });
    if approved.is_err() {
        audit("console_credential_denied");
        return protected_status("owner authentication required", StatusCode::FORBIDDEN);
    }
    let Ok((record, secret)) = PassphraseRecord::generate_console_secret() else {
        return protected_status(
            "console credential unavailable",
            StatusCode::SERVICE_UNAVAILABLE,
        );
    };
    if save_console_verifier(&record).is_err() {
        return protected_status(
            "console credential unavailable",
            StatusCode::SERVICE_UNAVAILABLE,
        );
    }
    audit("console_credential_issued");
    let mut response = Html(format!(
        "<!doctype html><html lang=\"en\"><head><meta charset=\"utf-8\"><title>Console password</title><style>body{{font:1rem system-ui;margin:3rem auto;max-width:42rem;padding:1rem;background:#101218;color:#edf0f4}}code{{display:block;margin:2rem 0;padding:1rem;border:1px solid #555;overflow-wrap:anywhere}}</style></head><body><main><h1>Console password issued</h1><p>Record this password securely. It is displayed only once; issuing another password invalidates the previous password after the console service reloads.</p><code>{}</code><p><a href=\"/\">Return to administration</a></p></main></body></html>",
        escape_html(&secret),
    )).into_response();
    response.headers_mut().insert(
        "content-security-policy",
        HeaderValue::from_static("default-src 'none'; style-src 'unsafe-inline'; form-action 'self'; frame-ancestors 'none'; base-uri 'none'"),
    );
    response
}

pub fn browser_router(shared: Shared) -> Router {
    Router::new()
        .route("/", get(browser_get))
        .route("/login", post(browser_login))
        .route("/close", post(browser_close))
        .route("/confirm/{id}", post(browser_confirm))
        .layer(DefaultBodyLimit::max(4096))
        .layer(middleware::from_fn(private_headers))
        .with_state(shared)
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn owner_browser_rejects_untrusted_origins() {
        assert!(valid_origin("https://auth.example.invalid"));
        for value in [
            "http://auth.example.invalid",
            "https://127.0.0.1",
            "https://localhost",
            "https://evil.invalid/escape",
            "https://bad@evil.invalid",
        ] {
            assert!(
                !valid_origin(value),
                "unsafe browser origin accepted: {value}"
            );
        }
    }

    #[tokio::test]
    async fn form_referrer_policy_survives_private_headers() {
        use axum::body::Body;
        use axum::http::Request;
        use tower::ServiceExt;

        let routes = Router::new()
            .route(
                "/form",
                get(|| async {
                    let mut response =
                        Html("<form method=\"post\" action=\"/login\"></form>").into_response();
                    response
                        .headers_mut()
                        .insert("referrer-policy", HeaderValue::from_static("same-origin"));
                    response
                }),
            )
            .route("/private", get(|| async { "private" }))
            .layer(middleware::from_fn(private_headers));

        let form = routes
            .clone()
            .oneshot(Request::builder().uri("/form").body(Body::empty()).unwrap())
            .await
            .unwrap();
        assert_eq!(
            form.headers().get("referrer-policy").unwrap(),
            "same-origin"
        );

        let private = routes
            .oneshot(
                Request::builder()
                    .uri("/private")
                    .body(Body::empty())
                    .unwrap(),
            )
            .await
            .unwrap();
        assert_eq!(
            private.headers().get("referrer-policy").unwrap(),
            "no-referrer"
        );
    }

    #[tokio::test]
    async fn socket_routers_do_not_cross_privilege_boundaries() {
        use crate::credentials::PassphraseRecord;
        use crate::sessions::OperationPolicy;
        use axum::body::Body;
        use axum::http::Request;
        use tower::ServiceExt;

        let mut id = [0u8; 16];
        getrandom::fill(&mut id).unwrap();
        let dir = std::env::temp_dir().join(format!("transport-boundary-test-{}", hex::encode(id)));
        fs::create_dir(&dir).unwrap();
        fs::set_permissions(&dir, fs::Permissions::from_mode(0o700)).unwrap();
        let authority = Authority::open(
            &dir,
            "operator".into(),
            "platform-gateway".into(),
            PassphraseRecord {
                version: 1,
                iterations: 310_000,
                salt: "YWJjZGVmZ2hpamtsbW5vcHFyc3R1dnd4".into(),
                digest: "l9m+PJ5k/gZlE6OL9Ek9YfJ7L5ajP+9F+d5Z1mp4TXg".into(),
            },
            "GEZDGNBVGY3TQOJQGEZDGNBVGY3TQOJQ",
            vec![OperationPolicy {
                operation: "workspace__status".into(),
                targets: vec!["jupiter".into()],
                destructive: false,
            }],
        )
        .unwrap();
        let shared = Shared::new(authority, None, None).unwrap();
        let csrf = shared.new_csrf("verified-principal").unwrap();
        assert!(!shared.take_csrf("another-principal", &csrf));
        assert!(!shared.take_csrf("verified-principal", &csrf));
        let accepted = shared.new_csrf("verified-principal").unwrap();
        assert!(shared.take_csrf("verified-principal", &accepted));
        assert!(!shared.take_csrf("verified-principal", &accepted));
        let owner = owner_router(shared.clone());
        let gateway = client_router(shared.clone());
        let browser = browser_router(shared.clone());
        let remote = remote_router(shared);

        let request = Request::builder()
            .uri("/v1/session")
            .body(Body::empty())
            .unwrap();
        assert_eq!(
            owner.oneshot(request).await.unwrap().status(),
            StatusCode::OK
        );

        let request = Request::builder()
            .uri("/v1/session")
            .body(Body::empty())
            .unwrap();
        assert_eq!(
            gateway.clone().oneshot(request).await.unwrap().status(),
            StatusCode::FORBIDDEN
        );

        let request = Request::builder()
            .uri("/v1/authorize")
            .method("POST")
            .header("content-type", "application/json")
            .header("cf-access-jwt-assertion", "untrusted")
            .body(Body::from(
                r#"{"operation":"workspace__status","target":"jupiter","arguments":{}}"#,
            ))
            .unwrap();
        assert_eq!(
            gateway.clone().oneshot(request).await.unwrap().status(),
            StatusCode::FORBIDDEN
        );

        let request = Request::builder()
            .uri("/console-credential")
            .method("POST")
            .body(Body::empty())
            .unwrap();
        assert_eq!(
            browser.oneshot(request).await.unwrap().status(),
            StatusCode::FORBIDDEN
        );

        for path in ["/v1/credentials", "/v1/oauth/providers"] {
            let request = Request::builder().uri(path).body(Body::empty()).unwrap();
            assert_eq!(
                gateway.clone().oneshot(request).await.unwrap().status(),
                StatusCode::NOT_FOUND
            );
        }

        let request = Request::builder()
            .uri("/v1/session")
            .body(Body::empty())
            .unwrap();
        assert_eq!(
            remote.clone().oneshot(request).await.unwrap().status(),
            StatusCode::NOT_FOUND
        );

        let request = Request::builder()
            .uri("/v1/credentials")
            .body(Body::empty())
            .unwrap();
        assert_eq!(
            remote.oneshot(request).await.unwrap().status(),
            StatusCode::NOT_FOUND
        );

        fs::remove_dir_all(dir).unwrap();
    }

    #[test]
    fn escaping_prevents_untrusted_confirmation_html() {
        assert_eq!(escape_html("<script>\"&"), "&lt;script&gt;&quot;&amp;");
    }
}
