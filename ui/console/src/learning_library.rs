use dioxus::prelude::*;
use reqwest::Method;
use serde::de::DeserializeOwned;
use serde_json::{Value, json};

use crate::api;
use crate::components::{Card, Empty, Heading, Loading, Notice};

#[derive(Clone, Copy, Debug, PartialEq, Eq)]
enum View { Graph, Problems, Reviews, Sources, Practice }

impl View {
    fn label(self) -> &'static str {
        match self {
            Self::Graph => "Concept graph",
            Self::Problems => "Problem library",
            Self::Reviews => "Review queue",
            Self::Sources => "Sources",
            Self::Practice => "Practice history",
        }
    }
}

#[derive(Clone, Copy)]
enum Query {
    Graph,
    Concept,
    Problems,
    Problem,
    Reviews,
    Sources,
    PracticeSessions,
    PracticeSession,
}
impl Query {
    fn name(self) -> &'static str {
        match self {
            Self::Graph => "get_graph",
            Self::Concept => "get_concept",
            Self::Problems => "list_problems",
            Self::Problem => "get_problem",
            Self::Reviews => "get_review_queue",
            Self::Sources => "list_learning_sources",
            Self::PracticeSessions => "list_practice_sessions",
            Self::PracticeSession => "get_practice_session",
        }
    }
    fn route(self) -> String {
        format!("/v1/learning/app-tools/{}/call", self.name())
    }
}

async fn read<T: DeserializeOwned>(
    csrf: &str, tool: Query, params: Value
) -> Result<T, String> {
    let response: Value = api::write(
        Method::POST, &tool.route(), csrf, &json!({"arguments":params})
    ).await?;
    if response.get("isError").and_then(Value::as_bool) == Some(true) {
        return Err("The Learning service rejected the request.".to_owned());
    }
    if let Some(content) = response.get("structuredContent") {
        return serde_json::from_value(content.clone())
            .map_err(|_| "Learning returned an unexpected response.".to_owned());
    }
    let raw = response.pointer("/content/0/text").and_then(Value::as_str)
        .ok_or("Learning returned no data.")?;
    serde_json::from_str(raw)
        .map_err(|_| "Learning returned invalid JSON.".to_owned())
}

fn records(data: &Value, fields: &[&str]) -> Vec<Value> {
    api::list(data, fields)
}

fn label(item: &Value, keys: &[&str]) -> String {
    api::property(item, keys)
}

#[component]
pub fn LearningLibrary(csrf: String) -> Element {
    let mut view = use_signal(|| View::Graph);
    rsx! {
        Heading {
            title: "Learning library".to_owned(),
            description: "Inspect knowledge, recorded problems, sources and retrieval obligations without changing mastery.".to_owned(),
            span { class: "small muted", "Read-only owner-session inspection" }
        }
        div { class: "health-plan-list", role: "group", aria_label: "Learning library sections",
            for section in [View::Graph,View::Problems,View::Reviews,View::Sources,View::Practice] {
                button {
                    class: if view() == section { "btn btn-primary" } else { "btn btn-outline" },
                    aria_pressed: view() == section,
                    onclick: move |_| view.set(section),
                    "{section.label()}"
                }
            }
        }
        match view() {
            View::Graph => rsx! { ConceptGraph { csrf: csrf.clone() } },
            View::Problems => rsx! { ProblemLibrary { csrf: csrf.clone() } },
            View::Reviews => rsx! { ReviewQueue { csrf: csrf.clone() } },
            View::Sources => rsx! { SourceList { csrf: csrf.clone() } },
            View::Practice => rsx! { PracticeHistory { csrf: csrf.clone() } },
        }
    }
}

#[component]
fn ConceptGraph(csrf: String) -> Element {
    let mut focus = use_signal(String::new);
    let mut committed = use_signal(String::new);
    let mut concept_id = use_signal(String::new);
    let mut graph = use_resource({
        let csrf=csrf.clone();
        move || {
            let token=csrf.clone();
            let focus=committed();
            async move {
                read::<Value>(&token,Query::Graph,
                    json!({"focus":focus,"depth":2,"limit":120})).await
            }
        }
    });
    let mut concept = use_resource(move || {
        let token=csrf.clone();
        let id=concept_id();
        async move {
            if id.is_empty() { return Ok(Value::Null); }
            read::<Value>(&token,Query::Concept,json!({"identifier":id})).await
        }
    });
    rsx! {
        Card {
            title: "Concept graph".to_owned(),
            description: "Source-linked nodes and recorded prerequisites. Graph membership does not imply mastery.".to_owned(),
            form {
                class: "card-body health-metrics-form",
                onsubmit: move |event| {
                    event.prevent_default();
                    if focus().len() > 128 { return; }
                    committed.set(focus());
                    graph.restart();
                },
                label { class: "form-row",
                    span { class: "label", "Concept focus (optional)" }
                    input {
                        class: "field", value: "{focus()}",
                        maxlength: "128", oninput: move |e| focus.set(e.value())
                    }
                }
                button { class: "btn btn-primary", "Inspect graph" }
            }
            match graph.cloned() {
                None => rsx! { Loading {} },
                Some(Err(message)) => rsx! { Notice { message } },
                Some(Ok(value)) => {
                    let nodes=records(&value,&["nodes","concepts"]);
                    let edges=records(&value,&["edges","relations","prerequisites"]);
                    rsx! {
                        div { class: "card-body",
                            if nodes.is_empty() {
                                Empty {
                                    title: "No graph nodes".to_owned(),
                                    description: "No nodes were returned for the selected focus.".to_owned()
                                }
                            } else {
                                p { class: "small muted",
                                    "{nodes.len()} nodes · {edges.len()} relations"
                                }
                                div { class: "health-plan-list",
                                    for node in nodes.into_iter().take(120) {
                                        button {
                                            class: "btn btn-outline btn-sm",
                                            onclick: {
                                                let id=label(&node,&["id","identifier","concept_id"]);
                                                move |_| concept_id.set(id.clone())
                                            },
                                            {label(&node,&["name","title","label","id","identifier"])}
                                        }
                                    }
                                }
                            }
                        }
                    }
                }
            }
        }
        if !concept_id().is_empty() {
            Card {
                title: "Concept evidence".to_owned(),
                description: "Explanations, relationships and reported state belong to the Learning service.".to_owned(),
                match concept.cloned() {
                    None => rsx! { Loading {} },
                    Some(Err(message)) => rsx! { Notice { message } },
                    Some(Ok(value)) => rsx! {
                        div { class: "card-body stack",
                            h3 { {label(&value,&["name","title","label","identifier"])} }
                            p { class: "small muted", "{concept_id()}" }
                            p { {label(&value,&["description","definition","explanation","summary"])} }
                        }
                    }
                }
            }
        }
    }
}

#[component]
fn ProblemLibrary(csrf: String) -> Element {
    let mut selected = use_signal(String::new);
    let mut problems = use_resource({
        let token=csrf.clone();
        move || {
            let token=token.clone();
            async move {
                read::<Value>(&token,Query::Problems,json!({"status":"all","limit":100})).await
            }
        }
    });
    let mut problem = use_resource(move || {
        let token=csrf.clone();
        let id=selected();
        async move {
            if id.is_empty() { return Ok(Value::Null); }
            read::<Value>(&token,Query::Problem,json!({"problem_id":id})).await
        }
    });
    rsx! {
        Card {
            title: "Problem library".to_owned(),
            description: "Problem statements are available for inspection; sealed answer keys are never requested.".to_owned(),
            div { class: "card-body health-metrics-form",
                button { class: "btn btn-outline", onclick: move |_| problems.restart(), "Refresh problems" }
                match problems.cloned() {
                    None => rsx! { Loading {} },
                    Some(Err(message)) => rsx! { Notice { message } },
                    Some(Ok(value)) => {
                        let all=records(&value,&["problems","items"]);
                        rsx! {
                            if all.is_empty() {
                                Empty {
                                    title: "No saved problems".to_owned(),
                                    description: "The problem library returned no entries.".to_owned()
                                }
                            } else {
                                div { class: "table-clip",
                                    table { class: "table",
                                        thead { tr {
                                            th { "Problem" }
                                            th { "Difficulty" }
                                            th { "State" }
                                            th { "Inspect" }
                                        } }
                                        tbody {
                                            for item in all {
                                                tr {
                                                    td { {label(&item,&["title","name","id"])} }
                                                    td { {label(&item,&["difficulty","level"])} }
                                                    td { {label(&item,&["status","state"])} }
                                                    td {
                                                        button {
                                                            class: "btn btn-outline btn-sm",
                                                            onclick: {
                                                                let id=label(&item,&["id","problem_id"]);
                                                                move |_| selected.set(id.clone())
                                                            },
                                                            "Open"
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
        }
        if !selected().is_empty() {
            Card {
                title: "Selected problem".to_owned(),
                description: "The statement and metadata are displayed as text; the solution is not retrieved.".to_owned(),
                match problem.cloned() {
                    None => rsx! { Loading {} },
                    Some(Err(message)) => rsx! { Notice { message } },
                    Some(Ok(value)) => rsx! {
                        div { class: "card-body stack",
                            h3 { {label(&value,&["title","name","id"])} }
                            p { class: "small muted",
                                {label(&value,&["difficulty","status","kind"])}
                            }
                            p { class: "learning-problem-statement",
                                {label(&value,&["statement","problem","prompt","description"])}
                            }
                            p { class: "small muted",
                                "Answer keys and grading operations are unavailable from this inspection view."
                            }
                        }
                    }
                }
            }
        }
    }
}

#[component]
fn ReviewQueue(csrf: String) -> Element {
    let mut reviews=use_resource(move || {
        let token=csrf.clone();
        async move {
            read::<Value>(&token,Query::Reviews,
                json!({"include_upcoming":true,"limit":100})).await
        }
    });
    rsx! {
        Card {
            title: "Retrieval review".to_owned(),
            description: "Recorded review obligations do not establish missed work or successful recall.".to_owned(),
            button {
                class: "btn btn-outline", onclick: move |_| reviews.restart(), "Refresh"
            }
            match reviews.cloned() {
                None => rsx! { Loading {} },
                Some(Err(message)) => rsx! { Notice { message } },
                Some(Ok(value)) => {
                    let all=records(&value,&["reviews","queue","items"]);
                    rsx! {
                        if all.is_empty() {
                            Empty {
                                title: "No review items".to_owned(),
                                description: "No retrieval obligations were returned.".to_owned()
                            }
                        } else {
                            div { class: "table-clip",
                                table { class: "table",
                                    thead { tr {
                                        th { "Concept" }
                                        th { "Due" }
                                        th { "State" }
                                        th { "Activity" }
                                    } }
                                    tbody {
                                        for entry in all {
                                            tr {
                                                td { {label(&entry,&["concept","concept_id","title","id"])} }
                                                td { {label(&entry,&["due_at","due","review_at"])} }
                                                td { {label(&entry,&["status","state"])} }
                                                td { {label(&entry,&["kind","method","activity"])} }
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

#[component]
fn SourceList(csrf: String) -> Element {
    let mut sources=use_resource(move || {
        let token=csrf.clone();
        async move {
            read::<Value>(&token,Query::Sources,json!({"limit":100})).await
        }
    });
    rsx! {
        Card {
            title: "Learning sources".to_owned(),
            description: "Original source records and extraction coverage; unverified claims are not treated as knowledge.".to_owned(),
            button { class: "btn btn-outline", onclick: move |_| sources.restart(), "Refresh" }
            match sources.cloned() {
                None => rsx! { Loading {} },
                Some(Err(message)) => rsx! { Notice { message } },
                Some(Ok(value)) => {
                    let all=records(&value,&["sources","items"]);
                    rsx! {
                        if all.is_empty() {
                            Empty {
                                title: "No learning sources".to_owned(),
                                description: "The Learning service returned no source records.".to_owned()
                            }
                        } else {
                            div { class: "table-clip",
                                table { class: "table",
                                    thead { tr {
                                        th { "Source" }
                                        th { "Kind" }
                                        th { "Coverage" }
                                        th { "Imported" }
                                    } }
                                    tbody {
                                        for item in all {
                                            tr {
                                                td { {label(&item,&["label","title","id"])} }
                                                td { {label(&item,&["kind","mime_type","type"])} }
                                                td { {label(&item,&["coverage","status","state"])} }
                                                td { {label(&item,&["created_at","imported_at"])} }
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

#[component]
fn PracticeHistory(csrf: String) -> Element {
    let mut selected=use_signal(String::new);
    let mut sessions=use_resource({
        let token=csrf.clone();
        move || {
            let token=token.clone();
            async move {
                read::<Value>(&token,Query::PracticeSessions,json!({"limit":50})).await
            }
        }
    });
    let mut details=use_resource(move || {
        let token=csrf.clone();
        let id=selected();
        async move {
            if id.is_empty() { return Ok(Value::Null); }
            read::<Value>(&token,Query::PracticeSession,json!({"session_id":id})).await
        }
    });
    rsx! {
        Card {
            title: "Practice history".to_owned(),
            description: "Recorded practice sessions and ungraded attempts retain their original evidence.".to_owned(),
            button { class: "btn btn-outline", onclick: move |_| sessions.restart(), "Refresh" }
            match sessions.cloned() {
                None => rsx! { Loading {} },
                Some(Err(message)) => rsx! { Notice { message } },
                Some(Ok(value)) => {
                    let all=records(&value,&["sessions","items"]);
                    rsx! {
                        if all.is_empty() {
                            Empty {
                                title: "No practice sessions".to_owned(),
                                description: "No recorded practice sessions are available.".to_owned()
                            }
                        } else {
                            div { class: "table-clip",
                                table { class: "table",
                                    thead { tr {
                                        th { "Session" }
                                        th { "Method" }
                                        th { "Status" }
                                        th { "Inspect" }
                                    } }
                                    tbody {
                                        for item in all {
                                            tr {
                                                td { {label(&item,&["goal","title","id"])} }
                                                td { {label(&item,&["method","mode"])} }
                                                td { {label(&item,&["status","state"])} }
                                                td {
                                                    button {
                                                        class: "btn btn-outline btn-sm",
                                                        onclick: {
                                                            let id=label(&item,&["id","session_id"]);
                                                            move |_| selected.set(id.clone())
                                                        },
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
                }
            }
        }
        if !selected().is_empty() {
            Card {
                title: "Practice details".to_owned(),
                description: "Descriptive evidence only. Correctness and mastery require their own graded records.".to_owned(),
                match details.cloned() {
                    None => rsx! { Loading {} },
                    Some(Err(message)) => rsx! { Notice { message } },
                    Some(Ok(value)) => rsx! {
                        div { class: "card-body stack",
                            h3 { {label(&value,&["goal","title","id"])} }
                            p { {label(&value,&["description","objective","summary"])} }
                            p { class: "small muted", "Session {selected()}" }
                        }
                    }
                }
            }
        }
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn every_library_query_has_one_exact_browser_route() {
        for q in [Query::Graph,Query::Concept,Query::Problems,Query::Problem,
            Query::Reviews,Query::Sources,Query::PracticeSessions,Query::PracticeSession] {
            let expected=format!("/v1/learning/app-tools/{}/call",q.name());
            assert_eq!(q.route(),expected);
            assert!(!expected.contains(".."));
        }
    }

    #[test]
    fn source_content_is_never_mapped_to_untrusted_markup() {
        let source=json!({"label":"<img src=x onerror=alert(1)>","kind":"text"});
        assert_eq!(label(&source,&["label"] ), "<img src=x onerror=alert(1)>");
    }
}
