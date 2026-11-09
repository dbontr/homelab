use dioxus::prelude::*;
use reqwest::Method;
use serde_json::{json, Value};

use crate::api;
use crate::components::{Badge, Empty, Heading, Loading, Notice};

#[derive(Clone, PartialEq)]
struct WorkflowDraft {
    project: String,
    objective: String,
    action: String,
    query: String,
    personal_read: bool,
    every_hours: String,
}

impl Default for WorkflowDraft {
    fn default() -> Self {
        Self {
            project: String::new(),
            objective: String::new(),
            action: "project_context".to_owned(),
            query: String::new(),
            personal_read: false,
            every_hours: "24".to_owned(),
        }
    }
}

impl WorkflowDraft {
    fn program(&self) -> Result<Value, String> {
        let project = self.project.trim();
        let objective = self.objective.trim();
        if project.is_empty() || project.len() > 80 ||
            !project.chars().all(|c| c.is_ascii_lowercase() || c.is_ascii_digit() || c == '-') {
            return Err("Enter an existing project ID using lowercase letters, digits or hyphens.".to_owned());
        }
        if objective.is_empty() || objective.len() > 2000 {
            return Err("The objective must contain 1–2,000 characters.".to_owned());
        }
        let needs_query = matches!(self.action.as_str(),
            "research_search" | "research_semantic_query" | "recipe_search");
        let personal = matches!(self.action.as_str(),
            "learning_summary" | "health_goals" | "recipe_search" | "project_tasks");
        let allowed = matches!(self.action.as_str(),
            "project_context" | "project_tasks" | "engineering_runs" |
            "research_search" | "research_semantic_query" | "research_claims" |
            "learning_summary" | "health_goals" | "recipe_search");
        if !allowed {
            return Err("The selected action is not approved for this workflow.".to_owned());
        }
        if personal && !self.personal_read {
            return Err("Personal application reads require explicit consent.".to_owned());
        }
        let query = self.query.trim();
        if needs_query && query.is_empty() ||
            query.len() > 120 || query.contains('\n') || query.contains('\r') {
            return Err("A search requires a query of at most 120 characters without line breaks.".to_owned());
        }
        let mut stage = json!({"id":"read","action":self.action});
        if needs_query {
            stage["query"] = json!(query);
        }
        Ok(json!({
            "project":project,
            "objective":objective,
            "personal_read":self.personal_read,
            "steps":[stage],
        }))
    }

    fn schedule(&self) -> Result<Value, String> {
        let hours = self.every_hours.trim().parse::<u64>()
            .map_err(|_| "The interval must be a whole number of hours.".to_owned())?;
        if !(1..=168).contains(&hours) {
            return Err("The interval must be between 1 hour and 7 days.".to_owned());
        }
        Ok(json!({
            "program":self.program()?,
            "every_minutes":hours * 60,
            "enabled":true,
        }))
    }
}

fn items(value: Option<Result<Value, String>>, key: &str) -> Vec<Value> {
    value
        .and_then(Result::ok)
        .and_then(|data| data.get(key).and_then(Value::as_array).cloned())
        .unwrap_or_default()
}

fn property(value: &Value, key: &str) -> String {
    match value.get(key) {
        Some(Value::String(text)) => text.clone(),
        Some(Value::Number(number)) => number.to_string(),
        _ => String::new(),
    }
}

#[component]
pub fn Workflows(csrf: String) -> Element {
    let csrf = use_signal(|| csrf);
    let mut runs = use_resource(|| async { api::get::<Value>("/v1/workflows").await });
    let mut schedules = use_resource(|| async { api::get::<Value>("/v1/automations").await });
    let mut draft = use_signal(WorkflowDraft::default);
    let mut busy = use_signal(|| false);
    let mut error = use_signal(String::new);
    let mut notice = use_signal(String::new);
    let mut selected_run = use_signal(String::new);
    let detail = use_resource(move || {
        let run_id = selected_run();
        async move {
            if run_id.is_empty() {
                Ok(Value::Null)
            } else {
                api::get::<Value>(&format!("/v1/workflows/{run_id}")).await
            }
        }
    });
    let visible_runs = items(runs.cloned(), "workflows");
    let visible_schedules = items(schedules.cloned(), "automations");

    rsx! {
        Heading {
            title: "Workflows".to_owned(),
            description: "Run bounded project reads and inspect owner-controlled recurring work.".to_owned(),
            button {
                class: "btn btn-outline",
                onclick: move |_| { runs.restart(); schedules.restart(); },
                "Refresh"
            }
        }
        if !error().is_empty() {
            Notice { message: error() }
        }
        if !notice().is_empty() {
            div { class: "alert", role: "status", "{notice}" }
        }
        section { class: "card",
            div { class: "card-header",
                h2 { class: "card-title", "Create a read-only workflow" }
                p { class: "card-description",
                    "Every operation is scoped to an existing project. A recurring schedule cannot change records or invoke privileged commands."
                }
            }
            form { class: "card-body form-stack",
                onsubmit: move |event| {
                    event.prevent_default();
                    if busy() { return; }
                    let request = match draft().schedule() {
                        Ok(value) => value,
                        Err(message) => { error.set(message); return; },
                    };
                    let token = csrf();
                    busy.set(true);
                    error.set(String::new());
                    spawn(async move {
                        match api::write::<Value, _>(
                            Method::POST, "/v1/automations", &token, &request
                        ).await {
                            Ok(_) => {
                                notice.set("Schedule saved. The first run is due after its interval.".to_owned());
                                schedules.restart();
                            }
                            Err(message) => error.set(message),
                        }
                        busy.set(false);
                    });
                },
                label { class: "form-row",
                    span { class: "label", "Project ID" }
                    input {
                        class: "field", required: true, maxlength: "80",
                        value: "{draft().project}",
                        oninput: move |event| {
                            let mut input = draft();
                            input.project = event.value();
                            draft.set(input);
                        }
                    }
                }
                label { class: "form-row",
                    span { class: "label", "Objective" }
                    textarea {
                        class: "field", rows: "2", required: true, maxlength: "2000",
                        value: "{draft().objective}",
                        oninput: move |event| {
                            let mut input = draft();
                            input.objective = event.value();
                            draft.set(input);
                        }
                    }
                }
                label { class: "form-row",
                    span { class: "label", "Read capability" }
                    select {
                        class: "field", value: "{draft().action}",
                        onchange: move |event| {
                            let mut input = draft();
                            input.action = event.value();
                            input.query.clear();
                            draft.set(input);
                        },
                        option { value: "project_context", "Project context" }
                        option { value: "project_tasks", "Project-linked tasks" }
                        option { value: "engineering_runs", "Engineering runs" }
                        option { value: "research_search", "Research search" }
                        option { value: "research_semantic_query", "Semantic research search" }
                        option { value: "research_claims", "Research claims" }
                        option { value: "learning_summary", "Learning summary" }
                        option { value: "health_goals", "Health goals" }
                        option { value: "recipe_search", "Recipe search" }
                    }
                }
                if matches!(draft().action.as_str(),
                    "research_search" | "research_semantic_query" | "recipe_search") {
                    label { class: "form-row",
                        span { class: "label", "Search query" }
                        input {
                            class: "field", required: true, maxlength: "120",
                            value: "{draft().query}",
                            oninput: move |event| {
                                let mut input = draft();
                                input.query = event.value();
                                draft.set(input);
                            }
                        }
                    }
                }
                label { class: "form-row",
                    span { class: "label", "Personal-data access" }
                    select {
                        class: "field",
                        value: if draft().personal_read { "yes" } else { "no" },
                        onchange: move |event| {
                            let mut input = draft();
                            input.personal_read = event.value() == "yes";
                            draft.set(input);
                        },
                        option { value: "no", "No personal records" }
                        option { value: "yes", "Allow the selected personal read" }
                    }
                }
                label { class: "form-row",
                    span { class: "label", "Schedule interval (hours)" }
                    input {
                        class: "field", type: "number", min: "1", max: "168",
                        value: "{draft().every_hours}",
                        oninput: move |event| {
                            let mut input = draft();
                            input.every_hours = event.value();
                            draft.set(input);
                        }
                    }
                }
                div { class: "action-row",
                    button {
                        class: "btn btn-primary", type: "submit", disabled: busy(),
                        if busy() { "Saving…" } else { "Save recurring schedule" }
                    }
                    button {
                        class: "btn btn-outline", type: "button", disabled: busy(),
                        onclick: move |_| {
                            if busy() { return; }
                            let request = match draft().program() {
                                Ok(value) => value,
                                Err(message) => { error.set(message); return; },
                            };
                            let token = csrf();
                            busy.set(true);
                            error.set(String::new());
                            spawn(async move {
                                match api::write::<Value, _>(
                                    Method::POST, "/v1/workflows", &token, &request
                                ).await {
                                    Ok(value) => {
                                        notice.set("Workflow submitted for execution.".to_owned());
                                        selected_run.set(property(&value, "id"));
                                        runs.restart();
                                    }
                                    Err(message) => error.set(message),
                                }
                                busy.set(false);
                            });
                        },
                        "Run once"
                    }
                }
            }
        }
        section { class: "card",
            div { class: "card-header",
                h2 { class: "card-title", "Recurring schedules" }
                p { class: "card-description",
                    "These schedules are independent of model planning and cannot perform writes."
                }
            }
            div { class: "card-body",
                if schedules.cloned().is_none() {
                    Loading {}
                } else if let Some(Err(message)) = schedules.cloned() {
                    Notice { message: message }
                } else if visible_schedules.is_empty() {
                    Empty {
                        title: "No schedules".to_owned(),
                        description: "Create a read-only recurring workflow above.".to_owned()
                    }
                } else {
                    div { class: "item-list",
                        for item in visible_schedules {
                            {
                                let id = property(&item, "id");
                                let project = property(&item, "project");
                                let next = property(&item, "next_at");
                                let last = property(&item, "last_workflow_id");
                                let enabled = item.get("enabled").and_then(Value::as_bool).unwrap_or(false);
                                let revision = item.get("revision").and_then(Value::as_u64).unwrap_or(0);
                                let target = id.clone();
                                rsx! {
                                    article { class: "list-item", key: "{id}",
                                        div {
                                            strong { "{project}" }
                                            p { class: "muted", "Next: {next}" }
                                            if !last.is_empty() {
                                                p { class: "muted", "Last run: {last}" }
                                            }
                                        }
                                        Badge { text: if enabled { "Enabled".to_owned() } else { "Paused".to_owned() } }
                                        button {
                                            class: "btn btn-outline", disabled: busy(),
                                            onclick: move |_| {
                                                let id = target.clone();
                                                let token = csrf();
                                                busy.set(true);
                                                error.set(String::new());
                                                spawn(async move {
                                                    let path = format!("/v1/automations/{id}/enabled");
                                                    let request = json!({
                                                        "revision":revision, "enabled":!enabled
                                                    });
                                                    match api::write::<Value, _>(Method::POST, &path, &token, &request).await {
                                                        Ok(_) => schedules.restart(),
                                                        Err(message) => error.set(message),
                                                    }
                                                    busy.set(false);
                                                });
                                            },
                                            if enabled { "Pause" } else { "Resume" }
                                        }
                                    }
                                }
                            }
                        }
                    }
                }
            }
        }
        section { class: "card",
            div { class: "card-header",
                h2 { class: "card-title", "Workflow runs" }
                p { class: "card-description",
                    "Completed, failed and cancelled runs remain distinguishable. Select a run to inspect stage evidence."
                }
            }
            div { class: "card-body",
                if runs.cloned().is_none() {
                    Loading {}
                } else if let Some(Err(message)) = runs.cloned() {
                    Notice { message: message }
                } else if visible_runs.is_empty() {
                    Empty {
                        title: "No workflow runs".to_owned(),
                        description: "Run a project read or save a schedule to begin.".to_owned()
                    }
                } else {
                    div { class: "item-list",
                        for run in visible_runs.into_iter().take(40) {
                            {
                                let id = property(&run, "id");
                                let project = property(&run, "project");
                                let status = property(&run, "status");
                                let target = id.clone();
                                rsx! {
                                    article { class: "list-item", key: "{id}",
                                        div {
                                            strong { "{project}" }
                                            p { class: "muted", "{id}" }
                                        }
                                        Badge { text: status }
                                        button {
                                            class: "btn btn-outline",
                                            onclick: move |_| selected_run.set(target.clone()),
                                            "Inspect"
                                        }
                                    }
                                }
                            }
                        }
                    }
                }
            }
        }
        if !selected_run().is_empty() {
            section { class: "card",
                div { class: "card-header",
                    h2 { class: "card-title", "Run details" }
                    p { class: "card-description", "{selected_run}" }
                }
                div { class: "card-body",
                    if let Some(Ok(record)) = detail.cloned() {
                        Badge { text: property(&record,"status") }
                        if let Some(steps) = record.get("steps").and_then(Value::as_array) {
                            div { class: "item-list",
                                for step in steps.iter() {
                                    div { class: "list-item",
                                        strong { "{property(step, \"id\")}" }
                                        span { class: "muted", "{property(step, \"action\")}" }
                                        Badge { text: property(step,"status") }
                                        if !property(step,"error").is_empty() {
                                            p { class: "muted", "{property(step, \"error\")}" }
                                        }
                                    }
                                }
                            }
                        }
                    } else if let Some(Err(message)) = detail.cloned() {
                        Notice { message: message }
                    } else {
                        Loading {}
                    }
                }
            }
        }
    }
}

#[cfg(test)]
mod tests {
    use super::WorkflowDraft;

    #[test]
    fn schedule_preserves_explicit_personal_consent() {
        let mut form = WorkflowDraft::default();
        form.project = "research".to_owned();
        form.objective = "Collect evidence".to_owned();
        form.action = "learning_summary".to_owned();
        assert!(form.schedule().is_err());
        form.personal_read = true;
        let value = form.schedule().expect("approved schedule");
        assert_eq!(value["program"]["personal_read"], true);
        assert_eq!(value["every_minutes"], 1440);
        assert_eq!(value["program"]["steps"][0]["action"], "learning_summary");
    }

    #[test]
    fn schedule_rejects_untrusted_actions_and_fast_polling() {
        let mut form = WorkflowDraft::default();
        form.project = "research".to_owned();
        form.objective = "Inspect sources".to_owned();
        form.action = "task_create".to_owned();
        assert!(form.program().is_err());
        form.action = "research_search".to_owned();
        assert!(form.program().is_err());
        form.query = "Fourier".to_owned();
        form.every_hours = "0".to_owned();
        assert!(form.schedule().is_err());
        form.every_hours = "168".to_owned();
        assert!(form.schedule().is_ok());
    }
}
