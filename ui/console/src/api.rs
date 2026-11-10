use serde::de::DeserializeOwned;
use serde::{Deserialize, Serialize};
use reqwest::Method;
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
    #[serde(default)]
    pub system: String,
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
    pub parent_id: String,
    #[serde(default)]
    pub decomposed: bool,
    #[serde(default)]
    pub status: String,
    #[serde(default)]
    pub minutes: i32,
    #[serde(default)]
    pub due: String,
    #[serde(default)]
    pub notes: String,
    #[serde(default)]
    pub scheduled: String,
    #[serde(default)]
    pub recurrence: String,
    #[serde(default)]
    pub energy: String,
    #[serde(default)]
    pub location: String,
    #[serde(default)]
    pub priority: i32,
    #[serde(default)]
    pub revision: i32,
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
        409 => "This record was modified or is still in use".to_owned(),
        428 => "Current record revision required".to_owned(),
        503 => "Service is not configured".to_owned(),
        code => format!("Service returned HTTP {code}"),
    }
}

#[cfg(not(feature = "embedded-app"))]
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

#[cfg(not(feature = "embedded-app"))]
pub async fn write<T: DeserializeOwned, P: Serialize>(
    method: Method, path: &str, csrf: &str, payload: &P
) -> Result<T, String> {
    let response = reqwest::Client::new()
        .request(method, url(path)?)
        .header("X-CSRF-Token", csrf)
        .json(payload)
        .send()
        .await
        .map_err(|_| "Could not contact the homelab API".to_owned())?;
    if !response.status().is_success() {
        let status = response.status();
        if matches!(status.as_u16(), 400 | 409 | 413 | 422) {
            if let Ok(body) = response.json::<serde_json::Value>().await {
                if let Some(message) = body.get("error").and_then(serde_json::Value::as_str) {
                    if !message.is_empty() && message.len() <= 250 {
                        return Err(message.to_owned());
                    }
                }
            }
        }
        return Err(response_error(status));
    }
    response.json::<T>().await
        .map_err(|_| "The service returned invalid data".to_owned())
}

#[derive(Clone, Debug, Deserialize)]
pub struct ImageUpload {
    pub image_ref: String,
}

pub async fn upload_recipe_image(
    csrf: &str, media_type: &str, contents: Vec<u8>
) -> Result<ImageUpload, String> {
    if contents.is_empty() || contents.len() > 5 * 1024 * 1024 {
        return Err("Recipe image must be 5 MiB or smaller".to_owned());
    }
    if media_type != "image/jpeg" && media_type != "image/png" {
        return Err("Choose a JPEG or PNG image".to_owned());
    }
    let response = reqwest::Client::new()
        .post(url("/v1/recipes/images")?)
        .header("X-CSRF-Token", csrf)
        .header("Content-Type", media_type)
        .body(contents)
        .send()
        .await
        .map_err(|_| "Could not upload the recipe image".to_owned())?;
    if !response.status().is_success() {
        return Err(response_error(response.status()));
    }
    response.json::<ImageUpload>().await
        .map_err(|_| "The service returned invalid image data".to_owned())
}

#[derive(Clone, Debug, Deserialize)]
pub struct BundleImport {
    pub imported: usize,
}

pub async fn import_recipe_bundle(csrf: &str, content: Vec<u8>) -> Result<BundleImport, String> {
    if content.is_empty() || content.len() > 96 * 1024 * 1024 {
        return Err("Recipe ZIP must be 96 MiB or smaller".to_owned());
    }
    let response = reqwest::Client::new()
        .post(url("/v1/recipes/bundle")?)
        .header("X-CSRF-Token", csrf)
        .header("Content-Type", "application/zip")
        .body(content)
        .send()
        .await
        .map_err(|_| "Could not upload the recipe ZIP".to_owned())?;
    if !response.status().is_success() {
        return Err(response_error(response.status()));
    }
    response.json::<BundleImport>().await
        .map_err(|_| "The service returned invalid recipe bundle data".to_owned())
}

pub async fn remove(path: &str, csrf: &str, revision: Option<i32>) -> Result<(), String> {
    let client = reqwest::Client::new();
    let mut request = client.delete(url(path)?).header("X-CSRF-Token", csrf);
    if let Some(version) = revision {
        request = request.header("If-Match", format!("\"{version}\""));
    }
    let response = request.send().await
        .map_err(|_| "Could not contact the homelab API".to_owned())?;
    if response.status().is_success() { Ok(()) }
    else { Err(response_error(response.status())) }
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
            if let Some(number) = item.as_f64() {
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


#[cfg(feature = "embedded-app")]
pub async fn get<T: DeserializeOwned>(path: &str) -> Result<T, String> {
    let action = match path {
        "/v1/health/goals" => "health_goal_list",
        "/v1/health/records" => "health_record_list",
        _ => return Err("The Health App cannot request this owner-only route".to_owned()),
    };
    crate::mcp_app::call_tool(action, serde_json::json!({})).await
}

#[cfg(feature = "embedded-app")]
pub async fn write<T: DeserializeOwned, P: Serialize>(
    method: Method,
    path: &str,
    _csrf: &str,
    payload: &P,
) -> Result<T, String> {
    if method != Method::POST || path != "/v1/health/reaction-test" {
        return Err("The Health App cannot write to this owner-only route".to_owned());
    }
    let value = serde_json::to_value(payload)
        .map_err(|_| "Reaction trials have invalid JSON".to_owned())?;
    let raw = value.get("trials_ms").and_then(Value::as_array)
        .ok_or("Reaction trial measurements are required")?;
    if raw.len() != 5 {
        return Err("The reaction task requires exactly five trials".to_owned());
    }
    let mut samples = Vec::with_capacity(5);
    for item in raw {
        let number = item.as_f64().ok_or("Invalid reaction trial")?;
        if !number.is_finite() || !(80.0..=1500.0).contains(&number) {
            return Err("Reaction trials must be between 80 and 1500 ms".to_owned());
        }
        samples.push(number);
    }
    let mut ordered = samples.clone();
    ordered.sort_by(f64::total_cmp);
    crate::mcp_app::call_tool("health_record_add", serde_json::json!({
        "domain": "cognition", "metric": "simple_reaction_ms",
        "value": ordered[2], "unit": "ms",
        "source": "browser_visual_reaction_test", "method": "estimated",
        "metadata": {
            "trials_ms": samples,
            "mean_ms": ordered.iter().sum::<f64>() / 5.0,
            "protocol": "five trial visual response",
            "constraints": "Browser and display latency; not clinically standardized"
        }
    })).await
}
