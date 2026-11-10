//! JSON-RPC transport for an MCP Apps sandboxed iframe.
//! The iframe receives no backend token; the host mediates tool calls.
use std::cell::RefCell;
use std::collections::HashMap;
use std::rc::Rc;

use dioxus::prelude::*;
use futures_channel::oneshot;
use gloo_timers::callback::Timeout;
use js_sys::{JSON, Object};
use serde::de::DeserializeOwned;
use serde_json::{Value, json};
use wasm_bindgen::{JsCast, JsValue, closure::Closure};
use web_sys::{MessageEvent, Window};

use crate::components::{Loading, Notice};

const PROTOCOL: &str = "2026-01-26";
const MAX_REQUEST: usize = 256 * 1024;
const MAX_REPLY: usize = 4 * 1024 * 1024;
const MAX_PENDING: usize = 32;


fn decode_response(raw: &str) -> Option<(u32, Result<Value, String>)> {
    if raw.len() > MAX_REPLY { return None; }
    let message: Value = serde_json::from_str(raw).ok()?;
    if message.get("jsonrpc").and_then(Value::as_str) != Some("2.0") {
        return None;
    }
    let id = message.get("id").and_then(Value::as_u64)
        .and_then(|value| u32::try_from(value).ok())?;
    let response = if let Some(error) = message.get("error") {
        let message = error.get("message").and_then(Value::as_str)
            .unwrap_or("Host rejected the request");
        Err(message.chars().take(240).collect())
    } else if let Some(result) = message.get("result") {
        Ok(result.clone())
    } else {
        Err("Host response has no result".to_owned())
    };
    Some((id, response))
}

struct Pending {
    reply: oneshot::Sender<Result<Value, String>>,
    _timeout: Timeout,
}

struct Bridge {
    parent: Window,
    current: Window,
    _listener: Closure<dyn FnMut(MessageEvent)>,
    pending: Rc<RefCell<HashMap<u32, Pending>>>,
    next_id: u32,
}

impl Bridge {
    fn create() -> Result<Self, String> {
        let current = web_sys::window().ok_or("Browser window unavailable")?;
        let parent = current.parent().map_err(|_| "MCP Apps parent unavailable")?
            .ok_or("MCP Apps host is missing")?;
        if Object::is(current.as_ref(), parent.as_ref()) {
            return Err("MCP Apps requires an embedded host".to_owned());
        }
        let pending = Rc::new(RefCell::new(HashMap::<u32, Pending>::new()));
        let pending_messages = pending.clone();
        let expected_source = parent.clone();
        let listener = Closure::<dyn FnMut(MessageEvent)>::new(move |event| {
            let Some(source) = event.source() else { return };
            if !Object::is(&JsValue::from(source), expected_source.as_ref()) {
                return;
            }
            let Ok(raw) = JSON::stringify(&event.data()) else { return };
            let Some(raw) = raw.as_string() else { return };
            let Some((id, response)) = decode_response(&raw) else { return };
            let Some(pending) = pending_messages.borrow_mut().remove(&id) else { return };
            let _ = pending.reply.send(response);
        });
        current.add_event_listener_with_callback("message", listener.as_ref().unchecked_ref())
            .map_err(|_| "MCP Apps message listener unavailable")?;
        Ok(Self { parent, current, _listener: listener, pending, next_id: 1 })
    }

    fn send(&mut self, method: &str, params: Value) -> Result<oneshot::Receiver<Result<Value, String>>, String> {
        if self.pending.borrow().len() >= MAX_PENDING {
            return Err("Too many pending MCP Apps requests".to_owned());
        }
        let id = self.next_id;
        self.next_id = self.next_id.checked_add(1)
            .ok_or_else(|| "MCP Apps request limit reached".to_owned())?;
        let request = json!({"jsonrpc":"2.0","id":id,"method":method,"params":params});
        let text = request.to_string();
        if text.len() > MAX_REQUEST { return Err("MCP Apps request is too large".to_owned()); }
        let payload = JSON::parse(&text)
            .map_err(|_| "Unable to encode MCP Apps request")?;
        let (sender, receiver) = oneshot::channel();
        let pending_messages = self.pending.clone();
        let timer = Timeout::new(12_000, move || {
            if let Some(pending) = pending_messages.borrow_mut().remove(&id) {
                let _ = pending.reply.send(Err("MCP Apps host did not respond".to_owned()));
            }
        });
        self.pending.borrow_mut().insert(id, Pending { reply: sender, _timeout: timer });
        if self.parent.post_message(&payload, "*").is_err() {
            self.pending.borrow_mut().remove(&id);
            return Err("MCP Apps host connection failed".to_owned());
        }
        Ok(receiver)
    }

    fn notify(&self, method: &str) -> Result<(), String> {
        let payload = JSON::parse(&json!({"jsonrpc":"2.0","method":method}).to_string())
            .map_err(|_| "Unable to encode MCP Apps notification")?;
        self.parent.post_message(&payload, "*")
            .map_err(|_| "MCP Apps notification failed".to_owned())
    }
}

impl Drop for Bridge {
    fn drop(&mut self) {
        let _ = self.current.remove_event_listener_with_callback(
            "message", self._listener.as_ref().unchecked_ref()
        );
        self.pending.borrow_mut().clear();
    }
}

thread_local! {
    static BRIDGE: RefCell<Option<Bridge>> = const { RefCell::new(None) };
}

pub async fn request(method: &str, params: Value) -> Result<Value, String> {
    let receiver = BRIDGE.with(|cell| {
        let mut state = cell.borrow_mut();
        if state.is_none() { *state = Some(Bridge::create()?); }
        state.as_mut().ok_or("MCP Apps bridge missing")?.send(method, params)
    })?;
    receiver.await
        .map_err(|_| "MCP Apps request was interrupted".to_owned())?
}

fn notify_initialized() -> Result<(), String> {
    BRIDGE.with(|cell| {
        let state = cell.borrow();
        state.as_ref().ok_or("MCP Apps bridge missing")?
            .notify("ui/notifications/initialized")
    })
}

pub async fn initialize() -> Result<(), String> {
    let result = request("ui/initialize", json!({
        "appInfo":{"name":"Homelab Health","version":"0.1.0"},
        "appCapabilities":{"availableDisplayModes":["inline","fullscreen"]},
        "protocolVersion":PROTOCOL
    })).await?;
    if result.get("protocolVersion").and_then(Value::as_str) != Some(PROTOCOL) {
        return Err("MCP Apps host did not confirm protocol initialization".to_owned());
    }
    notify_initialized()
}

pub async fn call_tool<T: DeserializeOwned>(name: &str, arguments: Value) -> Result<T, String> {
    if !name.starts_with("health_") {
        return Err("MCP Apps cannot invoke another service namespace".to_owned());
    }
    let result = request("tools/call", json!({"name":name,"arguments":arguments})).await?;
    if result.get("isError").and_then(Value::as_bool) == Some(true) {
        let message = result.pointer("/content/0/text").and_then(Value::as_str)
            .unwrap_or("Health rejected the request");
        return Err(message.chars().take(240).collect());
    }
    let value = if let Some(content) = result.get("structuredContent") {
        content.clone()
    } else {
        let text = result.pointer("/content/0/text").and_then(Value::as_str)
            .ok_or("Health tool returned no data")?;
        if text.len() > MAX_REPLY {
            return Err("Health tool response is too large".to_owned());
        }
        serde_json::from_str(text).map_err(|_| "Health returned invalid JSON")?
    };
    serde_json::from_value(value).map_err(|_| "Health returned an incompatible response".to_owned())
}

fn selected_view() -> Option<String> {
    let document = web_sys::window()?.document()?;
    document.query_selector("meta[name=\"homelab-mcp-view\"]").ok()??
        .get_attribute("content")
}

#[component]
pub fn Embedded() -> Element {
    let mut ready = use_signal(|| false);
    let mut error = use_signal(String::new);
    let selected = selected_view();
    use_future(move || async move {
        match initialize().await {
            Ok(()) => ready.set(true),
            Err(message) => error.set(message),
        }
    });
    rsx! {
        document::Stylesheet { href: crate::CSS }
        if !error().is_empty() {
            Notice { message: error() }
        } else if !ready() {
            Loading {}
        } else {
            match selected.as_deref() {
                Some("cognitive") => rsx! { crate::cognitive::ReactionTime { csrf: String::new() } },
                Some("dashboard") => rsx! { crate::health_dashboard::HealthDashboard { csrf: String::new() } },
                Some("workout-editor") => rsx! { crate::workout_planner::WorkoutPlanner { csrf: String::new() } },
                Some("groceries") => rsx! { crate::health_groceries::HealthGroceries { csrf: String::new() } },
                Some("workout-companion") => rsx! { crate::workout_companion::WorkoutCompanion { csrf: String::new() } },
                _ => rsx! { Notice { message: "Unknown Health App resource".to_owned() } },
            }
        }
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn host_requests_are_bounded() {
        assert_eq!(PROTOCOL, "2026-01-26");
        assert!(MAX_PENDING <= 32);
        assert!(MAX_REQUEST <= 256 * 1024);
        assert!(MAX_REPLY <= 4 * 1024 * 1024);
    }


    #[test]
    fn response_parser_rejects_notifications_and_malformed_envelopes() {
        for value in [
            r#"{"jsonrpc":"2.0","method":"ui/notifications/host-context-changed"}"#,
            r#"{"jsonrpc":"1.0","id":1,"result":{}}"#,
            r#"{"id":1,"result":{}}"#,
            r#"{"jsonrpc":"2.0","id":"one","result":{}}"#,
            r#"{"jsonrpc":"2.0","id":4294967296,"result":{}}"#,
        ] {
            assert!(decode_response(value).is_none(), "{value}");
        }
        let (id, value) = decode_response(
            r#"{"jsonrpc":"2.0","id":7,"result":{"protocolVersion":"2026-01-26"}}"#
        ).expect("valid response");
        assert_eq!(id, 7);
        assert_eq!(value.unwrap()["protocolVersion"], PROTOCOL);
        let (_, failure) = decode_response(
            r#"{"jsonrpc":"2.0","id":3,"error":{"code":-32601,"message":"not found"}}"#
        ).expect("valid error response");
        assert_eq!(failure.unwrap_err(), "not found");
    }
}
