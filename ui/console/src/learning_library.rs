use dioxus::prelude::*;
use reqwest::Method;
use serde::de::DeserializeOwned;
use serde_json::{Value, json};

use crate::api;
use crate::components::{Card, Empty, Heading, Loading, Notice};

#[derive(Clone, Copy, Debug, PartialEq, Eq)]
enum View { Graph, Reasoning, Problems, Reviews, Sources, Practice }

impl View {
    fn label(self) -> &'static str {
        match self {
            Self::Graph => "Concept graph",
            Self::Reasoning => "Reasoning",
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
    ReasoningGraph,
    Proposition,
    ReasoningChain,
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
            Self::ReasoningGraph => "get_reasoning_graph",
            Self::Proposition => "get_proposition",
            Self::ReasoningChain => "get_reasoning_chain",
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
            for section in [View::Graph,View::Reasoning,View::Problems,View::Reviews,View::Sources,View::Practice] {
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
            View::Reasoning => rsx! { ReasoningGraph { csrf: csrf.clone() } },
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
fn ReasoningGraph(csrf: String) -> Element {
    let mut proposition_id = use_signal(String::new);
    let mut chain_id = use_signal(String::new);
    let mut graph = use_resource({
        let token=csrf.clone();
        move || {
            let token=token.clone();
            async move {
                read::<Value>(&token,Query::ReasoningGraph,json!({"limit":150})).await
            }
        }
    });
    let mut proposition = use_resource({
        let token=csrf.clone();
        move || {
            let token=token.clone();
            let id=proposition_id();
            async move {
                if id.is_empty() {return Ok(Value::Null);}
                read::<Value>(&token,Query::Proposition,json!({"proposition_id":id})).await
            }
        }
    });
    let mut chain = use_resource(move || {
        let token=csrf.clone();
        let id=chain_id();
        async move {
            if id.is_empty() {return Ok(Value::Null);}
            read::<Value>(&token,Query::ReasoningChain,json!({"chain_id":id})).await
        }
    });
    rsx! {
        Card {
            title: "Reasoning provenance".to_owned(),
            description: "Review recorded propositions, premise groups, and disputes without inferring logical or source validity.".to_owned(),
            button { class: "btn btn-outline", onclick: move |_| graph.restart(), "Refresh reasoning" }
            match graph.cloned() {
                None => rsx! { Loading {} },
                Some(Err(message)) => rsx! { Notice { message } },
                Some(Ok(value)) => {
                    let claims=records(&value,&["propositions"]);
                    let chains=records(&value,&["chains"]);
                    let disputes=records(&value,&["disputes"]);
                    rsx! {
                        div { class: "card-body stack",
                            p { class: "small muted",
                                "{claims.len()} propositions · {chains.len()} reasoning chains · {disputes.len()} disputes"
                            }
                            if claims.is_empty() {
                                Empty {
                                    title: "No propositions".to_owned(),
                                    description: "No recorded propositions were returned.".to_owned()
                                }
                            } else {
                                h3 { "Propositions" }
                                div { class: "health-plan-list",
                                    for item in claims {
                                        button {
                                            class: "btn btn-outline btn-sm",
                                            onclick: {
                                                let id=label(&item,&["id","proposition_id"]);
                                                move |_| proposition_id.set(id.clone())
                                            },
                                            {label(&item,&["statement","title","id"])}
                                        }
                                    }
                                }
                            }
                            if !chains.is_empty() {
                                h3 { "Reasoning chains" }
                                div { class: "table-clip",
                                    table { class: "table",
                                        thead { tr {
                                            th { "Relation" }
                                            th { "Conclusion" }
                                            th { "Review" }
                                            th { "Inspect" }
                                        } }
                                        tbody {
                                            for item in chains {
                                                tr {
                                                    td { {label(&item,&["relation","kind"])} }
                                                    td { {label(&item,&["conclusion","conclusion_id"])} }
                                                    td {
                                                        if item.get("review_active").and_then(Value::as_bool) == Some(true) {
                                                            "Reviewed, current prerequisites"
                                                        } else {
                                                            "Unreviewed or blocked"
                                                        }
                                                    }
                                                    td {
                                                        button {
                                                            class: "btn btn-outline btn-sm",
                                                            onclick: {
                                                                let id=label(&item,&["id","chain_id"]);
                                                                move |_| chain_id.set(id.clone())
                                                            },
                                                            "Open chain"
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
        if !proposition_id().is_empty() {
            Card {
                title: "Proposition".to_owned(),
                description: "Original claim and source context; confidence is not automatically upgraded.".to_owned(),
                match proposition.cloned() {
                    None => rsx! { Loading {} },
                    Some(Err(message)) => rsx! { Notice { message } },
                    Some(Ok(value)) => rsx! {
                        div { class: "card-body stack",
                            p { class: "learning-problem-statement",
                                {label(&value,&["statement","title","id"])}
                            }
                            p { class: "small muted",
                                {label(&value,&["status","review_status"])}
                            }
                            p { {label(&value,&["context","source_reference","explanation"])} }
                        }
                    }
                }
            }
        }
        if !chain_id().is_empty() {
            Card {
                title: "Reasoning chain".to_owned(),
                description: "All premises are required together. A formal propositional result does not certify the interpretation.".to_owned(),
                match chain.cloned() {
                    None => rsx! { Loading {} },
                    Some(Err(message)) => rsx! { Notice { message } },
                    Some(Ok(value)) => {
                        let premises=value.get("premises").and_then(Value::as_array)
                            .map(|items| items.iter().map(|item|
                                item.as_str().map(str::to_owned).unwrap_or_else(|| item.to_string())
                            ).collect::<Vec<_>>().join(" AND "))
                            .unwrap_or_else(|| "Premises unavailable".to_owned());
                        let formal=value.get("latest_formal_check").cloned().unwrap_or(Value::Null);
                        rsx! {
                            div { class: "card-body stack",
                                p { class: "small muted",
                                    "Relation: {label(&value,&["relation","kind"])}"
                                }
                                p { "Premises: {premises}" }
                                p { "Conclusion: {label(&value,&["conclusion","conclusion_id"])}" }
                                p { class: "learning-problem-statement",
                                    {label(&value,&["explanation","rationale"])}
                                }
                                p { class: "small muted",
                                    if value.get("review_active").and_then(Value::as_bool)==Some(true) {
                                        "Reviewed and prerequisites current"
                                    } else {
                                        "Review is absent or blocked"
                                    }
                                }
                                if !formal.is_null() {
                                    p { class: "small muted",
                                        if formal.get("claim_snapshot_current").and_then(Value::as_bool)==Some(true) {
                                            {format!("Formal check: {}",label(&formal,&["verdict"]))}
                                        } else {
                                            "Formal check is stale for the current proposition wording"
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
fn ProblemLibrary(csrf: String) -> Element {
    let mut selected = use_signal(String::new);
    let attachments_csrf = csrf.clone();
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
            crate::learning_attachments::LearningAttachments {
                key: "{selected()}",
                csrf: attachments_csrf.clone(),
                owner_kind: "problem".to_owned(),
                owner_id: selected(),
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
    let attachments_csrf = csrf.clone();
    let curriculum_csrf = csrf.clone();
    let mut selected_source = use_signal(String::new);
    let mut selected_curriculum = use_signal(String::new);
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
                                        th { "Files" }
                                        th { "Curriculum" }
                                    } }
                                    tbody {
                                        for item in all {
                                            tr {
                                                td { {label(&item,&["label","title","id"])} }
                                                td { {label(&item,&["kind","mime_type","type"])} }
                                                td { {label(&item,&["coverage","status","state"])} }
                                                td { {label(&item,&["created_at","imported_at"])} }
                                                td {
                                                    button {
                                                        class: "btn btn-outline btn-sm",
                                                        r#type: "button",
                                                        onclick: {
                                                            let id = label(&item,&["id"]);
                                                            move |_| selected_source.set(id.clone())
                                                        },
                                                        "Originals"
                                                    }
                                                }
                                                td {
                                                    button {
                                                        class: "btn btn-outline btn-sm",
                                                        r#type: "button",
                                                        onclick: {
                                                            let id = label(&item, &["id"]);
                                                            move |_| selected_curriculum.set(id.clone())
                                                        },
                                                        "Review curriculum"
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
        if !selected_source().is_empty() {
            crate::learning_attachments::LearningAttachments {
                key: "{selected_source()}",
                csrf: attachments_csrf.clone(),
                owner_kind: "source".to_owned(),
                owner_id: selected_source(),
            }
        }
        if !selected_curriculum().is_empty() {
            SourceCurriculum {
                key: "{selected_curriculum()}",
                csrf: curriculum_csrf.clone(),
                source_id: selected_curriculum(),
                on_imported: move |_| sources.restart(),
            }
        }
    }
}

fn valid_curriculum_source_id(id: &str) -> bool {
    !id.is_empty() && id.len() <= 128
        && id.bytes().all(|byte| byte.is_ascii_alphanumeric() || byte == b'-' || byte == b'_')
}

fn importable_curriculum(plan: &Value) -> Option<&str> {
    if plan.get("ready").and_then(Value::as_bool) != Some(true) {
        return None;
    }
    let sha = plan.get("sha256").and_then(Value::as_str)?;
    if sha.len() != 64 || !sha.bytes().all(|byte|
        byte.is_ascii_digit() || (b'a'..=b'f').contains(&byte)) {
        return None;
    }
    Some(sha)
}

#[component]
fn SourceCurriculum(csrf: String, source_id: String, on_imported: EventHandler<()>) -> Element {
    let mut preview = use_signal(|| None::<Value>);
    let mut receipt = use_signal(|| None::<Value>);
    let mut confirmed = use_signal(|| false);
    let mut busy = use_signal(|| false);
    let mut error = use_signal(String::new);

    let plan = preview().and_then(|value| value.get("curriculum").cloned());
    let digest = plan.as_ref().and_then(importable_curriculum).map(str::to_owned);
    let steps = plan.as_ref().map(|value| records(value, &["steps"]))
        .unwrap_or_default();

    rsx! {
        Card {
            title: "Source curriculum".to_owned(),
            description: "Review source-grounded concepts and prerequisites before importing. Import does not award mastery.".to_owned(),
            div { class: "card-body stack",
                p { class: "small muted", "Source: {source_id}" }
                if !error().is_empty() { Notice { message: error() } }
                button {
                    class: "btn btn-outline", r#type: "button",
                    disabled: busy() || !valid_curriculum_source_id(&source_id),
                    onclick: {
                        let token = csrf.clone();
                        let id = source_id.clone();
                        move |_| {
                            if busy() || !valid_curriculum_source_id(&id) { return; }
                            let path = format!("/v1/learning/sources/{id}/curriculum/preview");
                            let token = token.clone();
                            preview.set(None);
                            receipt.set(None);
                            confirmed.set(false);
                            error.set(String::new());
                            busy.set(true);
                            spawn(async move {
                                match api::write::<Value,_>(
                                    Method::POST, &path, &token, &json!({"limit":80})
                                ).await {
                                    Ok(value) => preview.set(Some(value)),
                                    Err(message) => error.set(format!(
                                        "Cannot preview the source curriculum: {message}"
                                    )),
                                }
                                busy.set(false);
                            });
                        }
                    },
                    "Preview reviewed curriculum"
                }
                if let Some(value) = plan.as_ref() {
                    div { class: "kv-list",
                        div { class: "kv-entry",
                            span { class: "small muted", "Unprocessed spans" }
                            strong { {label(value, &["remaining_spans"])} }
                        }
                        div { class: "kv-entry",
                            span { class: "small muted", "Unreviewed claims" }
                            strong { {label(value, &["unreviewed_claims"])} }
                        }
                        div { class: "kv-entry",
                            span { class: "small muted", "Reviewed concepts" }
                            strong { "{steps.len()}" }
                        }
                    }
                    if let Some(blockers) = value.get("blockers").and_then(Value::as_array) {
                        if !blockers.is_empty() {
                            div { class: "alert", role: "status",
                                strong { "Import is blocked." }
                                for blocker in blockers {
                                    p { {blocker.as_str().unwrap_or("Unknown source-review blocker")} }
                                }
                            }
                        }
                    }
                    if steps.is_empty() {
                        Empty {
                            title: "No audited curriculum".to_owned(),
                            description: "Extract, audit and review source claims before importing.".to_owned(),
                        }
                    } else {
                        div { class: "task-items",
                            for (index, step) in steps.iter().enumerate() {
                                div { class: "task-item", key: "{index}",
                                    div { class: "task-information",
                                        strong { {label(step, &["title","statement"])} }
                                        span { class: "small muted", {label(step, &["domain"])} }
                                        if let Some(quote) = step.get("quote").and_then(Value::as_str) {
                                            p { class: "small muted", "{quote}" }
                                        }
                                        p { class: "small muted",
                                            "Prerequisites: {records(step,&["dependencies"]).len()}"
                                        }
                                    }
                                }
                            }
                        }
                    }
                    if let Some(sha) = digest.as_ref() {
                        label { class: "inline-row",
                            input {
                                r#type: "checkbox",
                                disabled: busy(),
                                checked: confirmed(),
                                onchange: move |event| confirmed.set(event.checked()),
                            }
                            span { "Import this audited prerequisite route without awarding learner mastery." }
                        }
                        button {
                            class: "btn btn-primary", r#type: "button",
                            disabled: busy() || !confirmed(),
                            onclick: {
                                let token = csrf.clone();
                                let id = source_id.clone();
                                let sha = sha.clone();
                                move |_| {
                                    if busy() || !confirmed() || !valid_curriculum_source_id(&id) {
                                        return;
                                    }
                                    let token = token.clone();
                                    let path = format!("/v1/learning/sources/{id}/curriculum/import");
                                    let payload = json!({"limit":80,"expected_sha256":sha,"confirm":true});
                                    busy.set(true);
                                    error.set(String::new());
                                    spawn(async move {
                                        match api::write::<Value,_>(
                                            Method::POST, &path, &token, &payload
                                        ).await {
                                            Ok(value) if value.get("mastery_awarded")
                                                .and_then(Value::as_bool)==Some(false) => {
                                                receipt.set(Some(value));
                                                confirmed.set(false);
                                                on_imported.call(());
                                            }
                                            Ok(_) => error.set(
                                                "The Learning service returned an unexpected import receipt.".to_owned()
                                            ),
                                            Err(message) => error.set(format!(
                                                "Import was not confirmed: {message}. Preview again before retrying."
                                            )),
                                        }
                                        busy.set(false);
                                    });
                                }
                            },
                            "Import confirmed curriculum"
                        }
                        p { class: "small muted",
                            "Import uses the exact preview digest. Source changes invalidate the confirmation."
                        }
                    }
                }
                if let Some(result) = receipt() {
                    div { class: "alert", role: "status",
                        strong { "Curriculum recorded" }
                        p { "Concepts created: {label(&result, &["concepts_created"])}" }
                        p { "Prerequisites linked: {label(&result, &["prerequisites_linked"])}" }
                        p { "No mastery was awarded." }
                    }
                }
            }
        }
    }
}

#[cfg(test)]
mod curriculum_tests {
    use super::*;

    #[test]
    fn curriculum_requires_completed_review_and_exact_evidence_digest() {
        let accepted = json!({"ready":true,"sha256":"a".repeat(64)});
        assert!(importable_curriculum(&accepted).is_some());
        assert!(importable_curriculum(&json!({"ready":false,"sha256":"a".repeat(64)})).is_none());
        assert!(importable_curriculum(&json!({"ready":true,"sha256":"a".repeat(63)})).is_none());
        assert!(importable_curriculum(&json!({"ready":true,"sha256":"A".repeat(64)})).is_none());
        assert!(valid_curriculum_source_id("source-01_example"));
        assert!(!valid_curriculum_source_id("../outside"));
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
            Query::Reviews,Query::Sources,Query::PracticeSessions,Query::PracticeSession,
            Query::ReasoningGraph,Query::Proposition,Query::ReasoningChain] {
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
