//! JSON-RPC transport for an MCP Apps sandboxed iframe.
//! The iframe receives no backend token; the host mediates tool calls.
use std::cell::{Cell, RefCell};
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
const MAX_FILE_TRANSFER: usize = 8 * 1024 * 1024;
const MAX_PENDING: usize = 32;


fn decode_response(raw: &str) -> Option<(u32, Result<Value, String>)> {
    if raw.len() > MAX_FILE_TRANSFER { return None; }
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

fn decode_host_result(raw:&str)->Option<Value>{
    if raw.len()>MAX_REPLY {return None}
    let message:Value=serde_json::from_str(raw).ok()?;
    if message.get("jsonrpc").and_then(Value::as_str)!=Some("2.0")||
        message.get("method").and_then(Value::as_str)!=Some("ui/notifications/tool-result"){
        return None;
    }
    let params=message.get("params")?;
    if params.get("isError").and_then(Value::as_bool)==Some(true){
        return None;
    }
    if let Some(data)=params.get("structuredContent"){
        return Some(data.clone());
    }
    let text=params.pointer("/content/0/text")?.as_str()?;
    serde_json::from_str(text).ok()
}

struct Pending {
    reply: oneshot::Sender<Result<Value, String>>,
    max_reply: usize,
    _timeout: Timeout,
}

struct Bridge {
    parent: Window,
    current: Window,
    _listener: Closure<dyn FnMut(MessageEvent)>,
    pending: Rc<RefCell<HashMap<u32, Pending>>>,
    host_result: Rc<RefCell<Option<Value>>>,
    closed: Rc<Cell<bool>>,
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
        let host_result = Rc::new(RefCell::new(None::<Value>));
        let observed_result = host_result.clone();
        let closed = Rc::new(Cell::new(false));
        let closed_for_listener = closed.clone();
        let expected_source = parent.clone();
        let reply_destination = parent.clone();
        let listener = Closure::<dyn FnMut(MessageEvent)>::new(move |event| {
            let Some(source) = event.source() else { return };
            if !Object::is(&JsValue::from(source), expected_source.as_ref()) {
                return;
            }
            let Ok(raw) = JSON::stringify(&event.data()) else { return };
            let Some(raw) = raw.as_string() else { return };
            if raw.len() > MAX_FILE_TRANSFER { return; }
            if let Ok(value) = serde_json::from_str::<Value>(&raw) {
                if value.get("jsonrpc").and_then(Value::as_str)==Some("2.0")
                    && value.get("method").and_then(Value::as_str)==Some("ui/resource-teardown") {
                    if let Some(id)=value.get("id").filter(|id|
                        id.is_number() || id.is_string()) {
                        let response=json!({"jsonrpc":"2.0","id":id,"result":{}});
                        if let Ok(reply)=JSON::parse(&response.to_string()) {
                            let _=reply_destination.post_message(&reply,"*");
                        }
                    }
                    closed_for_listener.set(true);
                    pending_messages.borrow_mut().clear();
                    observed_result.replace(None);
                    return;
                }
            }
            if closed_for_listener.get(){return}
            if let Some(value) = decode_host_result(&raw) {
                observed_result.replace(Some(value));
                return;
            }
            let Some((id, response)) = decode_response(&raw) else { return };
            let Some(pending) = pending_messages.borrow_mut().remove(&id) else { return };
            if raw.len() > pending.max_reply {
                let _ = pending.reply.send(Err("MCP Apps response exceeds its operation limit".to_owned()));
                return;
            }
            let _ = pending.reply.send(response);
        });
        current.add_event_listener_with_callback("message", listener.as_ref().unchecked_ref())
            .map_err(|_| "MCP Apps message listener unavailable")?;
        Ok(Self { parent, current, _listener: listener, pending, host_result, closed, next_id: 1 })
    }

    fn send(&mut self, method: &str, params: Value) -> Result<oneshot::Receiver<Result<Value, String>>, String> {
        if self.closed.get() {
            return Err("MCP Apps resource has been torn down".to_owned());
        }
        if self.pending.borrow().len() >= MAX_PENDING {
            return Err("Too many pending MCP Apps requests".to_owned());
        }
        let id = self.next_id;
        self.next_id = self.next_id.checked_add(1)
            .ok_or_else(|| "MCP Apps request limit reached".to_owned())?;
        let upload = method=="tools/call" &&
            params.get("name").and_then(Value::as_str)==Some("upload_attachment") &&
            selected_view().as_deref()==Some("learning");
        let download = method=="tools/call" &&
            params.get("name").and_then(Value::as_str)==Some("get_attachment") &&
            params.pointer("/arguments/include_original").and_then(Value::as_bool)==Some(true) &&
            selected_view().as_deref()==Some("learning");
        let max_request = if upload {MAX_FILE_TRANSFER} else {MAX_REQUEST};
        let max_reply = if download {MAX_FILE_TRANSFER} else {MAX_REPLY};
        let request = json!({"jsonrpc":"2.0","id":id,"method":method,"params":params});
        let text = request.to_string();
        if text.len() > max_request { return Err("MCP Apps request is too large".to_owned()); }
        let payload = JSON::parse(&text)
            .map_err(|_| "Unable to encode MCP Apps request")?;
        let (sender, receiver) = oneshot::channel();
        let pending_messages = self.pending.clone();
        let timer = Timeout::new(12_000, move || {
            if let Some(pending) = pending_messages.borrow_mut().remove(&id) {
                let _ = pending.reply.send(Err("MCP Apps host did not respond".to_owned()));
            }
        });
        self.pending.borrow_mut().insert(id, Pending { reply: sender, max_reply, _timeout: timer });
        if self.parent.post_message(&payload, "*").is_err() {
            self.pending.borrow_mut().remove(&id);
            return Err("MCP Apps host connection failed".to_owned());
        }
        Ok(receiver)
    }

    fn notify(&self, method: &str) -> Result<(), String> {
        if self.closed.get() {
            return Err("MCP Apps resource has been torn down".to_owned());
        }
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

// A tool-triggered Learning App receives the originating tool result as a
// host notification. It is not an authorization token or a grading verdict.
pub fn take_host_learning_result() -> Option<Value> {
    BRIDGE.with(|cell| cell.borrow().as_ref()
        .and_then(|bridge| bridge.host_result.borrow_mut().take()))
}

fn notify_initialized() -> Result<(), String> {
    BRIDGE.with(|cell| {
        let state = cell.borrow();
        state.as_ref().ok_or("MCP Apps bridge missing")?
            .notify("ui/notifications/initialized")
    })
}

pub async fn initialize() -> Result<(), String> {
    let app_name = if selected_view().as_deref() == Some("learning") {
        "Homelab Learning"
    } else {
        "Homelab Health"
    };
    let result = request("ui/initialize", json!({
        "appInfo":{"name":app_name,"version":"0.1.0"},
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

// The Learning iframe may access only domain-owned study and learner-response
// operations. Direct grading, answer-key reads, arbitrary backend tools, and
// privileged operations remain outside the browser capability.
const LEARNING_APP_TOOLS: &[&str] = &[
    "get_learning_summary", "get_graph", "get_concept",
    "list_learning_sessions", "get_learning_session", "start_learning_session",
    "get_pending_learning_question", "get_learning_question",
    "answer_learning_question", "next_learning_action",
    "recommend_session_probe", "get_reasoning_graph", "get_reasoning_chain",
    "list_problems", "get_problem", "get_review_queue",
    "list_learning_sources", "search_learning_sources",
    "list_practice_sessions", "get_practice_session",
    "preview_source_curriculum", "import_source_curriculum",
    "list_attachments", "get_attachment", "upload_attachment",
    "propose_question_model_assessment", "propose_step_model_assessment",
];

pub async fn call_learning_tool<T: DeserializeOwned>(
    name: &str, arguments: Value
) -> Result<T, String> {
    if selected_view().as_deref() != Some("learning")
        || !LEARNING_APP_TOOLS.contains(&name) {
        return Err("This operation is not available in the Learning App".to_owned());
    }
    let result = request("tools/call", json!({"name":name,"arguments":arguments})).await?;
    if result.get("isError").and_then(Value::as_bool) == Some(true) {
        let message = result.pointer("/content/0/text").and_then(Value::as_str)
            .unwrap_or("Learning did not accept the request");
        return Err(message.chars().take(240).collect());
    }
    let value = if let Some(content) = result.get("structuredContent") {
        content.clone()
    } else {
        let text = result.pointer("/content/0/text").and_then(Value::as_str)
            .ok_or("Learning tool returned no data")?;
        if text.len() > MAX_REPLY {
            return Err("Learning tool response is too large".to_owned());
        }
        serde_json::from_str(text)
            .map_err(|_| "Learning returned invalid JSON")?
    };
    serde_json::from_value(value)
        .map_err(|_| "Learning returned an incompatible response".to_owned())
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
        style { {include_str!("../assets/main.css")} }
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
                Some("learning") => rsx! { crate::embedded_learning::LearningApp {} },
                _ => rsx! { Notice { message: "Unknown MCP App resource".to_owned() } },
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
    fn host_result_notifications_are_read_only_and_bounded() {
        let event=r#"{"jsonrpc":"2.0","method":"ui/notifications/tool-result","params":{"structuredContent":{"view":"graph","nodes":[{"id":"linear-algebra"}]}}}"#;
        let data=decode_host_result(event).expect("structured host result");
        assert_eq!(data["view"],"graph");
        assert_eq!(data["nodes"][0]["id"],"linear-algebra");
        assert!(decode_host_result(r#"{"jsonrpc":"2.0","method":"ui/notifications/tool-result","params":{"isError":true,"content":[]}}"#).is_none());
        assert!(decode_host_result(r#"{"jsonrpc":"2.0","method":"ui/notifications/tool-input","params":{"arguments":{}}}"#).is_none());
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
