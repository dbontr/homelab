use axum::{
    Json, Router,
    extract::{Request, State},
    http::{StatusCode, header},
    middleware::{self, Next},
    response::{IntoResponse, Response},
    routing::{get, post, put},
};
use homelab_motivation::{
    Engine, Error, FeedbackInput, Intervention, ProfileChange, Record, unix_seconds,
};
use serde_json::{Value, json};
use std::{
    env, fs,
    net::SocketAddr,
    os::unix::fs::{OpenOptionsExt, PermissionsExt},
    io::Read,
    path::Path,
    sync::{Arc, Mutex},
};
use subtle::ConstantTimeEq;

#[derive(Clone)]
struct App {
    engine: Arc<Mutex<Engine>>,
    bearer: Arc<str>,
}

struct ApiError(StatusCode, &'static str);
impl IntoResponse for ApiError {
    fn into_response(self) -> Response {
        (self.0, Json(json!({"error": self.1}))).into_response()
    }
}
fn map_error(err: Error) -> ApiError {
    match err {
        Error::Invalid(_) => ApiError(StatusCode::BAD_REQUEST, "invalid motivation request"),
        Error::Conflict(_) => ApiError(StatusCode::CONFLICT, "motivation state conflict"),
        Error::Limited(_) => ApiError(StatusCode::TOO_MANY_REQUESTS, "motivation intervention limited"),
        Error::Io(_) => ApiError(StatusCode::SERVICE_UNAVAILABLE, "motivation state unavailable"),
    }
}
fn with_engine<T>(
    app: &App, action: impl FnOnce(&mut Engine) -> Result<T, Error>,
) -> Result<T, ApiError> {
    let mut lock = app.engine.lock().map_err(|_| ApiError(
        StatusCode::SERVICE_UNAVAILABLE, "motivation state unavailable",
    ))?;
    action(&mut lock).map_err(map_error)
}
async fn authenticate(
    State(app): State<App>, req: Request, next: Next,
) -> Response {
    let expected = format!("Bearer {}", app.bearer);
    let presented = req.headers().get(header::AUTHORIZATION)
        .and_then(|value| value.to_str().ok()).unwrap_or("");
    if presented.len() != expected.len() ||
        !bool::from(presented.as_bytes().ct_eq(expected.as_bytes())) {
        return ApiError(StatusCode::UNAUTHORIZED, "authorization required").into_response();
    }
    next.run(req).await
}
async fn read(State(app): State<App>) -> Result<Json<Record>, ApiError> {
    with_engine(&app, |engine| Ok(engine.snapshot())).map(Json)
}
async fn profile(
    State(app): State<App>, Json(change): Json<ProfileChange>,
) -> Result<Json<Record>, ApiError> {
    with_engine(&app, |engine| engine.update_profile(change)).map(Json)
}
async fn pause(State(app): State<App>) -> Result<Json<Record>, ApiError> {
    with_engine(&app, |engine| engine.pause()).map(Json)
}
async fn suggest(State(app): State<App>) -> Result<Json<Intervention>, ApiError> {
    with_engine(&app, |engine| engine.suggest(unix_seconds())).map(Json)
}
async fn feedback(
    State(app): State<App>, Json(input): Json<FeedbackInput>,
) -> Result<Json<Record>, ApiError> {
    with_engine(&app, |engine| engine.record_feedback(input, unix_seconds())).map(Json)
}
async fn healthz() -> Json<Value> { Json(json!({"status":"ok"})) }

fn read_token(path: &Path) -> Result<String, Box<dyn std::error::Error>> {
    if !path.is_absolute() { return Err("token file must be absolute".into()); }
    let metadata = fs::symlink_metadata(path)?;
    if !metadata.is_file() || metadata.file_type().is_symlink() ||
        metadata.permissions().mode() & 0o077 != 0 {
        return Err("token file must be a private regular file".into());
    }
    let file = fs::OpenOptions::new().read(true)
        .custom_flags(libc::O_NOFOLLOW).open(path)?;
    if !file.metadata()?.is_file() || file.metadata()?.permissions().mode() & 0o077 != 0 {
        return Err("token descriptor is not a private regular file".into());
    }
    let mut raw = String::new();
    file.take(4097).read_to_string(&mut raw)?;
    let token = raw.trim().to_owned();
    if token.len() < 32 || token.len() > 4096 ||
        token.bytes().any(|b| b <= 32 || b == 127) {
        return Err("invalid service token".into());
    }
    Ok(token)
}

#[tokio::main]
async fn main() -> Result<(), Box<dyn std::error::Error>> {
    let token_path = env::var("HOMELAB_MOTIVATION_TOKEN_FILE")?;
    let token = read_token(Path::new(&token_path))?;
    let state_path = env::var("HOMELAB_MOTIVATION_STATE_FILE")
        .unwrap_or_else(|_| "/var/lib/homelab-motivation/state.json".to_owned());
    let engine = Engine::open(state_path)?;
    let listen: SocketAddr = env::var("HOMELAB_MOTIVATION_LISTEN_ADDR")
        .unwrap_or_else(|_| "127.0.0.1:8341".to_owned()).parse()?;
    if !listen.ip().is_loopback() {
        return Err("motivation service must listen on loopback".into());
    }
    let app = App { engine: Arc::new(Mutex::new(engine)), bearer: token.into() };
    let protected = Router::new()
        .route("/v1/motivation/state", get(read))
        .route("/v1/motivation/profile", put(profile))
        .route("/v1/motivation/pause", post(pause))
        .route("/v1/motivation/interventions", post(suggest))
        .route("/v1/motivation/feedback", post(feedback))
        .route_layer(middleware::from_fn_with_state(app.clone(), authenticate));
    let router = Router::new().merge(protected)
        .route("/healthz", get(healthz))
        .with_state(app);
    let listener = tokio::net::TcpListener::bind(listen).await?;
    axum::serve(listener, router).await?;
    Ok(())
}
