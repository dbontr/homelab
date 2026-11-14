//! Allowlisted OAuth authorization-code/PKCE and device-code credential enrollment.
use std::collections::BTreeMap;
use std::net::{IpAddr, Ipv4Addr, SocketAddr};
use std::sync::Arc;
use std::time::{Duration, Instant};

use axum::body::Bytes;
use axum::extract::{Path as RoutePath, State};
use axum::http::{HeaderMap, StatusCode};
use axum::response::{IntoResponse, Response};
use axum::routing::{get, post};
use axum::{Json, Router};
use base64::{engine::general_purpose::URL_SAFE_NO_PAD, Engine};
use reqwest::{Client, Url};
use serde::{Deserialize, Serialize};
use serde_json::{json, Value};
use sha2::{Digest, Sha256};
use tokio::sync::Mutex;
use zeroize::{Zeroize, Zeroizing};

use crate::transport::{json_input, Shared};
use crate::Error;

const MAX_PENDING: usize = 64;
const MAX_TOKEN_BYTES: usize = 32768;
const MAX_CODE_BYTES: usize = 4096;

#[derive(Clone, Debug, Deserialize)]
#[serde(deny_unknown_fields)]
pub struct Provider {
    pub name: String,
    #[serde(rename = "authorizeURL", default)]
    pub authorize_url: String,
    #[serde(rename = "tokenURL")]
    pub token_url: String,
    #[serde(rename = "clientID")]
    pub client_id: String,
    #[serde(rename = "redirectURL", default)]
    pub redirect_url: String,
    #[serde(rename = "deviceAuthorizationURL", default)]
    pub device_authorization_url: String,
    #[serde(rename = "deviceVerificationHost", default)]
    pub device_verification_host: String,
    #[serde(default)]
    pub scopes: Vec<String>,
}

#[derive(Serialize)]
pub struct BeginResult {
    pub url: String,
    pub expires_at: String,
}

#[derive(Serialize)]
pub struct DeviceBeginResult {
    pub id: String,
    pub verification_url: String,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub verification_url_complete: Option<String>,
    pub user_code: String,
    pub expires_at: String,
    pub poll_after_seconds: u64,
}

#[derive(Serialize)]
pub struct DeviceStatus {
    pub state: String,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub retry_after_seconds: Option<u64>,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub credential: Option<crate::vault::CredentialHandle>,
}

struct BrowserFlow {
    provider: Provider,
    session_id: String,
    verifier: Zeroizing<String>,
    expires: Instant,
}

struct DeviceFlow {
    provider: Provider,
    session_id: String,
    device_code: Zeroizing<String>,
    expires: Instant,
    next_poll: Instant,
    interval: Duration,
}

pub struct Broker {
    providers: BTreeMap<String, Provider>,
    browser: BTreeMap<String, BrowserFlow>,
    device: BTreeMap<String, DeviceFlow>,
    callback_address: Option<SocketAddr>,
    http: Client,
}

fn random_token(bytes: usize) -> Result<String, Error> {
    let mut value = vec![0; bytes];
    getrandom::fill(&mut value).map_err(|_| Error::Storage)?;
    let encoded = URL_SAFE_NO_PAD.encode(&value);
    value.zeroize();
    Ok(encoded)
}

fn public_provider_host(host: &str) -> bool {
    if !crate::grants::valid_service_dns(host)
        || !host.contains('.')
        || host.ends_with(".local")
        || host.ends_with(".internal")
    {
        return false;
    }
    host.rsplit('.')
        .next()
        .is_some_and(|suffix| suffix.len() >= 2 && suffix.bytes().all(|c| c.is_ascii_lowercase()))
}

fn safe_https(value: &str) -> Result<Url, Error> {
    let url = Url::parse(value).map_err(|_| Error::InvalidConfiguration)?;
    if url.scheme() != "https"
        || url
            .host_str()
            .is_none_or(|host| !public_provider_host(host))
        || url.password().is_some()
        || !url.username().is_empty()
        || url.fragment().is_some()
        || url.query().is_some()
    {
        return Err(Error::InvalidConfiguration);
    }
    Ok(url)
}

fn verify_url(url: &str, name: &str) -> bool {
    if url.is_empty() || url.len() > 2048 {
        return false;
    }
    let Ok(url) = Url::parse(url) else {
        return false;
    };
    public_provider_host(name)
        && url.scheme() == "https"
        && url.host_str() == Some(name)
        && url.username().is_empty()
        && url.password().is_none()
        && url.fragment().is_none()
}

fn validate_provider(provider: &Provider) -> Result<Option<SocketAddr>, Error> {
    if provider.name.is_empty()
        || provider.name.len() > 64
        || provider
            .name
            .bytes()
            .any(|c| !c.is_ascii_alphanumeric() && c != b'-' && c != b'_')
        || provider.client_id.is_empty()
        || provider.client_id.len() > 256
        || provider.scopes.len() > 16
        || provider
            .scopes
            .iter()
            .any(|scope| scope.len() > 128 || scope.is_empty())
    {
        return Err(Error::InvalidConfiguration);
    }
    safe_https(&provider.token_url)?;
    if !provider.device_authorization_url.is_empty() {
        safe_https(&provider.device_authorization_url)?;
        if !provider.authorize_url.is_empty()
            || !provider.redirect_url.is_empty()
            || provider.device_verification_host.is_empty()
            || provider
                .device_verification_host
                .chars()
                .any(|c| matches!(c, '/' | ':' | '@' | '?' | '#' | ' '))
        {
            return Err(Error::InvalidConfiguration);
        }
        Ok(None)
    } else {
        safe_https(&provider.authorize_url)?;
        let redirect =
            Url::parse(&provider.redirect_url).map_err(|_| Error::InvalidConfiguration)?;
        if redirect.scheme() != "http"
            || redirect.host_str() != Some("127.0.0.1")
            || redirect.path() != "/oauth/callback"
            || redirect.port().is_none()
            || redirect.query().is_some()
            || redirect.fragment().is_some()
            || !redirect.username().is_empty()
            || redirect.password().is_some()
        {
            return Err(Error::InvalidConfiguration);
        }
        Ok(Some(SocketAddr::new(
            IpAddr::V4(Ipv4Addr::LOCALHOST),
            redirect.port().ok_or(Error::InvalidConfiguration)?,
        )))
    }
}

impl Broker {
    pub fn from_environment() -> Result<Option<Self>, Error> {
        let raw = match std::env::var("HOMELAB_SECURITY_OAUTH_PROVIDERS") {
            Ok(raw) if !raw.is_empty() && raw.len() <= (128 << 10) => raw,
            Ok(raw) if raw.is_empty() => return Ok(None),
            Err(_) => return Ok(None),
            _ => return Err(Error::InvalidConfiguration),
        };
        let providers: Vec<Provider> =
            serde_json::from_str(&raw).map_err(|_| Error::InvalidConfiguration)?;
        if providers.is_empty() || providers.len() > 32 {
            return Err(Error::InvalidConfiguration);
        }
        let mut allowed = BTreeMap::new();
        let mut callback_address = None;
        for p in providers {
            let endpoint = validate_provider(&p)?;
            if let Some(endpoint) = endpoint {
                if callback_address.is_some_and(|existing| existing != endpoint) {
                    return Err(Error::InvalidConfiguration);
                }
                callback_address = Some(endpoint);
            }
            if allowed.insert(p.name.clone(), p).is_some() {
                return Err(Error::InvalidConfiguration);
            }
        }
        let http = Client::builder()
            .no_proxy()
            .redirect(reqwest::redirect::Policy::none())
            .timeout(Duration::from_secs(15))
            .connect_timeout(Duration::from_secs(5))
            .build()
            .map_err(|_| Error::InvalidConfiguration)?;
        Ok(Some(Self {
            providers: allowed,
            browser: BTreeMap::new(),
            device: BTreeMap::new(),
            callback_address,
            http,
        }))
    }

    pub fn callback_address(&self) -> Option<SocketAddr> {
        self.callback_address
    }

    pub fn provider_names(&self) -> Vec<String> {
        self.providers.keys().cloned().collect()
    }

    fn prune(&mut self) {
        let now = Instant::now();
        self.browser.retain(|_, item| item.expires > now);
        self.device.retain(|_, item| item.expires > now);
    }

    pub fn begin(&mut self, provider: &str, session_id: String) -> Result<BeginResult, Error> {
        self.prune();
        if self.browser.len() >= MAX_PENDING {
            return Err(Error::Unauthorized);
        }
        let selected = self
            .providers
            .get(provider)
            .ok_or(Error::InvalidArguments)?;
        if selected.authorize_url.is_empty() {
            return Err(Error::InvalidArguments);
        }
        let selected = selected.clone();
        let state = random_token(32)?;
        let verifier = Zeroizing::new(random_token(32)?);
        let digest = Sha256::digest(verifier.as_bytes());
        let challenge = URL_SAFE_NO_PAD.encode(digest);
        let mut url = safe_https(&selected.authorize_url)?;
        url.query_pairs_mut()
            .append_pair("response_type", "code")
            .append_pair("client_id", &selected.client_id)
            .append_pair("redirect_uri", &selected.redirect_url)
            .append_pair("scope", &selected.scopes.join(" "))
            .append_pair("state", &state)
            .append_pair("code_challenge", &challenge)
            .append_pair("code_challenge_method", "S256");
        self.browser.insert(
            state,
            BrowserFlow {
                provider: selected,
                session_id,
                verifier,
                expires: Instant::now() + Duration::from_secs(300),
            },
        );
        let expires_at = chrono::Utc::now()
            .checked_add_signed(chrono::Duration::seconds(300))
            .ok_or(Error::Storage)?
            .to_rfc3339_opts(chrono::SecondsFormat::Secs, true);
        Ok(BeginResult {
            url: url.to_string(),
            expires_at,
        })
    }

    fn take_browser(&mut self, state: &str) -> Result<BrowserFlow, Error> {
        self.prune();
        self.browser.remove(state).ok_or(Error::Unauthorized)
    }

    pub async fn token_request(
        http: &Client,
        provider: &Provider,
        form: &[(String, String)],
    ) -> Result<Zeroizing<Vec<u8>>, Error> {
        let response = http
            .post(safe_https(&provider.token_url)?)
            .form(form)
            .send()
            .await
            .map_err(|_| Error::Storage)?;
        if !response.status().is_success()
            || !response
                .headers()
                .get(reqwest::header::CONTENT_TYPE)
                .and_then(|v| v.to_str().ok())
                .is_some_and(|v| v.starts_with("application/json"))
        {
            return Err(Error::Unauthorized);
        }
        let mut response = response;
        let mut bytes = Zeroizing::new(Vec::new());
        while let Some(chunk) = response.chunk().await.map_err(|_| Error::Storage)? {
            if bytes.len() + chunk.len() > MAX_TOKEN_BYTES {
                return Err(Error::Unauthorized);
            }
            bytes.extend_from_slice(&chunk);
        }
        let data: Value = serde_json::from_slice(&bytes).map_err(|_| Error::Unauthorized)?;
        if data
            .get("access_token")
            .and_then(Value::as_str)
            .is_none_or(str::is_empty)
            || data
                .get("token_type")
                .and_then(Value::as_str)
                .is_none_or(|kind| !kind.eq_ignore_ascii_case("bearer"))
        {
            return Err(Error::Unauthorized);
        }
        Ok(bytes)
    }

    pub async fn device_begin(
        &mut self,
        provider: &str,
        session_id: String,
    ) -> Result<DeviceBeginResult, Error> {
        self.prune();
        let selected = self
            .providers
            .get(provider)
            .ok_or(Error::InvalidArguments)?
            .clone();
        if selected.device_authorization_url.is_empty() || self.device.len() >= MAX_PENDING {
            return Err(Error::Unauthorized);
        }
        let form: Vec<(String, String)> = vec![
            ("client_id".into(), selected.client_id.clone()),
            ("scope".into(), selected.scopes.join(" ")),
        ];
        let mut response = self
            .http
            .post(safe_https(&selected.device_authorization_url)?)
            .form(&form)
            .send()
            .await
            .map_err(|_| Error::Storage)?;
        if response.status() != reqwest::StatusCode::OK
            || !response
                .headers()
                .get(reqwest::header::CONTENT_TYPE)
                .and_then(|v| v.to_str().ok())
                .is_some_and(|v| v.starts_with("application/json"))
        {
            return Err(Error::Unauthorized);
        }
        let mut bytes = Zeroizing::new(Vec::new());
        while let Some(chunk) = response.chunk().await.map_err(|_| Error::Storage)? {
            if bytes.len() + chunk.len() > MAX_TOKEN_BYTES {
                return Err(Error::Unauthorized);
            }
            bytes.extend_from_slice(&chunk);
        }
        #[derive(Deserialize)]
        struct DeviceInstructions {
            device_code: String,
            user_code: String,
            verification_uri: String,
            #[serde(default)]
            verification_uri_complete: String,
            expires_in: u64,
            #[serde(default)]
            interval: u64,
        }
        let instructions: DeviceInstructions =
            serde_json::from_slice(&bytes).map_err(|_| Error::Unauthorized)?;
        if !(8..=MAX_CODE_BYTES).contains(&instructions.device_code.len())
            || instructions.user_code.is_empty()
            || instructions.user_code.len() > 128
            || !(30..=900).contains(&instructions.expires_in)
            || !verify_url(
                &instructions.verification_uri,
                &selected.device_verification_host,
            )
            || (!instructions.verification_uri_complete.is_empty()
                && !verify_url(
                    &instructions.verification_uri_complete,
                    &selected.device_verification_host,
                ))
        {
            return Err(Error::Unauthorized);
        }
        let interval = if instructions.interval == 0 {
            5
        } else {
            instructions.interval
        };
        if !(5..=60).contains(&interval) {
            return Err(Error::Unauthorized);
        }
        let id = random_token(24)?;
        let expires_at = chrono::Utc::now()
            .checked_add_signed(chrono::Duration::seconds(instructions.expires_in as i64))
            .ok_or(Error::Storage)?
            .to_rfc3339_opts(chrono::SecondsFormat::Secs, true);
        self.device.insert(
            id.clone(),
            DeviceFlow {
                provider: selected,
                session_id,
                device_code: Zeroizing::new(instructions.device_code),
                expires: Instant::now() + Duration::from_secs(instructions.expires_in),
                next_poll: Instant::now() + Duration::from_secs(interval),
                interval: Duration::from_secs(interval),
            },
        );
        Ok(DeviceBeginResult {
            id,
            verification_url: instructions.verification_uri,
            verification_url_complete: (!instructions.verification_uri_complete.is_empty())
                .then_some(instructions.verification_uri_complete),
            user_code: instructions.user_code,
            expires_at,
            poll_after_seconds: interval,
        })
    }

    pub async fn device_poll(
        &mut self,
        provider: &str,
        id: &str,
    ) -> Result<(DeviceStatus, Option<(String, Zeroizing<Vec<u8>>)>), Error> {
        self.prune();
        let flow = self.device.get(id).ok_or(Error::Unauthorized)?;
        if flow.provider.name != provider {
            return Err(Error::Unauthorized);
        }
        let now = Instant::now();
        if now < flow.next_poll {
            return Ok((
                DeviceStatus {
                    state: "pending".into(),
                    retry_after_seconds: Some((flow.next_poll - now).as_secs().max(1)),
                    credential: None,
                },
                None,
            ));
        }
        let mut flow = self.device.remove(id).ok_or(Error::Unauthorized)?;
        let form: Vec<(String, String)> = vec![
            (
                "grant_type".into(),
                "urn:ietf:params:oauth:grant-type:device_code".into(),
            ),
            ("device_code".into(), flow.device_code.to_string()),
            ("client_id".into(), flow.provider.client_id.clone()),
        ];
        let request = self
            .http
            .post(safe_https(&flow.provider.token_url)?)
            .form(&form)
            .send()
            .await
            .map_err(|_| Error::Storage)?;
        let status = request.status();
        let content_type = request
            .headers()
            .get(reqwest::header::CONTENT_TYPE)
            .and_then(|v| v.to_str().ok())
            .unwrap_or("")
            .to_owned();
        let mut request = request;
        let mut bytes = Zeroizing::new(Vec::new());
        while let Some(chunk) = request.chunk().await.map_err(|_| Error::Storage)? {
            if bytes.len() + chunk.len() > MAX_TOKEN_BYTES {
                return Err(Error::Unauthorized);
            }
            bytes.extend_from_slice(&chunk);
        }
        if !content_type.starts_with("application/json") {
            return Err(Error::Unauthorized);
        }
        if status == reqwest::StatusCode::OK {
            let token: Value = serde_json::from_slice(&bytes).map_err(|_| Error::Unauthorized)?;
            if token
                .get("access_token")
                .and_then(Value::as_str)
                .is_none_or(str::is_empty)
                || token
                    .get("token_type")
                    .and_then(Value::as_str)
                    .is_none_or(|v| !v.eq_ignore_ascii_case("bearer"))
            {
                return Err(Error::Unauthorized);
            }
            return Ok((
                DeviceStatus {
                    state: "authorized".into(),
                    retry_after_seconds: None,
                    credential: None,
                },
                Some((flow.session_id, bytes)),
            ));
        }
        let response: Value = serde_json::from_slice(&bytes).map_err(|_| Error::Unauthorized)?;
        let error = response
            .get("error")
            .and_then(Value::as_str)
            .ok_or(Error::Unauthorized)?;
        if error != "authorization_pending" && error != "slow_down" {
            return Err(Error::Unauthorized);
        }
        if error == "slow_down" {
            flow.interval += Duration::from_secs(5);
        }
        flow.next_poll = Instant::now() + flow.interval;
        let wait = flow.interval.as_secs();
        self.device.insert(id.to_owned(), flow);
        Ok((
            DeviceStatus {
                state: "pending".into(),
                retry_after_seconds: Some(wait),
                credential: None,
            },
            None,
        ))
    }

    pub fn take_refresh_provider(&self, provider: &str) -> Result<Provider, Error> {
        self.providers
            .get(provider)
            .cloned()
            .ok_or(Error::Unauthorized)
    }

    pub fn client(&self) -> Client {
        self.http.clone()
    }
}

pub type SharedBroker = Arc<Mutex<Broker>>;

#[derive(Deserialize)]
#[serde(deny_unknown_fields)]
struct Factors {
    password: String,
    totp: String,
}
#[derive(Deserialize)]
#[serde(deny_unknown_fields)]
struct DevicePoll {
    id: String,
}
#[derive(Deserialize)]
#[serde(deny_unknown_fields)]
struct Refresh {
    handle: String,
    password: String,
    totp: String,
    confirm: bool,
}

fn failure(message: &'static str, code: StatusCode) -> Response {
    (code, message).into_response()
}
async fn oauth_providers(State(shared): State<Shared>) -> Response {
    let Some(broker) = &shared.oauth else {
        return failure("OAuth unavailable", StatusCode::SERVICE_UNAVAILABLE);
    };
    let names = broker.lock().await.provider_names();
    Json(json!({"providers":names})).into_response()
}
async fn oauth_begin(
    State(shared): State<Shared>,
    RoutePath(provider): RoutePath<String>,
    headers: HeaderMap,
    raw: Bytes,
) -> Response {
    let Some(broker) = &shared.oauth else {
        return failure("OAuth unavailable", StatusCode::SERVICE_UNAVAILABLE);
    };
    let Ok(factors) = json_input::<Factors>(&headers, &raw) else {
        return failure("invalid owner request", StatusCode::BAD_REQUEST);
    };
    let password = Zeroizing::new(factors.password);
    let totp = Zeroizing::new(factors.totp);
    let owner = shared.with_authority(|auth| {
        auth.verify_owner_factors(&password, &totp)?;
        auth.local_session_id()
    });
    let Ok(session_id) = owner else {
        return failure("owner authentication required", StatusCode::FORBIDDEN);
    };
    match broker.lock().await.begin(&provider, session_id) {
        Ok(auth) => Json(auth).into_response(),
        Err(_) => failure("OAuth provider unavailable", StatusCode::BAD_REQUEST),
    }
}

async fn oauth_device_begin(
    State(shared): State<Shared>,
    RoutePath(provider): RoutePath<String>,
    headers: HeaderMap,
    raw: Bytes,
) -> Response {
    let Some(broker) = &shared.oauth else {
        return failure("OAuth unavailable", StatusCode::SERVICE_UNAVAILABLE);
    };
    let Ok(factors) = json_input::<Factors>(&headers, &raw) else {
        return failure("invalid owner request", StatusCode::BAD_REQUEST);
    };
    let password = Zeroizing::new(factors.password);
    let totp = Zeroizing::new(factors.totp);
    let owner = shared.with_authority(|auth| {
        auth.verify_owner_factors(&password, &totp)?;
        auth.local_session_id()
    });
    let Ok(session_id) = owner else {
        return failure("owner authentication required", StatusCode::FORBIDDEN);
    };
    match broker
        .lock()
        .await
        .device_begin(&provider, session_id)
        .await
    {
        Ok(instructions) => Json(instructions).into_response(),
        Err(_) => failure(
            "device authorization unavailable",
            StatusCode::SERVICE_UNAVAILABLE,
        ),
    }
}

async fn oauth_device_poll(
    State(shared): State<Shared>,
    RoutePath(provider): RoutePath<String>,
    headers: HeaderMap,
    raw: Bytes,
) -> Response {
    let Some(broker) = &shared.oauth else {
        return failure("OAuth unavailable", StatusCode::SERVICE_UNAVAILABLE);
    };
    let Ok(input) = json_input::<DevicePoll>(&headers, &raw) else {
        return failure("invalid device status request", StatusCode::BAD_REQUEST);
    };
    let result = broker.lock().await.device_poll(&provider, &input.id).await;
    match result {
        Ok((mut status, Some((session, token)))) => {
            let stored = shared
                .with_authority(|auth| auth.store_broker_credential(&session, &provider, &token));
            match stored {
                Ok(handle) => {
                    status.credential = Some(handle);
                    Json(status).into_response()
                }
                Err(_) => failure(
                    "device credential could not be secured",
                    StatusCode::FORBIDDEN,
                ),
            }
        }
        Ok((status, None)) => Json(status).into_response(),
        Err(_) => failure("device authorization refused", StatusCode::BAD_REQUEST),
    }
}

fn merge_refreshed_credential(previous: &Value, response: &[u8]) -> Result<Value, Error> {
    let mut merged = previous.as_object().cloned().ok_or(Error::Unauthorized)?;
    let new_values: Value = serde_json::from_slice(response).map_err(|_| Error::Unauthorized)?;
    let updates = new_values.as_object().ok_or(Error::Unauthorized)?;
    for (name, value) in updates {
        if name == "refresh_token" && value.as_str().is_some_and(str::is_empty) {
            continue;
        }
        merged.insert(name.clone(), value.clone());
    }
    merged.remove("error");
    Ok(Value::Object(merged))
}

async fn oauth_refresh(
    State(shared): State<Shared>,
    RoutePath(provider): RoutePath<String>,
    headers: HeaderMap,
    raw: Bytes,
) -> Response {
    let Some(broker) = &shared.oauth else {
        return failure("OAuth unavailable", StatusCode::SERVICE_UNAVAILABLE);
    };
    let Ok(input) = json_input::<Refresh>(&headers, &raw) else {
        return failure("invalid refresh request", StatusCode::BAD_REQUEST);
    };
    if !input.confirm {
        return failure(
            "explicit refresh confirmation required",
            StatusCode::BAD_REQUEST,
        );
    }
    let password = Zeroizing::new(input.password);
    let totp = Zeroizing::new(input.totp);
    let owner = shared.with_authority(|auth| {
        auth.verify_owner_factors(&password, &totp)?;
        auth.local_session_id()
    });
    let Ok(session) = owner else {
        return failure("owner authentication required", StatusCode::FORBIDDEN);
    };
    let (provider_config, client) = {
        let broker = broker.lock().await;
        let Ok(config) = broker.take_refresh_provider(&provider) else {
            return failure("OAuth provider unavailable", StatusCode::NOT_FOUND);
        };
        (config, broker.client())
    };
    let snapshot = shared
        .with_authority(|auth| auth.broker_credential_snapshot(&session, &input.handle, &provider));
    let Ok((secret, revision)) = snapshot else {
        return failure("credential unavailable", StatusCode::FORBIDDEN);
    };
    let Ok(existing) = serde_json::from_slice::<Value>(&secret) else {
        return failure("invalid stored token", StatusCode::FORBIDDEN);
    };
    let Some(refresh) = existing.get("refresh_token").and_then(Value::as_str) else {
        return failure("credential cannot be refreshed", StatusCode::BAD_REQUEST);
    };
    let form = vec![
        ("grant_type".into(), "refresh_token".into()),
        ("refresh_token".into(), refresh.into()),
        ("client_id".into(), provider_config.client_id.clone()),
    ];
    let replacement = Broker::token_request(&client, &provider_config, &form).await;
    let Ok(token) = replacement else {
        return failure("OAuth refresh failed", StatusCode::SERVICE_UNAVAILABLE);
    };
    // RFC 6749 permits refresh responses without a replacement refresh
    // token. Retain existing provider metadata unless explicitly replaced.
    let response = match merge_refreshed_credential(&existing, &token) {
        Ok(value) => value,
        Err(_) => {
            return failure(
                "invalid OAuth refresh response",
                StatusCode::SERVICE_UNAVAILABLE,
            )
        }
    };
    let secured = match serde_json::to_vec(&response) {
        Ok(raw) => Zeroizing::new(raw),
        Err(_) => {
            return failure(
                "invalid OAuth refresh response",
                StatusCode::SERVICE_UNAVAILABLE,
            )
        }
    };
    match shared.with_authority(|auth| {
        auth.replace_broker_credential(&session, &input.handle, &provider, &revision, &secured)
    }) {
        Ok(()) => Json(json!({"refreshed":true})).into_response(),
        Err(_) => failure(
            "OAuth credential custody unavailable",
            StatusCode::FORBIDDEN,
        ),
    }
}

pub fn register_owner_routes(router: Router<Shared>) -> Router<Shared> {
    router
        .route("/v1/oauth/providers", get(oauth_providers))
        .route("/v1/oauth/{provider}/begin", post(oauth_begin))
        .route(
            "/v1/oauth/{provider}/device/begin",
            post(oauth_device_begin),
        )
        .route("/v1/oauth/{provider}/device/poll", post(oauth_device_poll))
        .route("/v1/oauth/{provider}/refresh", post(oauth_refresh))
}

async fn oauth_callback(
    State(shared): State<Shared>,
    headers: HeaderMap,
    raw: axum::extract::RawQuery,
) -> Response {
    let Some(broker) = &shared.oauth else {
        return failure("OAuth unavailable", StatusCode::NOT_FOUND);
    };
    let (address, http) = {
        let broker = broker.lock().await;
        (broker.callback_address(), broker.client())
    };
    let Some(address) = address else {
        return failure("OAuth callback not configured", StatusCode::NOT_FOUND);
    };
    if headers.get("host").and_then(|v| v.to_str().ok()) != Some(address.to_string().as_str()) {
        return failure("wrong OAuth callback host", StatusCode::FORBIDDEN);
    }
    let Some(query) = raw.0 else {
        return failure(
            "OAuth callback parameters required",
            StatusCode::BAD_REQUEST,
        );
    };
    if query.len() > 8192 {
        return failure("OAuth callback exceeds size limit", StatusCode::BAD_REQUEST);
    }
    let parameters: Vec<_> = url::form_urlencoded::parse(query.as_bytes()).collect();
    if parameters.len() != 2
        || parameters.iter().filter(|(name, _)| name == "code").count() != 1
        || parameters
            .iter()
            .filter(|(name, _)| name == "state")
            .count()
            != 1
    {
        return failure("invalid OAuth callback", StatusCode::BAD_REQUEST);
    }
    let code = parameters
        .iter()
        .find(|(name, _)| name == "code")
        .map(|(_, value)| value.as_ref())
        .unwrap_or("");
    let state = parameters
        .iter()
        .find(|(name, _)| name == "state")
        .map(|(_, value)| value.as_ref())
        .unwrap_or("");
    if code.is_empty() || code.len() > MAX_CODE_BYTES || state.len() != 43 {
        return failure("invalid OAuth callback", StatusCode::BAD_REQUEST);
    }
    let flow = {
        let mut broker = broker.lock().await;
        broker.take_browser(state)
    };
    let Ok(flow) = flow else {
        return failure(
            "unknown or expired OAuth authorization",
            StatusCode::FORBIDDEN,
        );
    };
    let form = vec![
        ("grant_type".into(), "authorization_code".into()),
        ("code".into(), code.into()),
        ("redirect_uri".into(), flow.provider.redirect_url.clone()),
        ("client_id".into(), flow.provider.client_id.clone()),
        ("code_verifier".into(), flow.verifier.to_string()),
    ];
    let Ok(credential) = Broker::token_request(&http, &flow.provider, &form).await else {
        return failure("OAuth token exchange rejected", StatusCode::FORBIDDEN);
    };
    let stored = shared.with_authority(|auth| {
        auth.store_broker_credential(&flow.session_id, &flow.provider.name, &credential)
    });
    if stored.is_err() {
        return failure(
            "OAuth credential custody unavailable",
            StatusCode::FORBIDDEN,
        );
    }
    Json(json!({"connected":true,"message":"Provider authorization completed in the independent credential service."})).into_response()
}

pub async fn serve_callback(
    broker: SharedBroker,
    shared: Shared,
) -> Result<(), Box<dyn std::error::Error + Send + Sync>> {
    let Some(address) = broker.lock().await.callback_address() else {
        return Ok(());
    };
    let listener = tokio::net::TcpListener::bind(address).await?;
    let router = Router::new()
        .route("/oauth/callback", get(oauth_callback))
        .with_state(shared);
    axum::serve(listener, router).await?;
    Ok(())
}

#[cfg(test)]
mod tests {
    use super::*;
    #[test]
    fn retains_refresh_grant_and_provider_metadata() {
        let stored = json!({
            "access_token":"old", "refresh_token":"refresh-a",
            "token_type":"Bearer", "scope":"read", "error":"stale"
        });
        let refreshed =
            merge_refreshed_credential(&stored, br#"{"access_token":"new","token_type":"Bearer"}"#)
                .unwrap();
        assert_eq!(refreshed["access_token"], "new");
        assert_eq!(refreshed["refresh_token"], "refresh-a");
        assert_eq!(refreshed["scope"], "read");
        assert!(refreshed.get("error").is_none());

        let changed = merge_refreshed_credential(
            &refreshed,
            br#"{"access_token":"newer","refresh_token":"refresh-b","token_type":"Bearer"}"#,
        )
        .unwrap();
        assert_eq!(changed["refresh_token"], "refresh-b");
    }

    #[test]
    fn oauth_endpoints_require_public_dns_hosts() {
        for endpoint in [
            "https://127.0.0.1/token",
            "https://[::1]/token",
            "https://localhost/token",
            "https://metadata.internal/token",
            "https://identity.local/token",
            "https://example.invalid/token?redirect=1",
            "https://user@example.invalid/token",
        ] {
            assert!(
                safe_https(endpoint).is_err(),
                "unsafe OAuth endpoint: {endpoint}"
            );
        }
        assert!(safe_https("https://example.invalid/token").is_ok());
        assert!(!verify_url("https://127.0.0.1/verify", "127.0.0.1"));
        assert!(verify_url(
            "https://example.invalid/verify",
            "example.invalid"
        ));
    }

    #[test]
    fn provider_policy_rejects_nonloopback_callbacks_and_untrusted_endpoints() {
        let normal = Provider {
            name: "research".into(),
            authorize_url: "https://example.invalid/authorize".into(),
            token_url: "https://example.invalid/token".into(),
            client_id: "public-client".into(),
            redirect_url: "http://127.0.0.1:40234/oauth/callback".into(),
            device_authorization_url: String::new(),
            device_verification_host: String::new(),
            scopes: vec!["read".into()],
        };
        assert!(validate_provider(&normal).unwrap().is_some());
        let mut unsafe_provider = normal.clone();
        unsafe_provider.redirect_url = "http://0.0.0.0:40234/oauth/callback".into();
        assert!(validate_provider(&unsafe_provider).is_err());
        unsafe_provider = normal.clone();
        unsafe_provider.token_url = "http://example.invalid/token".into();
        assert!(validate_provider(&unsafe_provider).is_err());
    }
}
