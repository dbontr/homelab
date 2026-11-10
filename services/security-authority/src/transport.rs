//! Owner-only and authenticated-gateway HTTP transports over Unix sockets.
use std::collections::BTreeMap;
use std::ffi::CString;
use std::fs;
use std::os::unix::fs::PermissionsExt;
use std::path::Path;
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
use serde_json::value::RawValue;
use serde_json::json;
use tokio::net::UnixListener;
use zeroize::Zeroizing;

use crate::sessions::{Authority, AuthorizationWire};
use crate::Error;

const MAX_JSON_BYTES: usize = 1 << 20;
const MAX_CSRF: usize = 256;
const CSRF_LIFETIME: Duration = Duration::from_secs(600);
const LOCAL_OWNER: &str = "local-operator";

#[derive(Clone)]
pub struct Shared {
    pub(crate) oauth: Option<crate::oauth::SharedBroker>,
    authority: Arc<Mutex<Authority>>,
    csrf: Arc<Mutex<BTreeMap<String, Instant>>>,
    browser_origin: Option<String>,
}

impl Shared {
    pub fn new(authority: Authority, browser_origin: Option<String>, oauth: Option<crate::oauth::SharedBroker>) -> Result<Self, Error> {
        if browser_origin.as_ref().is_some_and(|origin| !valid_origin(origin)) {
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
        let mut authority = self.authority.lock().unwrap_or_else(|_| std::process::abort());
        action(&mut authority)
    }

    fn new_csrf(&self) -> Option<String> {
        let mut values = self.csrf.lock().ok()?;
        let now = Instant::now();
        values.retain(|_, expiry| *expiry > now);
        if values.len() >= MAX_CSRF {
            return None;
        }
        let mut bytes = [0; 24];
        getrandom::fill(&mut bytes).ok()?;
        let token = hex::encode(bytes);
        values.insert(token.clone(), now + CSRF_LIFETIME);
        Some(token)
    }

    fn take_csrf(&self, token: &str) -> bool {
        if token.len() != 48 {
            return false;
        }
        let Ok(mut values) = self.csrf.lock() else {
            return false;
        };
        values.remove(token).is_some_and(|expiry| expiry > Instant::now())
    }
}

fn valid_origin(origin: &str) -> bool {
    let Some(host) = origin.strip_prefix("https://") else {
        return false;
    };
    !host.is_empty() && host.len() < 254
        && !host.chars().any(|c| matches!(c, '/' | '?' | '#' | '@' | '\\' | ' '))
        && host.bytes().all(|b| b.is_ascii_alphanumeric() || matches!(b, b'.' | b'-'))
        && !host.starts_with('.') && !host.ends_with('.')
        && host.parse::<std::net::IpAddr>().is_err()
        && host != "localhost"
}

fn protected_status(message: &'static str, status: StatusCode) -> Response {
    (status, message).into_response()
}

fn json_reply<T: Serialize>(status: StatusCode, value: T) -> Response {
    (status, Json(value)).into_response()
}

fn owner_assertion(headers: &HeaderMap) -> Result<&str, Response> {
    let assertion = headers.get("cf-access-jwt-assertion")
        .and_then(|header| header.to_str().ok())
        .filter(|value| !value.is_empty() && value.len() <= (16 << 10));
    assertion.ok_or_else(|| protected_status("verified owner identity required", StatusCode::FORBIDDEN))
}

pub(crate) fn json_input<T: serde::de::DeserializeOwned>(headers: &HeaderMap, raw: &[u8]) -> Result<T, Response> {
    if headers.get(header::CONTENT_TYPE).and_then(|value| value.to_str().ok())
        != Some("application/json") || raw.len() > MAX_JSON_BYTES
    {
        return Err(protected_status("invalid JSON request", StatusCode::BAD_REQUEST));
    }
    let mut deserializer = serde_json::Deserializer::from_slice(raw);
    let value = T::deserialize(&mut deserializer)
        .map_err(|_| protected_status("invalid JSON request", StatusCode::BAD_REQUEST))?;
    deserializer.end()
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
    input.arguments.as_ref().map_or(b"{}" as &[u8], |raw| raw.get().as_bytes())
}

async fn session_owner_get(State(shared): State<Shared>) -> Response {
    json_reply(StatusCode::OK, shared.with_authority(Authority::local_status))
}

async fn session_owner_post(State(shared): State<Shared>, headers: HeaderMap, raw: Bytes) -> Response {
    let Ok(factors) = json_input::<OwnerFactors>(&headers, &raw) else {
        return protected_status("invalid request", StatusCode::BAD_REQUEST);
    };
    let password = Zeroizing::new(factors.password);
    match shared.with_authority(|auth| auth.login(LOCAL_OWNER, &password, &factors.totp)) {
        Ok(status) => json_reply(StatusCode::OK, status),
        Err(_) => protected_status("authentication denied", StatusCode::UNAUTHORIZED),
    }
}

async fn session_owner_delete(State(shared): State<Shared>) -> Response {
    shared.with_authority(Authority::local_revoke);
    json_reply(StatusCode::OK, json!({"active":false}))
}

async fn confirmations_owner_get(State(shared): State<Shared>) -> Response {
    json_reply(StatusCode::OK, shared.with_authority(Authority::local_pending))
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
    match shared.with_authority(|auth| auth.confirm(LOCAL_OWNER, &id, &password, &factors.totp)) {
        Ok(()) => json_reply(StatusCode::OK, json!({"confirmed":true})),
        Err(_) => protected_status("confirmation denied", StatusCode::FORBIDDEN),
    }
}

async fn credentials_owner_get(State(shared): State<Shared>) -> Response {
    match shared.with_authority(|auth| auth.credential_handles(LOCAL_OWNER)) {
        Ok(handles) => json_reply(StatusCode::OK, handles),
        Err(_) => protected_status("credential vault unavailable", StatusCode::FORBIDDEN),
    }
}

async fn credentials_owner_post(State(shared): State<Shared>, headers: HeaderMap, raw: Bytes) -> Response {
    let Ok(input) = json_input::<CredentialInput>(&headers, &raw) else {
        return protected_status("invalid credential request", StatusCode::BAD_REQUEST);
    };
    let password = Zeroizing::new(input.password);
    let secret = Zeroizing::new(input.secret);
    let result = shared.with_authority(|auth| auth.store_credential(
        LOCAL_OWNER, &password, &input.totp, &input.provider, secret.as_bytes()));
    match result {
        Ok(handle) => json_reply(StatusCode::OK, handle),
        Err(_) => protected_status("credential request denied", StatusCode::FORBIDDEN),
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
    let secret = Zeroizing::new(input.secret);
    let result = shared.with_authority(|auth| auth.rotate_credential(
        LOCAL_OWNER, &password, &input.totp, &id, &input.provider, secret.as_bytes()));
    match result {
        Ok(()) => json_reply(StatusCode::OK, json!({"id":id,"provider":input.provider})),
        Err(_) => protected_status("credential rotation denied", StatusCode::FORBIDDEN),
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
    match shared.with_authority(|auth| auth.revoke_credential(LOCAL_OWNER, &password, &input.totp, &id)) {
        Ok(()) => json_reply(StatusCode::OK, json!({"revoked":true})),
        Err(_) => protected_status("credential revocation denied", StatusCode::FORBIDDEN),
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

async fn operation_response(shared: Shared, headers: HeaderMap, raw: Bytes, consume: bool) -> Response {
    let Ok(assertion) = owner_assertion(&headers) else {
        return protected_status("independent owner identity required", StatusCode::FORBIDDEN);
    };
    let Ok(input) = json_input::<OperationInput>(&headers, &raw) else {
        return protected_status("invalid operation request", StatusCode::BAD_REQUEST);
    };
    let args = arguments(&input);
    let result = shared.with_authority(|auth| {
        if consume {
            auth.authorize_gateway(assertion, &input.operation, &input.target, args)
        } else {
            auth.preview_gateway(assertion, &input.operation, &input.target, args)
        }
    });
    match result {
        Ok(decision) => json_reply(StatusCode::OK, AuthorizationWire::from(decision)),
        Err(_) => protected_status("independent owner identity required", StatusCode::FORBIDDEN),
    }
}

async fn operation_client_preview(State(shared): State<Shared>, headers: HeaderMap, raw: Bytes) -> Response {
    operation_response(shared, headers, raw, false).await
}

async fn operation_client_authorize(State(shared): State<Shared>, headers: HeaderMap, raw: Bytes) -> Response {
    operation_response(shared, headers, raw, true).await
}

pub fn owner_router(shared: Shared) -> Router {
    let owner = crate::oauth::register_owner_routes(Router::new());
    owner
        .route("/v1/session", get(session_owner_get).post(session_owner_post).delete(session_owner_delete))
        .route("/v1/confirmations", get(confirmations_owner_get))
        .route("/v1/confirmations/{id}", post(confirmations_owner_post))
        .route("/v1/credentials", get(credentials_owner_get).post(credentials_owner_post))
        .route("/v1/credentials/{id}", axum::routing::put(credentials_owner_put).delete(credentials_owner_delete))
        .layer(DefaultBodyLimit::max(MAX_JSON_BYTES))
        .layer(middleware::from_fn(private_headers))
        .with_state(shared)
}

async fn remote_authorize(
    axum::Extension(_peer): axum::Extension<crate::remote::VerifiedPeer>,
    State(shared): State<Shared>,
    headers: HeaderMap,
    raw: Bytes,
) -> Response {
    operation_response(shared, headers, raw, true).await
}

async fn remote_preview(
    axum::Extension(_peer): axum::Extension<crate::remote::VerifiedPeer>,
    State(shared): State<Shared>,
    headers: HeaderMap,
    raw: Bytes,
) -> Response {
    operation_response(shared, headers, raw, false).await
}

async fn remote_service_credential(
    axum::Extension(peer): axum::Extension<crate::remote::VerifiedPeer>,
    State(shared): State<Shared>,
    RoutePath(alias): RoutePath<String>,
) -> Response {
    let credential = shared.with_authority(|authority| {
        authority.use_service_credential(&peer.identity, &alias, |secret| secret.to_vec())
    });
    let Ok(bytes) = credential else {
        return protected_status("service credential access denied", StatusCode::FORBIDDEN);
    };
    if bytes.is_empty() || bytes.len() > 32_768 {
        return protected_status("service credential unavailable", StatusCode::FORBIDDEN);
    }
    let mut response = axum::body::Body::from(bytes).into_response();
    response.headers_mut().insert(
        header::CONTENT_TYPE, HeaderValue::from_static("application/octet-stream")
    );
    response
}

// No session, owner-factor or browser handler is available over remote TLS.
pub fn remote_router(shared: Shared) -> Router {
    Router::new()
        .route("/v1/authorize", post(remote_authorize))
        .route("/v1/preview", post(remote_preview))
        .route("/v1/service-credentials/{alias}", get(remote_service_credential))
        .layer(DefaultBodyLimit::max(MAX_JSON_BYTES))
        .layer(middleware::from_fn(private_headers))
        .with_state(shared)
}

pub fn client_router(shared: Shared) -> Router {
    Router::new()
        .route("/v1/session", get(session_client_get).delete(session_client_delete))
        .route("/v1/preview", post(operation_client_preview))
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
    headers.insert("x-content-type-options", HeaderValue::from_static("nosniff"));
    headers.insert("referrer-policy", HeaderValue::from_static("no-referrer"));
    headers.insert("x-frame-options", HeaderValue::from_static("DENY"));
    response
}

/// Paths must be owned by the security service and absent before bind.
pub async fn serve_unix(
    socket: &Path,
    mode: u32,
    group: Option<u32>,
    router: Router,
) -> Result<(), Box<dyn std::error::Error + Send + Sync>> {
    if !socket.is_absolute() || !matches!(mode, 0o600 | 0o660) {
        return Err("invalid private security socket configuration".into());
    }
    let parent = socket.parent().ok_or("missing security runtime directory")?;
    let metadata = fs::symlink_metadata(parent)?;
    if !metadata.is_dir() || metadata.file_type().is_symlink() || metadata.permissions().mode() & 0o007 != 0 {
        return Err("security runtime directory is accessible by other users".into());
    }
    if fs::symlink_metadata(socket).is_ok() {
        return Err("security socket path already exists".into());
    }
    let listener = UnixListener::bind(socket)?;
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
            let served = tokio::time::timeout(Duration::from_secs(35),
                builder.serve_connection(io, service)).await;
            if let Ok(Err(error)) = served {
                eprintln!("security request failed: {error}");
            }
        });
    }
}

// Browser routes are deliberately separate from the owner and gateway sockets.
fn escape_html(value: &str) -> String {
    value.replace('&', "&amp;").replace('<', "&lt;").replace('>', "&gt;")
        .replace('"', "&quot;").replace('\'', "&#39;")
}

fn checked_browser_assertion<'a>(shared: &Shared, headers: &'a HeaderMap) -> Result<&'a str, Response> {
    let Some(origin) = &shared.browser_origin else {
        return Err(protected_status("owner browser unavailable", StatusCode::FORBIDDEN));
    };
    if headers.get(header::HOST).and_then(|v| v.to_str().ok()) != origin.strip_prefix("https://") {
        return Err(protected_status("wrong owner host", StatusCode::MISDIRECTED_REQUEST));
    }
    owner_assertion(headers)
}

fn browser_page(shared: &Shared, assertion: &str, message: &str) -> Response {
    let Some(csrf) = shared.new_csrf() else {
        return protected_status("owner browser unavailable", StatusCode::SERVICE_UNAVAILABLE);
    };
    let (session, pending) = shared.with_authority(|auth| (
        auth.browser_status(assertion), auth.browser_pending(assertion),
    ));
    let Ok(session) = session else {
        return protected_status("verified owner identity required", StatusCode::FORBIDDEN);
    };
    let pending = pending.unwrap_or_default();
    let mut html = String::from("<!doctype html><html lang=\"en\"><head><meta charset=\"utf-8\"><meta name=\"viewport\" content=\"width=device-width,initial-scale=1\"><title>Homelab administration</title><style>body{font:1rem system-ui;margin:2rem auto;max-width:44rem;padding:0 1rem;background:#101218;color:#edf0f4}section{border:1px solid #424855;padding:1rem;border-radius:.5rem;margin:1rem 0}input{display:block;margin:.5rem 0;padding:.6rem}button{padding:.6rem 1rem}code{overflow-wrap:anywhere}</style></head><body><main><h1>Homelab administration</h1><p>Enter credentials only on this protected owner page.</p>");
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
        || headers.get("sec-fetch-site").and_then(|h| h.to_str().ok())
            .is_some_and(|v| v != "same-origin")
        || headers.get(header::CONTENT_TYPE).and_then(|h| h.to_str().ok())
            != Some("application/x-www-form-urlencoded")
        || raw.len() > 4096
    {
        return protected_status("untrusted owner request", StatusCode::FORBIDDEN);
    }
    if shared.with_authority(|auth| auth.browser_principal(assertion).is_err()) {
        return protected_status("verified owner identity required", StatusCode::FORBIDDEN);
    }
    let Ok(form) = serde_urlencoded::from_bytes::<BTreeMap<String, String>>(&raw) else {
        return protected_status("invalid owner form", StatusCode::BAD_REQUEST);
    };
    let csrf = form.get("csrf").map(String::as_str).unwrap_or("");
    if !shared.take_csrf(csrf) {
        return protected_status("invalid owner form", StatusCode::FORBIDDEN);
    }
    let password = Zeroizing::new(form.get("password").cloned().unwrap_or_default());
    let otp = form.get("totp").map(String::as_str).unwrap_or("");
    let result = shared.with_authority(|auth| match action {
        "login" => auth.login_browser(assertion, &password, otp).map(|_| ()),
        "close" => auth.browser_revoke(assertion).and_then(|closed| {
            if closed { Ok(()) } else { Err(Error::Unauthorized) }
        }),
        "confirm" => auth.browser_confirm(assertion, confirmation_id.unwrap_or(""), &password, otp),
        _ => Err(Error::Unauthorized),
    });
    if result.is_err() {
        return browser_page(&shared, assertion, "Owner authentication or confirmation denied");
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
    headers: HeaderMap, raw: Bytes,
) -> Response {
    if id.len() != 48 || !id.bytes().all(|b| b.is_ascii_hexdigit()) {
        return protected_status("invalid confirmation", StatusCode::NOT_FOUND);
    }
    browser_form(shared, headers, raw, "confirm", Some(&id)).await
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
        for value in ["http://auth.example.invalid", "https://127.0.0.1",
            "https://localhost", "https://evil.invalid/escape", "https://bad@evil.invalid"] {
            assert!(!valid_origin(value), "unsafe browser origin accepted: {value}");
        }
    }

    #[tokio::test]
    async fn socket_routers_do_not_cross_privilege_boundaries() {
        use axum::body::Body;
        use axum::http::Request;
        use tower::ServiceExt;
        use crate::credentials::PassphraseRecord;
        use crate::sessions::OperationPolicy;

        let mut id = [0u8; 16];
        getrandom::fill(&mut id).unwrap();
        let dir = std::env::temp_dir()
            .join(format!("transport-boundary-test-{}", hex::encode(id)));
        fs::create_dir(&dir).unwrap();
        fs::set_permissions(&dir, fs::Permissions::from_mode(0o700)).unwrap();
        let authority = Authority::open(
            &dir, "operator".into(), "platform-gateway".into(),
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
        ).unwrap();
        let shared = Shared::new(authority, None, None).unwrap();
        let owner = owner_router(shared.clone());
        let gateway = client_router(shared.clone());
        let remote = remote_router(shared);

        let request = Request::builder().uri("/v1/session")
            .body(Body::empty()).unwrap();
        assert_eq!(owner.oneshot(request).await.unwrap().status(), StatusCode::OK);

        let request = Request::builder().uri("/v1/session")
            .body(Body::empty()).unwrap();
        assert_eq!(gateway.oneshot(request).await.unwrap().status(), StatusCode::FORBIDDEN);

        let request = Request::builder().uri("/v1/session")
            .body(Body::empty()).unwrap();
        assert_eq!(remote.oneshot(request).await.unwrap().status(), StatusCode::NOT_FOUND);

        fs::remove_dir_all(dir).unwrap();
    }

    #[test]
    fn escaping_prevents_untrusted_confirmation_html() {
        assert_eq!(escape_html("<script>\"&"), "&lt;script&gt;&quot;&amp;");
    }
}
