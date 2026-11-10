use reqwest::Method;
use serde::de::DeserializeOwned;
use serde_json::{Value, json};

use crate::api;

// Browser pages use the owner-session, CSRF-protected HTTP surface. Service
// credentials remain inside the Go transport and are never sent to WASM.
#[derive(Clone, Copy)]
pub enum Tool {
    MetricSeries,
    NutrientDaily,
    GoalSet,
    PlanList,
    PlanSave,
}

impl Tool {
    fn route(self) -> &'static str {
        match self {
            Self::MetricSeries => "/v1/tools/health__health_metric_series/call",
            Self::NutrientDaily => "/v1/tools/health__health_nutrient_daily/call",
            Self::GoalSet => "/v1/tools/health__health_goal_set/call",
            Self::PlanList => "/v1/tools/health__health_plan_list/call",
            Self::PlanSave => "/v1/tools/health__health_plan_save/call",
        }
    }
}

pub async fn call<T: DeserializeOwned>(
    csrf: &str,
    tool: Tool,
    args: Value,
) -> Result<T, String> {
    let response: Value = api::write(
        Method::POST,
        tool.route(),
        csrf,
        &json!({"arguments": args}),
    ).await?;
    if response.get("isError").and_then(Value::as_bool) == Some(true) {
        let message = response.pointer("/content/0/text").and_then(Value::as_str)
            .unwrap_or("The Health service rejected the request");
        return Err(message.chars().take(256).collect());
    }
    if let Some(body) = response.get("structuredContent") {
        return serde_json::from_value(body.clone())
            .map_err(|_| "Health returned an unsupported structured result".to_owned());
    }
    let text = response.pointer("/content/0/text").and_then(Value::as_str)
        .ok_or_else(|| "Health returned no result".to_owned())?;
    serde_json::from_str(text)
        .map_err(|_| "Health returned invalid JSON".to_owned())
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn health_tools_have_fixed_owner_session_routes() {
        for tool in [Tool::MetricSeries, Tool::NutrientDaily, Tool::GoalSet, Tool::PlanList, Tool::PlanSave] {
            assert!(tool.route().starts_with("/v1/tools/health__"));
            assert!(tool.route().ends_with("/call"));
        }
    }
}
