use dioxus::prelude::*;
use reqwest::Method;
use serde_json::{json, Value};

use crate::api;
use crate::components::{Card, Empty, Loading, Notice};

fn valid_session_id(id: &str) -> bool {
    !id.is_empty() && id.len() <= 128 && id.bytes().all(|c|
        c.is_ascii_alphanumeric() || c == b'-' || c == b'_'
    )
}

fn next_phases(current: &str) -> &'static [&'static str] {
    match current {
        "diagnose" => &["plan"],
        "plan" => &["diagnose", "teach"],
        "teach" => &["plan", "practice"],
        "practice" => &["teach", "reassess"],
        "reassess" => &["practice", "complete", "diagnose"],
        _ => &[],
    }
}

fn permits_plan(phase: &str) -> bool {
    matches!(phase, "plan" | "teach" | "practice" | "reassess")
}

fn expected_revision(record: &Value) -> Option<i64> {
    record.get("revision")?.as_i64().filter(|revision| *revision >= 0)
}

// A phase change is revision-fenced, and the Learning store verifies
// prerequisite evidence, pending grading, and all allowed transitions.
#[component]
pub fn LearningRoute(
    csrf: String,
    session_id: String,
    detail: Value,
    on_saved: EventHandler<()>,
) -> Element {
    let mut target_phase = use_signal(String::new);
    let mut confirm_complete = use_signal(|| false);
    let mut busy = use_signal(|| false);
    let mut error = use_signal(String::new);
    let mut notice = use_signal(String::new);

    let mut next = use_resource({
        let id = session_id.clone();
        move || {
            let id = id.clone();
            async move {
                if !valid_session_id(&id) {
                    return Err("Invalid learning session identifier.".to_owned());
                }
                api::get::<Value>(&format!("/v1/learning/sessions/{id}/next")).await
            }
        }
    });
    let mut probe = use_resource({
        let id = session_id.clone();
        let phase = api::property(&detail, &["phase"]);
        move || {
            let id = id.clone();
            let phase = phase.clone();
            async move {
                if !valid_session_id(&id) {
                    return Err("Invalid learning session identifier.".to_owned());
                }
                if phase != "diagnose" && phase != "reassess" {
                    return Ok(Value::Null);
                }
                api::get::<Value>(&format!("/v1/learning/sessions/{id}/probe")).await
            }
        }
    });

    let phase = api::property(&detail, &["phase"]);
    let revision = expected_revision(&detail);
    let choices = next_phases(&phase);
    let has_ungraded_question = detail.get("pending_question")
        .filter(|question| !question.is_null())
        .is_some_and(|question| question.get("status").and_then(Value::as_str) != Some("graded"));
    let steps = api::list(&detail, &["steps"]);
    let build_allowed = permits_plan(&phase) && !has_ungraded_question && revision.is_some();
    let transition_allowed = choices.contains(&target_phase().as_str())
        && !has_ungraded_question && revision.is_some()
        && (target_phase() != "complete" || confirm_complete());

    rsx! {
        Card {
            title: "Next learning action".to_owned(),
            description: "Suggestions identify unresolved work. They are not graded evidence of understanding.".to_owned(),
            div { class: "card-body stack",
                button {
                    class: "btn btn-outline", r#type: "button",
                    onclick: move |_| {
                        next.restart();
                        probe.restart();
                    },
                    "Refresh recommendation"
                }
                match next.cloned() {
                    None => rsx! { Loading {} },
                    Some(Err(message)) => rsx! { Notice { message } },
                    Some(Ok(value)) => rsx! {
                        div { class: "kv-list",
                            div { class: "kv-entry",
                                span { class: "small muted", "Recommended action" }
                                strong { {api::property(&value, &["action"])} }
                            }
                            div { class: "kv-entry",
                                span { class: "small muted", "Reason" }
                                span { {api::property(&value, &["reason"])} }
                            }
                            if let Some(step) = value.get("step_title").and_then(Value::as_str) {
                                div { class: "kv-entry",
                                    span { class: "small muted", "Reasoning step" }
                                    span { "{step}" }
                                }
                            }
                            if let Some(concept) = value.get("concept_id").and_then(Value::as_str) {
                                div { class: "kv-entry",
                                    span { class: "small muted", "Concept" }
                                    span { "{concept}" }
                                }
                            }
                        }
                    },
                }
                if phase == "diagnose" || phase == "reassess" {
                    match probe.cloned() {
                        None => rsx! { Loading {} },
                        Some(Err(message)) => rsx! { Notice { message } },
                        Some(Ok(value)) => rsx! {
                            p { class: "small muted",
                                "Diagnostic: {api::property(&value, &["reason"])}"
                            }
                            p { {api::property(&value, &["title", "concept_id"])} }
                        }
                    }
                }
            }
        }
        Card {
            title: "Prerequisite route".to_owned(),
            description: "A reasoning step is independently supported only after an explicit evaluation.".to_owned(),
            if steps.is_empty() {
                Empty {
                    title: "No reasoning route".to_owned(),
                    description: "Build a prerequisite plan after diagnosis. The plan does not award mastery.".to_owned()
                }
            } else {
                div { class: "card-body stack",
                    for (index, step) in steps.iter().enumerate() {
                        div { class: "task-item", key: "{index}",
                            div { class: "task-information",
                                span { class: "task-title",
                                    {api::property(step, &["title", "concept_id", "id"])}
                                }
                                span { class: "small muted",
                                    {format!("{} · ", api::property(step, &["kind"]))}
                                    if step.get("independently_supported")
                                        .and_then(Value::as_bool) == Some(true) {
                                        "Independently supported"
                                    } else {
                                        "Requires evidence"
                                    }
                                }
                                if let Some(premises) = step.get("premises").and_then(Value::as_array) {
                                    p { class: "small muted",
                                        {format!("Premises: {}", premises.iter().filter_map(Value::as_str).collect::<Vec<_>>().join(", "))}
                                    }
                                }
                            }
                        }
                    }
                }
            }
        }
        Card {
            title: "Session progression".to_owned(),
            description: "Each change uses the current session revision. Reload after a conflict or new assessment.".to_owned(),
            div { class: "card-body stack",
                if !error().is_empty() { Notice { message: error() } }
                if !notice().is_empty() {
                    p { class: "small muted", role: "status", "{notice()}" }
                }
                if has_ungraded_question {
                    p { class: "small muted",
                        "An unresolved question must be assessed before the session can advance."
                    }
                }
                if permits_plan(&phase) {
                    button {
                        class: "btn btn-outline",
                        r#type: "button",
                        disabled: !build_allowed || busy(),
                        onclick: {
                            let token = csrf.clone();
                            let id = session_id.clone();
                            move |_| {
                                let Some(rev) = revision else { return };
                                if !valid_session_id(&id) || busy() { return }
                                let url = format!("/v1/learning/sessions/{id}/plan");
                                let payload = json!({"expected_revision":rev});
                                let csrf = token.clone();
                                busy.set(true);
                                error.set(String::new());
                                notice.set(String::new());
                                spawn(async move {
                                    match api::write::<Value,_>(Method::POST, &url, &csrf, &payload).await {
                                        Ok(_) => {
                                            notice.set("Prerequisite route saved.".to_owned());
                                            on_saved.call(());
                                            next.restart();
                                        }
                                        Err(message) => error.set(format!(
                                            "Cannot update the learning plan: {message}. Refresh the session before retrying."
                                        )),
                                    }
                                    busy.set(false);
                                });
                            }
                        },
                        "Build prerequisite plan"
                    }
                }
                if choices.is_empty() {
                    p { class: "small muted", "This phase has no further manual transitions." }
                } else {
                    label { class: "form-row",
                        span { class: "label", "Next phase" }
                        select {
                            class: "field",
                            disabled: busy(),
                            value: "{target_phase()}",
                            onchange: move |event| {
                                target_phase.set(event.value());
                                confirm_complete.set(false);
                            },
                            option { value: "", "Select a phase" }
                            for choice in choices {
                                option { value: "{choice}", "{choice}" }
                            }
                        }
                    }
                    if target_phase() == "complete" {
                        label { class: "inline-row",
                            input {
                                r#type: "checkbox",
                                disabled: busy(),
                                checked: confirm_complete(),
                                onchange: move |event| confirm_complete.set(event.checked())
                            }
                            span { "Request completion; Learning must independently confirm mastery and reassessment." }
                        }
                    }
                    button {
                        class: "btn btn-primary",
                        r#type: "button",
                        disabled: !transition_allowed || busy(),
                        onclick: {
                            let token = csrf.clone();
                            let id = session_id.clone();
                            move |_| {
                                let choice = target_phase();
                                if busy() || !next_phases(&phase).contains(&choice.as_str())
                                    || (choice == "complete" && !confirm_complete()) {
                                    return;
                                }
                                let Some(rev) = revision else { return };
                                if !valid_session_id(&id) { return; }
                                let csrf = token.clone();
                                let url = format!("/v1/learning/sessions/{id}/phase");
                                let payload = json!({"expected_revision":rev,"next_phase":choice});
                                busy.set(true);
                                error.set(String::new());
                                notice.set(String::new());
                                spawn(async move {
                                    match api::write::<Value,_>(Method::POST, &url, &csrf, &payload).await {
                                        Ok(_) => {
                                            target_phase.set(String::new());
                                            confirm_complete.set(false);
                                            notice.set("Session phase recorded.".to_owned());
                                            on_saved.call(());
                                            next.restart();
                                            probe.restart();
                                        }
                                        Err(message) => error.set(format!(
                                            "Session phase was not changed: {message}. Refresh before retrying."
                                        )),
                                    }
                                    busy.set(false);
                                });
                            }
                        },
                        "Advance session"
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
    fn phase_choices_match_learning_state_machine() {
        assert_eq!(next_phases("diagnose"), &["plan"]);
        assert_eq!(next_phases("plan"), &["diagnose", "teach"]);
        assert_eq!(next_phases("teach"), &["plan", "practice"]);
        assert_eq!(next_phases("practice"), &["teach", "reassess"]);
        assert_eq!(next_phases("reassess"), &["practice", "complete", "diagnose"]);
        assert!(next_phases("complete").is_empty());
        assert!(next_phases("arbitrary").is_empty());
    }

    #[test]
    fn revision_and_id_are_required() {
        assert_eq!(expected_revision(&json!({"revision": 0})), Some(0));
        assert!(expected_revision(&json!({"revision": -1})).is_none());
        assert!(expected_revision(&json!({})).is_none());
        assert!(valid_session_id("a-1_2"));
        assert!(!valid_session_id("../bad"));
        assert!(!valid_session_id("a/b"));
        assert!(!valid_session_id(""));
    }
}
