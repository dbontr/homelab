use dioxus::prelude::*;
use serde_json::Value;

use crate::api;
use crate::components::{Badge, Card, Empty, Heading, Icon, Loading, Notice};

fn as_items(value: &Value, key: &str) -> Vec<Value> {
    value.get(key).and_then(Value::as_array).cloned().unwrap_or_default()
}

fn text(value: &Value, key: &str) -> String {
    api::property(value, &[key])
}

fn draft_url(steps: &[Value]) -> Option<String> {
    steps.iter()
        .find(|step| step.get("kind").and_then(Value::as_str) == Some("publish"))
        .and_then(|step| step.get("result"))
        .filter(|result| result.get("draft").and_then(Value::as_bool) == Some(true))
        .and_then(|result| result.get("url").and_then(Value::as_str))
        .filter(|url| url.starts_with("https://github.com/"))
        .map(str::to_owned)
}

#[component]
fn RunDetail(run: Value) -> Element {
    let steps = as_items(&run, "steps");
    let source = run.get("agent").cloned().unwrap_or(Value::Null);
    let plan = as_items(&source, "plan");
    let changed = as_items(&source, "changed_paths");
    let review = steps.iter()
        .find(|step| step.get("kind").and_then(Value::as_str) == Some("review"))
        .and_then(|step| step.get("result"))
        .cloned()
        .unwrap_or(Value::Null);
    let findings = as_items(&review, "findings");
    let publication = draft_url(&steps);

    rsx! {
        details { class: "card",
            summary { class: "card-header",
                div {
                    h2 { class: "card-title", "{text(&run, "id")}" }
                    p { class: "card-description", "Project: {text(&run, "project")}" }
                }
                Badge { text: text(&run, "status") }
            }
            div { class: "card-body",
                if source.is_object() {
                    div { class: "inline-row",
                        span { class: "muted", "Decisions: {text(&source, "turns")}" }
                        span { class: "muted", "Latest action: {text(&source, "last_action")}" }
                    }
                    if !plan.is_empty() {
                        h3 { "Engineering plan" }
                        ol {
                            for item in plan {
                                li { "{item.as_str().unwrap_or("Unspecified step")}" }
                            }
                        }
                    }
                    if !changed.is_empty() {
                        h3 { "Changed files" }
                        ul {
                            for path in changed {
                                li { class: "mono", "{path.as_str().unwrap_or("")}" }
                            }
                        }
                    }
                    if let Some(summary) = source.get("summary").and_then(Value::as_str) {
                        p { "{summary}" }
                    }
                }
                h3 { "Execution and verification" }
                div { class: "table-clip",
                    table { class: "table",
                        thead { tr { th { "Step" } th { "State" } th { "Attempts" } } }
                        tbody {
                            for step in &steps {
                                tr {
                                    td { "{text(step, "kind")}" }
                                    td { Badge { text: text(step, "status") } }
                                    td { "{text(step, "attempts")}" }
                                }
                            }
                        }
                    }
                }
                if review.is_object() {
                    h3 { "Source review" }
                    div { class: "inline-row",
                        Badge { text: text(&review, "verdict") }
                        span { "Risk: {text(&review, "risk")}" }
                    }
                    if !findings.is_empty() {
                        ul {
                            for finding in findings {
                                li { "{finding.as_str().unwrap_or("")}" }
                            }
                        }
                    }
                }
                if let Some(url) = publication {
                    a { class: "btn btn-outline", href: "{url}", target: "_blank", rel: "noopener noreferrer",
                        "Inspect GitHub diff and draft publication"
                    }
                } else {
                    p { class: "muted", "No draft publication has been confirmed." }
                }
            }
        }
    }
}

#[component]
fn TeamDetail(team: Value) -> Element {
    let members = as_items(&team, "members");
    let artifacts = as_items(&team, "artifacts");
    let conflicts = as_items(&team, "conflicts");
    let ready = team.get("ready_for_review").and_then(Value::as_bool).unwrap_or(false);
    rsx! {
        details { class: "card",
            summary { class: "card-header",
                div {
                    h2 { class: "card-title", "{text(&team, "id")}" }
                    p { class: "card-description", "{text(&team, "project")} · {text(&team, "repository")}" }
                }
                Badge { text: text(&team, "status") }
            }
            div { class: "card-body",
                p { class: "muted", "Each member edits a separate approved file scope. Changes remain unpublished until reviewed together." }
                for member in members {
                    div { class: "inline-row",
                        strong { "{text(&member, "role")}" }
                        span { class: "mono", "{text(&member, "key")}" }
                        Badge { text: member.get("run").map(|run| text(run, "status")).unwrap_or_default() }
                    }
                }
                if !artifacts.is_empty() {
                    h3 { "Verified artifacts" }
                    for artifact in artifacts {
                        div { class: "inline-row",
                            span { class: "mono", "{text(&artifact, "path")}" }
                            span { class: "muted", "Prepared by {text(&artifact, "member")}" }
                        }
                    }
                }
                if !conflicts.is_empty() {
                    h3 { "Reconciliation conflicts" }
                    for conflict in conflicts {
                        p { role: "alert", "{conflict.as_str().unwrap_or("")}" }
                    }
                }
                if ready {
                    p { class: "muted", "Verified artifact manifest is ready for owner review. It has not been published." }
                }
            }
        }
    }
}

#[component]
pub fn EngineeringRuns(csrf: String) -> Element {
    let mut runs = use_resource(|| async { api::get::<Value>("/v1/engineering/runs").await });
    let mut teams = use_resource(|| async { api::get::<Value>("/v1/engineering/teams").await });
    rsx! {
        Heading {
            title: "Engineering".to_owned(),
            description: "Autonomous plans, isolated execution, verified source, and controlled publication.".to_owned(),
            button {
                class: "btn btn-outline",
                onclick: move |_| { runs.restart(); teams.restart(); },
                Icon { name: "refresh".to_owned() }
                "Refresh"
            }
        }
        crate::engineering_create::EngineeringCreate { csrf: csrf.clone() }
        Card {
            title: "Publication boundary".to_owned(),
            description: "Only draft changes may be prepared automatically. Merging and deployment require independent owner authorization.".to_owned(),
            p { class: "muted", "Review the verified source and checks before approving a draft." }
        }
        match runs.cloned() {
            None => rsx! { Loading {} },
            Some(Err(err)) => rsx! { Notice { message: err } },
            Some(Ok(result)) => {
                let items = api::list(&result, &["runs"]);
                rsx! {
                    if items.is_empty() {
                        Empty { title: "No engineering runs".to_owned(),
                            description: "Create a run through the authenticated engineering tools.".to_owned() }
                    } else {
                        for run in items { RunDetail { run } }
                    }
                }
            }
        }
        Heading {
            title: "Cooperative engineering".to_owned(),
            description: "Bounded parallel agents with separate workspaces and disjoint source ownership.".to_owned(),
            span {}
        }
        match teams.cloned() {
            None => rsx! { Loading {} },
            Some(Err(err)) => rsx! { Notice { message: err } },
            Some(Ok(result)) => {
                let items = api::list(&result, &["teams"]);
                rsx! {
                    if items.is_empty() {
                        Empty { title: "No cooperative teams".to_owned(),
                            description: "Submit a scoped engineering team to prepare separate verified artifacts.".to_owned() }
                    } else {
                        for team in items { TeamDetail { team } }
                    }
                }
            }
        }
    }
}
