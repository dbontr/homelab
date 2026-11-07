use serde::de::DeserializeOwned;
use serde::Deserialize;
use serde_json::Value;

#[derive(Clone, Debug, Deserialize, PartialEq)]
pub struct Session {
    pub authenticated: bool,
    #[serde(default)]
    pub csrf_token: Option<String>,
}

#[derive(Clone, Debug, Deserialize, PartialEq)]
pub struct Node {
    pub id: String,
    pub mode: String,
    #[serde(default)]
    pub role: String,
    #[serde(default, rename = "nodeIP")]
    pub node_ip: String,
    #[serde(default, rename = "lanInterface")]
    pub lan_interface: String,
}

#[derive(Clone, Debug, Deserialize, PartialEq)]
pub struct Device {
    pub id: String,
    pub kind: String,
    pub mode: String,
    #[serde(default)]
    pub connection: String,
    #[serde(default)]
    pub node: String,
    #[serde(default, rename = "eligibleForCompute")]
    pub eligible_for_compute: bool,
}

#[derive(Clone, Debug, Deserialize, PartialEq)]
pub struct Profile {
    pub id: String,
    pub title: String,
    pub version: i32,
    #[serde(default)]
    pub environment: String,
}

#[derive(Clone, Debug, Deserialize, PartialEq)]
pub struct Task {
    pub id: String,
    pub title: String,
    #[serde(default)]
    pub status: String,
    #[serde(default)]
    pub minutes: i32,
    #[serde(default)]
    pub due: String,
}

#[derive(Clone, Debug, Deserialize, PartialEq)]
pub struct Tool {
    pub name: String,
    #[serde(default)]
    pub description: String,
}

#[derive(Clone, Debug, Deserialize, PartialEq)]
pub struct ToolList {
    pub tools: Vec<Tool>,
}

fn url(path: &str) -> Result<String, String> {
    if !path.starts_with('/') || path.starts_with("//") {
        return Err("Invalid same-origin API path".to_owned());
    }
    let origin = web_sys::window()
        .and_then(|window| window.location().origin().ok())
        .ok_or_else(|| "Browser origin is unavailable".to_owned())?;
    Ok(format!("{origin}{path}"))
}

fn response_error(status: reqwest::StatusCode) -> String {
    match status.as_u16() {
        401 => "Authentication required".to_owned(),
        403 => "Permission denied".to_owned(),
        404 => "Resource not found".to_owned(),
        503 => "Service is not configured".to_owned(),
        code => format!("Service returned HTTP {code}"),
    }
}

pub async fn get<T: DeserializeOwned>(path: &str) -> Result<T, String> {
    let response = reqwest::Client::new()
        .get(url(path)?)
        .send()
        .await
        .map_err(|_| "Could not connect to the homelab API".to_owned())?;
    if !response.status().is_success() {
        return Err(response_error(response.status()));
    }
    response
        .json::<T>()
        .await
        .map_err(|_| "The service returned invalid data".to_owned())
}

pub async fn login(passphrase: &str) -> Result<(), String> {
    let response = reqwest::Client::new()
        .post(url("/ui/session")?)
        .json(&serde_json::json!({"passphrase": passphrase}))
        .send()
        .await
        .map_err(|_| "Could not connect to the session service".to_owned())?;
    if response.status().is_success() {
        Ok(())
    } else {
        Err(response_error(response.status()))
    }
}

pub async fn logout(csrf: &str) -> Result<(), String> {
    let response = reqwest::Client::new()
        .delete(url("/ui/session")?)
        .header("X-CSRF-Token", csrf)
        .send()
        .await
        .map_err(|_| "Could not contact the session service".to_owned())?;
    if response.status().is_success() {
        Ok(())
    } else {
        Err(response_error(response.status()))
    }
}

pub fn list(value: &Value, keys: &[&str]) -> Vec<Value> {
    if let Some(items) = value.as_array() {
        return items.clone();
    }
    for key in keys {
        if let Some(items) = value.get(key).and_then(Value::as_array) {
            return items.clone();
        }
    }
    Vec::new()
}

pub fn property(value: &Value, keys: &[&str]) -> String {
    for key in keys {
        if let Some(item) = value.get(key) {
            if let Some(text) = item.as_str() {
                if !text.is_empty() {
                    return text.to_owned();
                }
            }
            if let Some(number) = item.as_i64() {
                return number.to_string();
            }
        }
    }
    "—".to_owned()
}

#[cfg(test)]
mod tests {
    use super::{list, property};

    #[test]
    fn extract_provider_items_without_inventing_values() {
        let input = serde_json::json!({"projects":[{"id":"research","name":"Research"}]});
        let rows = list(&input, &["projects"]);
        assert_eq!(rows.len(), 1);
        assert_eq!(property(&rows[0], &["title", "name"]), "Research");
        assert_eq!(property(&rows[0], &["unknown"]), "—");
        assert!(list(&serde_json::json!({}), &["projects"]).is_empty());
    }
}
