#[cfg(not(feature = "embedded-app"))]
use reqwest::Method;
use serde::de::DeserializeOwned;
use serde_json::Value;
#[cfg(not(feature = "embedded-app"))]
use serde_json::json;

#[cfg(not(feature = "embedded-app"))]
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
    GroceryList,
    GrocerySave,
    GroceryFromPlan,
    TrainingSetList,
    TrainingSetLog,
    WorkoutLog,
}

impl Tool {
    fn route(self) -> &'static str {
        match self {
            Self::MetricSeries => "/v1/health/app-tools/health_metric_series/call",
            Self::NutrientDaily => "/v1/health/app-tools/health_nutrient_daily/call",
            Self::GoalSet => "/v1/health/app-tools/health_goal_set/call",
            Self::PlanList => "/v1/health/app-tools/health_plan_list/call",
            Self::PlanSave => "/v1/health/app-tools/health_plan_save/call",
            Self::GroceryList => "/v1/health/app-tools/health_grocery_list/call",
            Self::GrocerySave => "/v1/health/app-tools/health_grocery_save/call",
            Self::GroceryFromPlan => "/v1/health/app-tools/health_grocery_from_plan/call",
            Self::TrainingSetList => "/v1/health/app-tools/health_training_set_list/call",
            Self::TrainingSetLog => "/v1/health/app-tools/health_training_set_log/call",
            Self::WorkoutLog => "/v1/health/app-tools/health_workout_log/call",
        }
    }
}

#[cfg(not(feature = "embedded-app"))]
pub async fn call<T: DeserializeOwned>(csrf: &str, tool: Tool, args: Value) -> Result<T, String> {
    let response: Value = api::write(
        Method::POST,
        tool.route(),
        csrf,
        &json!({"arguments": args}),
    )
    .await?;
    if response.get("isError").and_then(Value::as_bool) == Some(true) {
        let message = response
            .pointer("/content/0/text")
            .and_then(Value::as_str)
            .unwrap_or("The Health service rejected the request");
        return Err(message.chars().take(256).collect());
    }
    if let Some(body) = response.get("structuredContent") {
        return serde_json::from_value(body.clone())
            .map_err(|_| "Health returned an unsupported structured result".to_owned());
    }
    let text = response
        .pointer("/content/0/text")
        .and_then(Value::as_str)
        .ok_or_else(|| "Health returned no result".to_owned())?;
    serde_json::from_str(text).map_err(|_| "Health returned invalid JSON".to_owned())
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn health_tools_have_fixed_owner_session_routes() {
        for tool in [
            Tool::MetricSeries,
            Tool::NutrientDaily,
            Tool::GoalSet,
            Tool::PlanList,
            Tool::PlanSave,
            Tool::GroceryList,
            Tool::GrocerySave,
            Tool::GroceryFromPlan,
            Tool::TrainingSetList,
            Tool::TrainingSetLog,
            Tool::WorkoutLog,
        ] {
            assert!(tool.route().starts_with("/v1/health/app-tools/"));
            assert!(tool.route().ends_with("/call"));
        }
    }
}

#[cfg(feature = "embedded-app")]
pub async fn call<T: DeserializeOwned>(_csrf: &str, tool: Tool, args: Value) -> Result<T, String> {
    let name = tool
        .route()
        .strip_prefix("/v1/health/app-tools/")
        .and_then(|route| route.strip_suffix("/call"))
        .ok_or("Invalid Health MCP App tool identity")?;
    crate::mcp_app::call_tool(name, args).await
}
