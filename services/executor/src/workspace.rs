//! Exact, owner-authorized calls to the Workspace controller.
use std::time::Duration;

use reqwest::{header, redirect::Policy as RedirectPolicy, Client, StatusCode};
use serde_json::{json, Value};
use zeroize::Zeroizing;

use crate::authorization::AuthorizationClient;
use crate::policy::{bounded_file, Policy};
use crate::Error;

fn validate_tool_response(raw: &[u8]) -> Result<(), Error> {
    let value: Value = serde_json::from_slice(raw)
        .map_err(|_| Error::Protocol("invalid Workspace MCP response"))?;
    let object = value.as_object().ok_or(Error::Protocol("Workspace reply must be an object"))?;
    if object.get("jsonrpc").and_then(Value::as_str) != Some("2.0")
        || object.get("id").and_then(Value::as_u64) != Some(1)
        || object.contains_key("error") {
        return Err(Error::Denied("Workspace tool invocation was not accepted"));
    }
    let result = object.get("result").and_then(Value::as_object)
        .ok_or(Error::Protocol("missing Workspace MCP tool result"))?;
    let content = result.get("content").and_then(Value::as_array)
        .ok_or(Error::Protocol("invalid Workspace content array"))?;
    if content.iter().any(|block| !block.is_object()) {
        return Err(Error::Protocol("invalid Workspace content block"));
    }
    if let Some(error) = result.get("isError") {
        if error.as_bool() != Some(false) {
            return Err(Error::Denied("Workspace returned an error"));
        }
    }
    Ok(())
}

pub async fn call(policy: &Policy, authorization: &AuthorizationClient,
    tool: &str, arguments: Value) -> Result<(), Error>
{
    let workspace = policy.workspace.as_ref()
        .ok_or(Error::Denied("Workspace administration disabled"))?;
    let token = if let Some(alias) = &policy.workspace_vault_alias {
        authorization.vault_credential(alias).await?
    } else {
        let path = policy.workspace_token.as_ref()
            .ok_or(Error::Configuration("Workspace credential source missing"))?;
        Zeroizing::new(bounded_file(path, 8192, true)?)
    };
    let raw = std::str::from_utf8(&token)
        .map_err(|_| Error::Denied("non-UTF8 Workspace credential"))?;
    let value = raw.strip_suffix('\n').unwrap_or(raw);
    if value.len() < 32 || value.len() > 8192 ||
        value.bytes().any(|b| b == 0 || b.is_ascii_whitespace()) {
        return Err(Error::Denied("invalid Workspace credential"));
    }
    let http = Client::builder().no_proxy().redirect(RedirectPolicy::none())
        .timeout(Duration::from_secs(30))
        .build().map_err(|_| Error::Configuration("Workspace HTTP client unavailable"))?;
    let request = json!({"jsonrpc":"2.0","id":1,"method":"tools/call",
        "params":{"name":tool,"arguments":arguments}});
    let mut response = http.post(&workspace.endpoint)
        .header(header::ACCEPT, "application/json")
        .bearer_auth(value).json(&request).send().await
        .map_err(|_| Error::Unavailable("Workspace controller request failed"))?;
    if response.status() != StatusCode::OK ||
        !response.headers().get(header::CONTENT_TYPE)
            .and_then(|x| x.to_str().ok())
            .is_some_and(|s| s.starts_with("application/json")) {
        return Err(Error::Denied("Workspace controller rejected operation"));
    }
    let mut data = Vec::new();
    while let Some(chunk) = response.chunk().await
        .map_err(|_| Error::Protocol("Workspace reply interrupted"))?
    {
        if chunk.len() > 32768usize.saturating_sub(data.len()) {
            return Err(Error::Protocol("Workspace reply exceeds limit"));
        }
        data.extend_from_slice(&chunk);
    }
    validate_tool_response(&data)
}
#[cfg(test)]
mod tests {
    use super::*;
    #[test]
    fn require_compliant_tool_envelopes() {
        assert!(validate_tool_response(
            br#"{"jsonrpc":"2.0","id":1,"result":{"content":[]}}"#).is_ok());
        assert!(validate_tool_response(
            br#"{"jsonrpc":"2.0","id":1,"result":{"content":[],"isError":false}}"#).is_ok());
        for invalid in [
            br#"{"jsonrpc":"2.0","id":1,"result":{"content":[],"isError":true}}"#.as_slice(),
            br#"{"jsonrpc":"2.0","id":1,"result":{"content":[],"isError":null}}"#,
            br#"{"jsonrpc":"2.0","id":1,"error":{"code":-1}}"#,
            br#"{"jsonrpc":"2.0","id":1,"result":{"content":null}}"#,
            br#"{"jsonrpc":"2.0","id":2,"result":{"content":[]}}"#,
        ] {
            assert!(validate_tool_response(invalid).is_err());
        }
    }
}
