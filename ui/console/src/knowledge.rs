use dioxus::prelude::*;
use serde_json::Value;

use crate::api;
use crate::components::{Badge, Card, Empty, Heading, Icon, Loading, Notice};

fn encode_query(value: &str) -> String {
    let mut result = String::new();
    for byte in value.as_bytes() {
        match *byte {
            b'A'..=b'Z' | b'a'..=b'z' | b'0'..=b'9' | b'-' | b'_' | b'.' | b'~' => result.push(*byte as char),
            _ => result.push_str(&format!("%{byte:02X}")),
        }
    }
    result
}
fn project_path(project: &str, section: &str) -> String {
    format!("/v1/engineering/projects/{project}/{section}")
}
fn document_body(context: &Value, key: &str) -> Option<String> {
    context.get(key)
        .and_then(|record| record.get("body"))
        .and_then(Value::as_str)
        .map(|text| text.chars().take(1800).collect())
}
#[component]
pub fn ProjectKnowledge() -> Element {
    let projects = use_resource(|| async { api::get::<Value>("/v1/engineering/projects").await });
    let mut selected = use_signal(String::new);
    let mut query = use_signal(String::new);
    let mut submitted = use_signal(String::new);
    let mut context = use_resource(move || {
        let id = selected();
        async move {
            if id.is_empty() { Ok(Value::Null) }
            else { api::get::<Value>(&project_path(&id, "context")).await }
        }
    });
    let mut overview = use_resource(move || {
        let id = selected();
        async move {
            if id.is_empty() { Ok(Value::Null) }
            else { api::get::<Value>(&format!("/v1/projects/{id}/overview")).await }
        }
    });
    let mut workflows = use_resource(|| async { api::get::<Value>("/v1/workflows").await });
    let mut repositories = use_resource(move || {
        let id = selected();
        async move {
            if id.is_empty() { Ok(Value::Null) }
            else { api::get::<Value>(&project_path(&id, "repositories")).await }
        }
    });
    let mut research = use_resource(move || {
        let id = selected();
        let q = submitted();
        async move {
            if id.is_empty() || q.is_empty() { Ok(Value::Null) }
            else {
                let endpoint = format!("{}?q={}", project_path(&id, "research"), encode_query(&q));
                api::get::<Value>(&endpoint).await
            }
        }
    });
    rsx! {
        Heading {
            title: "Project knowledge".to_owned(),
            description: "Project context, connected repositories, shared libraries, and source-backed research.".to_owned(),
            button {
                class: "btn btn-outline",
                onclick: move |_| { context.restart(); repositories.restart(); overview.restart(); workflows.restart(); research.restart(); },
                Icon { name: "refresh".to_owned() }
                "Refresh"
            }
        }
        Card {
            title: "Project".to_owned(),
            description: "Knowledge access follows the selected project's configured authority and links.".to_owned(),
            match projects.cloned() {
                None => rsx! { Loading {} },
                Some(Err(err)) => rsx! { Notice { message: err } },
                Some(Ok(value)) => {
                    let items = api::list(&value, &["projects", "result"]);
                    rsx! {
                        select {
                            class: "input",
                            aria_label: "Project",
                            value: selected(),
                            onchange: move |event| { selected.set(event.value()); submitted.set(String::new()); },
                            option { value: "", "Choose a project" }
                            for item in items {
                                option {
                                    value: "{api::property(&item, &["id"])}",
                                    "{api::property(&item, &["title", "name", "id"])}"
                                }
                            }
                        }
                    }
                }
            }
        }
        if !selected().is_empty() {
            Card {
                title: "Canonical context".to_owned(),
                description: "Project notes and decisions are provided by the owning knowledge service.".to_owned(),
                match context.cloned() {
                    None => rsx! { Loading {} },
                    Some(Err(err)) => rsx! { Notice { message: err } },
                    Some(Ok(value)) => rsx! {
                        div { class: "inline-row",
                            span { "Authority" }
                            Badge { text: api::property(&value, &["authority"]) }
                        }
                        if let Some(body) = document_body(&value, "project_document") {
                            h3 { "Project" }
                            p { class: "muted", "{body}" }
                        }
                        if let Some(body) = document_body(&value, "context_document") {
                            h3 { "Context" }
                            p { class: "muted", "{body}" }
                        }
                        if let Some(handoff) = document_body(&value, "latest_handoff") {
                            h3 { "Latest handoff" }
                            p { class: "muted", "{handoff}" }
                        }
                        h3 { "Linked libraries" }
                        for library in api::list(&value, &["libraries"]) {
                            div { class: "inline-row",
                                span { "{api::property(&library, &["title", "id", "library"])}" }
                                span { class: "muted", "{api::property(&library, &["kind", "library_kind"])}" }
                            }
                        }
                    }
                }
            }
            Card {
                title: "Project connections".to_owned(),
                description: "Explicit personal task links, goals, engineering runs, and source-owned project resources.".to_owned(),
                match overview.cloned() {
                    None => rsx! { Loading {} },
                    Some(Err(err)) => rsx! { Notice { message: err } },
                    Some(Ok(value)) => rsx! {
                        h3 { "Linked tasks" }
                        for task in api::list(&value, &["tasks"]) {
                            div { class: "inline-row",
                                span { "{api::property(&task, &["title"])}" }
                                Badge { text: api::property(&task, &["status"]) }
                            }
                        }
                        h3 { "Associated goals" }
                        for goal in api::list(&value, &["goals"]) {
                            div { class: "inline-row",
                                span { "{api::property(&goal, &["title"])}" }
                            }
                        }
                        h3 { "Engineering runs" }
                        for run in api::list(&value, &["engineering_runs"]) {
                            div { class: "inline-row",
                                span { class: "mono", "{api::property(&run, &["id"])}" }
                                Badge { text: api::property(&run, &["status"]) }
                            }
                        }
                        for missing in api::list(&value, &["unavailable"]) {
                            span { class: "muted", "Unavailable: {missing}" }
                        }
                    }
                }
            }
            Card {
                title: "Autonomous workflow activity".to_owned(),
                description: "Bounded read-only cross-capability work, with durable state and owner-controlled cancellation.".to_owned(),
                match workflows.cloned() {
                    None => rsx! { Loading {} },
                    Some(Err(err)) => rsx! { Notice { message: err } },
                    Some(Ok(value)) => {
                        let selected_project = selected();
                        let matching: Vec<Value> = api::list(&value, &["workflows"])
                            .into_iter()
                            .filter(|flow| flow.get("project").and_then(Value::as_str) == Some(selected_project.as_str()))
                            .collect();
                        rsx! {
                            if matching.is_empty() {
                                Empty {
                                    title: "No recorded workflows".to_owned(),
                                    description: "Submit an approved workflow through the assistant to begin a durable research or review process.".to_owned()
                                }
                            }
                            for flow in matching {
                                div { class: "inline-row",
                                    span { "{api::property(&flow, &["objective"])}" }
                                    Badge { text: api::property(&flow, &["status"]) }
                                }
                            }
                        }
                    }
                }
            }
            Card {
                title: "Source repositories".to_owned(),
                description: "Linked source and reference repositories remain separately versioned.".to_owned(),
                match repositories.cloned() {
                    None => rsx! { Loading {} },
                    Some(Err(err)) => rsx! { Notice { message: err } },
                    Some(Ok(value)) => rsx! {
                        for item in api::list(&value, &["result", "repositories"]) {
                            div { class: "inline-row",
                                span { class: "mono", "{api::property(&item, &["repository"])}" }
                                Badge { text: api::property(&item, &["role"]) }
                                span { class: "muted", "{api::property(&item, &["default_ref"])}" }
                            }
                        }
                    }
                }
            }
            Card {
                title: "Research search".to_owned(),
                description: "Search project-visible indexed passages without modifying the research library.".to_owned(),
                div { class: "inline-row",
                    input {
                        class: "input",
                        aria_label: "Research query",
                        placeholder: "Search indexed research",
                        value: query(),
                        oninput: move |event| query.set(event.value())
                    }
                    button {
                        class: "btn btn-outline",
                        disabled: query().trim().is_empty() || query().len() > 120,
                        onclick: move |_| submitted.set(query()),
                        "Search"
                    }
                }
                if !submitted().is_empty() {
                    match research.cloned() {
                        None => rsx! { Loading {} },
                        Some(Err(err)) => rsx! { Notice { message: err } },
                        Some(Ok(value)) => {
                            let matches = api::list(&value, &["result", "passages"]);
                            rsx! {
                                if matches.is_empty() {
                                    Empty { title: "No matching passages".to_owned(),
                                        description: "Try another term or add indexed sources through the research library.".to_owned() }
                                } else {
                                    for passage in matches {
                                        details {
                                            summary { "{api::property(&passage, &["title", "source", "id"])}" }
                                            p { class: "muted",
                                                "{api::property(&passage, &["text", "excerpt"])}"
                                            }
                                            span { class: "mono",
                                                "{api::property(&passage, &["citation_ref", "origin_revision", "revision"])}"
                                            }
                                        }
                                    }
                                }
                            }
                        }
                    }
                }
            }
        }
    }
}
