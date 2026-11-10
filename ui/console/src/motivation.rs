use dioxus::prelude::*;
use reqwest::Method;
use serde_json::{Value, json};

use crate::api;
use crate::components::{Card, Empty, Heading, Loading, Notice};

fn field(value: &Value, name: &str) -> String {
    value.get(name).and_then(Value::as_str).unwrap_or("").to_owned()
}

#[component]
pub fn Motivation(csrf: String) -> Element {
    let csrf = use_signal(|| csrf);
    let mut state = use_resource(|| async {
        api::get::<Value>("/v1/motivation/state").await
    });
    let mut initialized = use_signal(|| false);
    let mut goal_id = use_signal(String::new);
    let mut identity = use_signal(String::new);
    let mut outcome = use_signal(String::new);
    let mut next_step = use_signal(String::new);
    let mut enabled = use_signal(|| false);
    let mut intensity = use_signal(|| "guided".to_owned());
    let mut daily_limit = use_signal(|| 4u8);
    let mut quiet_start = use_signal(|| 0u8);
    let mut quiet_end = use_signal(|| 0u8);
    let mut engagement = use_signal(|| 3u8);
    let mut progress = use_signal(|| 3u8);
    let mut wellbeing = use_signal(|| 3u8);
    let mut notes = use_signal(String::new);
    let mut busy = use_signal(|| false);
    let mut error = use_signal(String::new);
    let mut notice = use_signal(String::new);

    use_effect(move || {
        if initialized() { return; }
        if let Some(Ok(snapshot)) = state.cloned() {
            if let Some(profile) = snapshot.get("profile") {
                goal_id.set(field(profile, "goal_id"));
                identity.set(field(profile, "identity"));
                outcome.set(field(profile, "outcome"));
                next_step.set(field(profile, "next_step"));
                enabled.set(profile.get("enabled").and_then(Value::as_bool).unwrap_or(false));
                intensity.set(field(profile, "intensity"));
                daily_limit.set(profile.get("daily_limit").and_then(Value::as_u64).unwrap_or(4) as u8);
                quiet_start.set(profile.get("quiet_start_utc_hour").and_then(Value::as_u64).unwrap_or(0) as u8);
                quiet_end.set(profile.get("quiet_end_utc_hour").and_then(Value::as_u64).unwrap_or(0) as u8);
                initialized.set(true);
            }
        }
    });
    let snapshot = state.cloned().and_then(Result::ok);
    let profile = snapshot.as_ref().and_then(|value| value.get("profile"));
    let revision = profile.and_then(|p| p.get("revision")).and_then(Value::as_u64).unwrap_or(0);
    let service_enabled = profile.and_then(|p| p.get("enabled")).and_then(Value::as_bool).unwrap_or(false);
    let interventions = snapshot.as_ref()
        .and_then(|value| value.get("interventions"))
        .and_then(Value::as_array).cloned().unwrap_or_default();
    let pending = interventions.iter().rev()
        .find(|item| item.get("feedback").is_none_or(Value::is_null) &&
            !item.get("cancelled").and_then(Value::as_bool).unwrap_or(false)).cloned();
    let pending_id = pending.as_ref().and_then(|item| item.get("id"))
        .and_then(Value::as_u64);

    rsx! {
        Heading {
            title: "Motivation".to_owned(),
            description: "Cultivate deliberate intellectual interests through owner-defined goals and measured interventions.".to_owned(),
            button {
                class: "btn btn-outline",
                onclick: move |_| { initialized.set(false); state.restart(); },
                "Refresh"
            }
        }
        if !error().is_empty() { Notice { message: error() } }
        if !notice().is_empty() {
            div { class: "alert", role: "status", "{notice()}" }
        }
        match state.cloned() {
            None => rsx! { Loading {} },
            Some(Err(message)) => rsx! { Notice { message } },
            Some(Ok(_)) => rsx! {
                Card {
                    title: "Aspirational identity".to_owned(),
                    description: "Choose a real outcome that excites you. The assistant cannot revise these commitments without your approval.".to_owned(),
                    form {
                        class: "task-editor-fields",
                        onsubmit: move |event| {
                            event.prevent_default();
                            if busy() { return; }
                            let payload = json!({
                                "expected_revision": revision,
                                "goal_id": goal_id(),
                                "identity": identity(),
                                "outcome": outcome(),
                                "next_step": next_step(),
                                "enabled": enabled(),
                                "intensity": intensity(),
                                "daily_limit": daily_limit(),
                                "quiet_start_utc_hour": quiet_start(),
                                "quiet_end_utc_hour": quiet_end()
                            });
                            let token = csrf();
                            busy.set(true);
                            error.set(String::new());
                            spawn(async move {
                                match api::write::<Value, _>(Method::PUT, "/v1/motivation/profile", &token, &payload).await {
                                    Ok(_) => {
                                        notice.set("Motivational profile saved".to_owned());
                                        state.restart();
                                    }
                                    Err(message) => error.set(message),
                                }
                                busy.set(false);
                            });
                        },
                        label { class: "form-row",
                            span { class: "label", "Linked goal ID (optional)" }
                            input { class: "field", value: "{goal_id()}",
                                maxlength: "128",
                                oninput: move |event| goal_id.set(event.value()) }
                        }
                        label { class: "form-row",
                            span { class: "label", "Who you intend to become" }
                            textarea { class: "field", required: true, maxlength: "240",
                                value: "{identity()}",
                                oninput: move |event| identity.set(event.value()) }
                        }
                        label { class: "form-row",
                            span { class: "label", "Compelling, authentic outcome" }
                            textarea { class: "field", required: true, maxlength: "400",
                                value: "{outcome()}",
                                oninput: move |event| outcome.set(event.value()) }
                        }
                        label { class: "form-row",
                            span { class: "label", "Concrete next step" }
                            textarea { class: "field", required: true, maxlength: "320",
                                value: "{next_step()}",
                                oninput: move |event| next_step.set(event.value()) }
                        }
                        div { class: "task-field-grid",
                            label { class: "form-row",
                                span { class: "label", "Intervention style" }
                                select { class: "field", value: "{intensity()}",
                                    onchange: move |event| intensity.set(event.value()),
                                    option { value: "guided", "Guided" }
                                    option { value: "direct", "Direct accountability" }
                                }
                            }
                            label { class: "form-row",
                                span { class: "label", "Daily maximum" }
                                input { class: "field", type: "number", min: "1", max: "8",
                                    value: "{daily_limit()}",
                                    oninput: move |event| {
                                        if let Ok(value) = event.value().parse::<u8>() { daily_limit.set(value); }
                                    } }
                            }
                            label { class: "form-row",
                                span { class: "label", "Quiet start (UTC hour)" }
                                input { class: "field", type: "number", min: "0", max: "23",
                                    value: "{quiet_start()}",
                                    oninput: move |event| {
                                        if let Ok(value) = event.value().parse::<u8>() { quiet_start.set(value); }
                                    } }
                            }
                            label { class: "form-row",
                                span { class: "label", "Quiet end (UTC hour)" }
                                input { class: "field", type: "number", min: "0", max: "23",
                                    value: "{quiet_end()}",
                                    oninput: move |event| {
                                        if let Ok(value) = event.value().parse::<u8>() { quiet_end.set(value); }
                                    } }
                            }
                        }
                        label { class: "form-row",
                            input { type: "checkbox", checked: enabled(),
                                onchange: move |event| enabled.set(event.checked()) }
                            span { class: "label", "Enable owner-requested motivational interventions" }
                        }
                        p { class: "card-description",
                            "The default is off. Equal quiet-hour values disable quiet hours. No automatic messages or device restrictions are installed."
                        }
                        div { class: "inline-row",
                            button { class: "btn btn-primary", disabled: busy(), "Save profile" }
                            button {
                                type: "button",
                                class: "btn btn-outline",
                                disabled: busy() || !service_enabled,
                                onclick: move |_| {
                                    let token = csrf();
                                    busy.set(true);
                                    error.set(String::new());
                                    spawn(async move {
                                        match api::write::<Value, _>(Method::POST, "/v1/motivation/pause", &token, &json!({})).await {
                                            Ok(_) => {
                                                enabled.set(false);
                                                notice.set("Interventions paused".to_owned());
                                                state.restart();
                                            }
                                            Err(message) => error.set(message),
                                        }
                                        busy.set(false);
                                    });
                                },
                                "Pause immediately"
                            }
                        }
                    }
                }
                Card {
                    title: "Intervention".to_owned(),
                    description: "Choose when to request one. Strategies adapt to self-reported feedback, not fabricated performance metrics.".to_owned(),
                    div { class: "inline-row",
                        button {
                            class: "btn btn-primary", disabled: busy() || !service_enabled,
                            onclick: move |_| {
                                let token = csrf();
                                busy.set(true);
                                error.set(String::new());
                                spawn(async move {
                                    match api::write::<Value, _>(Method::POST, "/v1/motivation/interventions", &token, &json!({})).await {
                                        Ok(_) => state.restart(),
                                        Err(message) => error.set(message),
                                    }
                                    busy.set(false);
                                });
                            },
                            "Request intervention"
                        }
                    }
                    if let Some(event) = pending {
                        div { class: "card",
                            div { class: "card-body",
                                p { {field(&event, "message")} }
                                p { class: "card-description", {format!("Strategy: {}", field(&event, "strategy"))} }
                            }
                        }
                        form {
                            class: "task-editor-fields",
                            onsubmit: move |event| {
                                event.prevent_default();
                                if busy() { return; }
                                let Some(id) = pending_id else { return; };
                                let payload = json!({
                                    "intervention_id": id,
                                    "engagement": engagement(),
                                    "progress": progress(),
                                    "wellbeing": wellbeing(),
                                    "notes": notes()
                                });
                                let token = csrf();
                                busy.set(true);
                                error.set(String::new());
                                spawn(async move {
                                    match api::write::<Value, _>(Method::POST, "/v1/motivation/feedback", &token, &payload).await {
                                        Ok(_) => {
                                            notes.set(String::new());
                                            notice.set("Self-reported feedback recorded".to_owned());
                                            state.restart();
                                        }
                                        Err(message) => error.set(message),
                                    }
                                    busy.set(false);
                                });
                            },
                            div { class: "task-field-grid",
                                label { class: "form-row",
                                    span { class: "label", "Engagement (0–5)" }
                                    input { class: "field", type: "number", min: "0", max: "5",
                                        value: "{engagement()}",
                                        oninput: move |event| {
                                            if let Ok(value) = event.value().parse::<u8>() { engagement.set(value); }
                                        } }
                                }
                                label { class: "form-row",
                                    span { class: "label", "Actual progress (0–5)" }
                                    input { class: "field", type: "number", min: "0", max: "5",
                                        value: "{progress()}",
                                        oninput: move |event| {
                                            if let Ok(value) = event.value().parse::<u8>() { progress.set(value); }
                                        } }
                                }
                                label { class: "form-row",
                                    span { class: "label", "Well-being (0–5)" }
                                    input { class: "field", type: "number", min: "0", max: "5",
                                        value: "{wellbeing()}",
                                        oninput: move |event| {
                                            if let Ok(value) = event.value().parse::<u8>() { wellbeing.set(value); }
                                        } }
                                }
                            }
                            label { class: "form-row",
                                span { class: "label", "Evidence or observations (optional)" }
                                textarea { class: "field", maxlength: "400", value: "{notes()}",
                                    oninput: move |event| notes.set(event.value()) }
                            }
                            p { class: "card-description", "A well-being score of 0 or 1 pauses further interventions. Record 0 for an intervention you chose not to follow." }
                            button { class: "btn btn-primary", disabled: busy(), "Record feedback" }
                        }
                    } else {
                        Empty {
                            title: "No outstanding intervention".to_owned(),
                            description: "Request one when you wish to pursue a chosen goal.".to_owned()
                        }
                    }
                }
                Card {
                    title: "Recorded experiments".to_owned(),
                    description: "Personal observations remain in the private motivation service and do not constitute verified learning results.".to_owned(),
                    if interventions.is_empty() {
                        Empty {
                            title: "No feedback yet".to_owned(),
                            description: "Your completed interventions will appear here.".to_owned()
                        }
                    } else {
                        for event in interventions.iter().rev().take(12) {
                            div { class: "list-row",
                                div {
                                    strong { {field(event, "strategy")} }
                                    p { class: "card-description", {field(event, "message")} }
                                    if let Some(feedback) = event.get("feedback").filter(|v| !v.is_null()) {
                                        p { class: "card-description",
                                            {format!("Engagement {} / 5 · Progress {} / 5 · Well-being {} / 5",
                                                api::property(feedback, &["engagement"]),
                                                api::property(feedback, &["progress"]),
                                                api::property(feedback, &["wellbeing"]))}
                                        }
                                    } else {
                                        p { class: "card-description", "Awaiting feedback" }
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
