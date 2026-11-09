use dioxus::prelude::*;
use reqwest::Method;
use serde_json::{json, Value};

use crate::api;
use crate::components::{Card, Notice};

#[derive(Clone, PartialEq)]
struct Draft {
    project: String,
    repository: String,
    revision: String,
    title: String,
    objective: String,
    allowed_paths: String,
    linked_repositories: String,
    checks: String,
    image_pool_id: String,
    knowledge: bool,
    review: bool,
    prepare_only: bool,
}
impl Default for Draft {
    fn default() -> Self {
        Self {
            project: String::new(),
            repository: String::new(),
            revision: String::new(),
            title: String::new(),
            objective: String::new(),
            allowed_paths: String::new(),
            linked_repositories: String::new(),
            checks: String::new(),
            image_pool_id: "debian-bookworm-slim".to_owned(),
            knowledge: false,
            review: true,
            prepare_only: false,
        }
    }
}
fn lines(value: &str) -> Vec<String> {
    value.lines().map(str::trim).filter(|line| !line.is_empty()).map(str::to_owned).collect()
}
fn program(d: &Draft) -> Result<Value, String> {
    let mut linked = Vec::new();
    for entry in lines(&d.linked_repositories) {
        let Some((repository, revision)) = entry.split_once('@') else {
            return Err("Linked repositories require owner/name@40-character-commit".to_owned());
        };
        if revision.len() != 40 {
            return Err("Linked repositories require an exact 40-character commit".to_owned());
        }
        linked.push(json!({"repository": repository, "revision": revision}));
    }
    let allowed = lines(&d.allowed_paths);
    let checks = lines(&d.checks);
    if d.project.is_empty() || d.repository.is_empty() || d.title.trim().is_empty() ||
        d.objective.trim().is_empty() || d.revision.len() != 40 ||
        allowed.is_empty() || checks.is_empty() {
        return Err("Complete the project, repository, revision, objective, allowed files, and checks.".to_owned());
    }
    Ok(json!({
        "project": d.project, "repository": d.repository, "revision": d.revision,
        "expected_head": d.revision, "base_branch": "main",
        "title": d.title, "objective": d.objective,
        "allowed_paths": allowed, "checks": checks, "linked_repositories": linked,
        "autonomous": true, "include_knowledge": d.knowledge,
        "review": d.review, "prepare_only": d.prepare_only,
        "image_pool_id": d.image_pool_id, "cpu": 1, "memory_mb": 2048,
        "duration_minutes": 45
    }))
}
#[component]
pub fn EngineeringCreate(csrf: String) -> Element {
    let mut draft = use_signal(Draft::default);
    let mut busy = use_signal(|| false);
    let mut error = use_signal(String::new);
    let mut result = use_signal(String::new);
    rsx! {
        Card {
            title: "Start an engineering run".to_owned(),
            description: "Source changes are limited to the listed files and exact repository revision. Publication creates a draft only.".to_owned(),
            form {
                class: "form-stack",
                onsubmit: move |event| {
                    event.prevent_default();
                    if busy() { return; }
                    let request = match program(&draft()) {
                        Ok(request) => request,
                        Err(message) => { error.set(message); return; }
                    };
                    let token = csrf.clone();
                    busy.set(true);
                    error.set(String::new());
                    result.set(String::new());
                    spawn(async move {
                        match api::write::<Value, _>(Method::POST, "/v1/engineering/runs", &token, &request).await {
                            Ok(record) => result.set(api::property(&record, &["id"])),
                            Err(message) => error.set(message),
                        }
                        busy.set(false);
                    });
                },
                label { class: "form-row",
                    span { class: "label", "Project ID" }
                    input { class: "field", required: true, maxlength: "80",
                        value: "{draft().project}", oninput: move |e| {
                            let mut d = draft(); d.project = e.value(); draft.set(d);
                        }
                    }
                }
                label { class: "form-row",
                    span { class: "label", "Source repository" }
                    input { class: "field", required: true, placeholder: "owner/repository",
                        value: "{draft().repository}", oninput: move |e| {
                            let mut d = draft(); d.repository = e.value(); draft.set(d);
                        }
                    }
                }
                label { class: "form-row",
                    span { class: "label", "Exact Git revision" }
                    input { class: "field", required: true, maxlength: "40",
                        value: "{draft().revision}", oninput: move |e| {
                            let mut d = draft(); d.revision = e.value(); draft.set(d);
                        }
                    }
                }
                label { class: "form-row",
                    span { class: "label", "Change title" }
                    input { class: "field", required: true,
                        value: "{draft().title}", oninput: move |e| {
                            let mut d = draft(); d.title = e.value(); draft.set(d);
                        }
                    }
                }
                label { class: "form-row",
                    span { class: "label", "Engineering objective" }
                    textarea { class: "field", required: true, rows: "3",
                        value: "{draft().objective}", oninput: move |e| {
                            let mut d = draft(); d.objective = e.value(); draft.set(d);
                        }
                    }
                }
                label { class: "form-row",
                    span { class: "label", "Editable paths · one per line" }
                    textarea { class: "field", required: true, rows: "3",
                        value: "{draft().allowed_paths}", oninput: move |e| {
                            let mut d = draft(); d.allowed_paths = e.value(); draft.set(d);
                        }
                    }
                }
                label { class: "form-row",
                    span { class: "label", "Verification commands · one per line" }
                    textarea { class: "field", required: true, rows: "2",
                        value: "{draft().checks}", oninput: move |e| {
                            let mut d = draft(); d.checks = e.value(); draft.set(d);
                        }
                    }
                }
                label { class: "form-row",
                    span { class: "label", "Linked read-only repositories · owner/name@commit, one per line" }
                    textarea { class: "field", rows: "2",
                        value: "{draft().linked_repositories}", oninput: move |e| {
                            let mut d = draft(); d.linked_repositories = e.value(); draft.set(d);
                        }
                    }
                }
                label { class: "form-row",
                    span { class: "label", "Approved compute image" }
                    input { class: "field", required: true,
                        value: "{draft().image_pool_id}", oninput: move |e| {
                            let mut d = draft(); d.image_pool_id = e.value(); draft.set(d);
                        }
                    }
                }
                label { class: "form-row",
                    span { class: "label", "Project knowledge" }
                    select { class: "field", value: if draft().knowledge { "enabled" } else { "disabled" },
                        onchange: move |e| {
                            let mut d = draft(); d.knowledge = e.value() == "enabled"; draft.set(d);
                        },
                        option { value: "disabled", "Do not send project knowledge to the model" }
                        option { value: "enabled", "Use authorized project knowledge" }
                    }
                }
                label { class: "form-row",
                    span { class: "label", "Review gate" }
                    select { class: "field", value: if draft().review { "enabled" } else { "disabled" },
                        onchange: move |e| {
                            let mut d = draft(); d.review = e.value() == "enabled"; draft.set(d);
                        },
                        option { value: "enabled", "Require source review" }
                        option { value: "disabled", "Use verification checks only" }
                    }
                }
                label { class: "form-row",
                    span { class: "label", "Publication" }
                    select { class: "field", value: if draft().prepare_only { "prepare" } else { "draft" },
                        onchange: move |e| {
                            let mut d = draft(); d.prepare_only = e.value() == "prepare"; draft.set(d);
                        },
                        option { value: "draft", "Prepare verified draft PR" }
                        option { value: "prepare", "Export artifacts without publication" }
                    }
                }
                button { class: "btn btn-primary", disabled: busy(),
                    if busy() { "Submitting…" } else { "Create engineering run" }
                }
            }
            if !error().is_empty() { Notice { message: error() } }
            if !result().is_empty() {
                p { class: "muted", role: "status", "Run created: {result()}. Select Refresh to inspect progress." }
            }
        }
    }
}
