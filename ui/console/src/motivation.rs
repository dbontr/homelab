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
    let mut proactive_enabled = use_signal(|| false);
    let mut cadence_minutes = use_signal(|| 240u16);
    let mut holdout_day = use_signal(|| "none".to_owned());
    let mut quiet_start = use_signal(|| 0u8);
    let mut quiet_end = use_signal(|| 0u8);
    let mut engagement = use_signal(|| 3u8);
    let mut progress = use_signal(|| 3u8);
    let mut wellbeing = use_signal(|| 3u8);
    let mut notes = use_signal(String::new);
    let mut session_minutes = use_signal(|| 45u16);
    let mut self_initiated = use_signal(|| true);
    let mut fascination = use_signal(|| 3u8);
    let mut session_wellbeing = use_signal(|| 3u8);
    let mut evidence_ref = use_signal(String::new);
    let mut focus_duration = use_signal(|| 45u16);
    let mut focus_reflection = use_signal(String::new);
    let mut trigger = use_signal(|| "session_start".to_owned());
    let mut current_question = use_signal(String::new);
    let mut metrics = use_resource(|| async {
        api::get::<Value>("/v1/motivation/metrics").await
    });
    // The service schedules in-app prompts. Refresh only while this view is open.
    use_future(move || async move {
        loop {
            gloo_timers::future::TimeoutFuture::new(60_000).await;
            state.restart();
            metrics.restart();
        }
    });
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
                proactive_enabled.set(profile.get("proactive_enabled").and_then(Value::as_bool).unwrap_or(false));
                cadence_minutes.set(profile.get("cadence_minutes").and_then(Value::as_u64).unwrap_or(240) as u16);
                holdout_day.set(profile.get("holdout_weekday_utc").and_then(Value::as_u64)
                    .map(|day| day.to_string()).unwrap_or_else(|| "none".to_owned()));
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
    let focus_active = snapshot.as_ref()
        .and_then(|value| value.get("focus_sessions"))
        .and_then(Value::as_array)
        .and_then(|items| items.iter().rev().find(|s|
            s.get("status").and_then(Value::as_str) == Some("active")))
        .cloned();
    let active_focus_id = focus_active.as_ref()
        .and_then(|item| item.get("id")).and_then(Value::as_u64);

    rsx! {
        Heading {
            title: "Motivation".to_owned(),
            description: "Cultivate deliberate intellectual interests through owner-defined goals and measured interventions.".to_owned(),
            button {
                class: "btn btn-outline",
                onclick: move |_| { initialized.set(false); state.restart(); metrics.restart(); },
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
                                "proactive_enabled": proactive_enabled(),
                                "cadence_minutes": cadence_minutes(),
                                "holdout_weekday_utc": holdout_day().parse::<u8>().ok(),
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
                                input { class: "field", r#type: "number", min: "1", max: "8",
                                    value: "{daily_limit()}",
                                    oninput: move |event| {
                                        if let Ok(value) = event.value().parse::<u8>() { daily_limit.set(value); }
                                    } }
                            }
                            label { class: "form-row",
                                span { class: "label", "Quiet start (UTC hour)" }
                                input { class: "field", r#type: "number", min: "0", max: "23",
                                    value: "{quiet_start()}",
                                    oninput: move |event| {
                                        if let Ok(value) = event.value().parse::<u8>() { quiet_start.set(value); }
                                    } }
                            }
                            label { class: "form-row",
                                span { class: "label", "Quiet end (UTC hour)" }
                                input { class: "field", r#type: "number", min: "0", max: "23",
                                    value: "{quiet_end()}",
                                    oninput: move |event| {
                                        if let Ok(value) = event.value().parse::<u8>() { quiet_end.set(value); }
                                    } }
                            }
                        }
                        label { class: "form-row",
                            input { r#type: "checkbox", checked: enabled(),
                                onchange: move |event| enabled.set(event.checked()) }
                            span { class: "label", "Enable owner-requested motivational interventions" }
                        }
                        label { class: "form-row",
                            input { r#type: "checkbox", checked: proactive_enabled(),
                                onchange: move |event| proactive_enabled.set(event.checked()) }
                            span { class: "label", "Schedule prompts to the private in-app inbox" }
                        }
                        label { class: "form-row",
                            span { class: "label", "Scheduled prompt interval (60–1440 minutes)" }
                            input { class: "field", r#type: "number", min: "60", max: "1440",
                                value: "{cadence_minutes()}",
                                oninput: move |event| {
                                    if let Ok(value) = event.value().parse::<u16>() { cadence_minutes.set(value); }
                                } }
                        }
                        label { class: "form-row",
                            span { class: "label", "Weekly intervention-free day (UTC)" }
                            select { class: "field", value: "{holdout_day()}",
                                onchange: move |event| holdout_day.set(event.value()),
                                option { value: "none", "No scheduled holdout" }
                                option { value: "0", "Sunday" }
                                option { value: "1", "Monday" }
                                option { value: "2", "Tuesday" }
                                option { value: "3", "Wednesday" }
                                option { value: "4", "Thursday" }
                                option { value: "5", "Friday" }
                                option { value: "6", "Saturday" }
                            }
                        }
                        p { class: "card-description",
                            "Defaults are off. Quiet hours use UTC; identical start and end disables quiet hours. A holdout day suppresses scheduled prompts so voluntary interest can be observed. External messages and device restrictions are not enabled."
                        }
                        div { class: "inline-row",
                            button { class: "btn btn-primary", disabled: busy(), "Save profile" }
                            button {
                                r#type: "button",
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
                    label { class: "form-row",
                        span { class: "label", "Current situation" }
                        select { class: "field", value: "{trigger()}",
                            onchange: move |event| trigger.set(event.value()),
                            option { value: "session_start", "Beginning meaningful work" }
                            option { value: "resistance", "Resisting the next step" }
                            option { value: "distraction", "Attention drifting" }
                            option { value: "uncertainty", "Unsure how to proceed" }
                            option { value: "milestone", "A meaningful result" }
                        }
                    }
                    label { class: "form-row",
                        span { class: "label", "Authentic question (optional)" }
                        input { class: "field", maxlength: "320", value: "{current_question()}",
                            placeholder: "Which relationship am I trying to derive?",
                            oninput: move |event| current_question.set(event.value()) }
                    }
                    div { class: "inline-row",
                        button {
                            class: "btn btn-primary", disabled: busy() || !service_enabled,
                            onclick: move |_| {
                                let token = csrf();
                                busy.set(true);
                                error.set(String::new());
                                spawn(async move {
                                    match api::write::<Value, _>(Method::POST, "/v1/motivation/interventions", &token,
                                        &json!({"trigger":trigger(),"question":current_question()})).await {
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
                        button {
                            class: "btn btn-outline",
                            disabled: busy(),
                            onclick: move |_| {
                                let Some(id) = pending_id else { return; };
                                let token = csrf();
                                busy.set(true);
                                error.set(String::new());
                                spawn(async move {
                                    match api::write::<Value, _>(Method::POST,
                                        "/v1/motivation/dismiss", &token, &json!({"id":id})).await {
                                        Ok(_) => {
                                            notice.set("Intervention dismissed without feedback".to_owned());
                                            state.restart();
                                        }
                                        Err(message) => error.set(message),
                                    }
                                    busy.set(false);
                                });
                            },
                            "Dismiss without rating"
                        }
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
                                    input { class: "field", r#type: "number", min: "0", max: "5",
                                        value: "{engagement()}",
                                        oninput: move |event| {
                                            if let Ok(value) = event.value().parse::<u8>() { engagement.set(value); }
                                        } }
                                }
                                label { class: "form-row",
                                    span { class: "label", "Actual progress (0–5)" }
                                    input { class: "field", r#type: "number", min: "0", max: "5",
                                        value: "{progress()}",
                                        oninput: move |event| {
                                            if let Ok(value) = event.value().parse::<u8>() { progress.set(value); }
                                        } }
                                }
                                label { class: "form-row",
                                    span { class: "label", "Well-being (0–5)" }
                                    input { class: "field", r#type: "number", min: "0", max: "5",
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
                    title: "Voluntary focus commitment".to_owned(),
                    description: "Commit to a concrete intellectual step. The server records the elapsed interval; actual work remains yours to verify. You may always end a session.".to_owned(),
                    if let Some(active) = focus_active {
                        p { class: "card-description",
                            {format!("Current step: {}", field(&active, "step"))}
                        }
                        p { class: "card-description",
                            {format!("Target: {} minutes · Started (Unix UTC): {}",
                                api::property(&active, &["target_minutes"]),
                                api::property(&active, &["started_at"]))}
                        }
                        label { class: "form-row",
                            span { class: "label", "Reflection or reason for changing course" }
                            textarea { class: "field", maxlength: "400",
                                value: "{focus_reflection()}",
                                oninput: move |event| focus_reflection.set(event.value()) }
                        }
                        div { class: "inline-row",
                            button {
                                class: "btn btn-primary",
                                disabled: busy(),
                                onclick: move |_| {
                                    let Some(id) = active_focus_id else { return; };
                                    let token = csrf();
                                    busy.set(true);
                                    error.set(String::new());
                                    spawn(async move {
                                        let payload=json!({"id":id,"completed":true,"reflection":focus_reflection()});
                                        match api::write::<Value,_>(Method::POST,
                                            "/v1/motivation/focus/end",&token,&payload).await {
                                            Ok(_) => {
                                                focus_reflection.set(String::new());
                                                notice.set("Focus outcome recorded".to_owned());
                                                state.restart();
                                                metrics.restart();
                                            }
                                            Err(message) => error.set(message),
                                        }
                                        busy.set(false);
                                    });
                                },
                                "Finish commitment"
                            }
                            button {
                                class: "btn btn-outline",
                                disabled: busy() || focus_reflection().trim().chars().count() < 5,
                                onclick: move |_| {
                                    let Some(id) = active_focus_id else { return; };
                                    let token = csrf();
                                    busy.set(true);
                                    error.set(String::new());
                                    spawn(async move {
                                        let payload=json!({"id":id,"completed":false,"reflection":focus_reflection()});
                                        match api::write::<Value,_>(Method::POST,
                                            "/v1/motivation/focus/end",&token,&payload).await {
                                            Ok(_) => {
                                                focus_reflection.set(String::new());
                                                notice.set("Deliberate early exit recorded".to_owned());
                                                state.restart();
                                                metrics.restart();
                                            }
                                            Err(message) => error.set(message),
                                        }
                                        busy.set(false);
                                    });
                                },
                                "End early"
                            }
                            button {
                                class: "btn btn-outline",
                                disabled: busy(),
                                onclick: move |_| {
                                    let Some(id) = active_focus_id else { return; };
                                    let token = csrf();
                                    busy.set(true);
                                    error.set(String::new());
                                    spawn(async move {
                                        let payload=json!({"id":id,"completed":false,"emergency":true});
                                        match api::write::<Value,_>(Method::POST,
                                            "/v1/motivation/focus/end",&token,&payload).await {
                                            Ok(_) => {
                                                focus_reflection.set(String::new());
                                                notice.set("Focus commitment ended".to_owned());
                                                state.restart();
                                                metrics.restart();
                                            }
                                            Err(message) => error.set(message),
                                        }
                                        busy.set(false);
                                    });
                                },
                                "Leave immediately"
                            }
                        }
                    } else {
                        form {
                            class: "task-editor-fields",
                            onsubmit: move |event| {
                                event.prevent_default();
                                if busy() { return; }
                                let payload=json!({"target_minutes":focus_duration()});
                                let token=csrf();
                                busy.set(true);
                                error.set(String::new());
                                spawn(async move {
                                    match api::write::<Value,_>(Method::POST,
                                        "/v1/motivation/focus",&token,&payload).await {
                                        Ok(_) => {
                                            notice.set("Focus commitment opened".to_owned());
                                            state.restart();
                                            metrics.restart();
                                        }
                                        Err(message) => error.set(message),
                                    }
                                    busy.set(false);
                                });
                            },
                            label { class: "form-row",
                                span { class: "label", "Intended focus interval (10–240 minutes)" }
                                input { class: "field", r#type: "number", min: "10", max: "240",
                                    value: "{focus_duration()}",
                                    oninput: move |event| {
                                        if let Ok(value)=event.value().parse::<u16>() {
                                            focus_duration.set(value);
                                        }
                                    } }
                            }
                            p { class: "card-description",
                                "This uses the next step in your current aspirational identity. It remains available even when motivational prompts are disabled."
                            }
                            button { class: "btn btn-primary", disabled: busy(), "Begin focus" }
                        }
                    }
                }
                Card {
                    title: "Independent intellectual engagement".to_owned(),
                    description: "Record genuine intellectual work, including days without any intervention. These are self-reports, not verified learning achievements.".to_owned(),
                    form {
                        class: "task-editor-fields",
                        onsubmit: move |event| {
                            event.prevent_default();
                            if busy() { return; }
                            let payload = json!({
                                "minutes": session_minutes(),
                                "self_initiated": self_initiated(),
                                "fascination": fascination(),
                                "wellbeing": session_wellbeing(),
                                "intervention_id": if self_initiated() { None } else { pending_id },
                                "evidence_ref": evidence_ref()
                            });
                            let token = csrf();
                            busy.set(true);
                            error.set(String::new());
                            spawn(async move {
                                match api::write::<Value, _>(Method::POST,
                                    "/v1/motivation/checkins", &token, &payload).await {
                                    Ok(_) => {
                                        notice.set("Engagement record saved".to_owned());
                                        evidence_ref.set(String::new());
                                        state.restart();
                                        metrics.restart();
                                    }
                                    Err(message) => error.set(message),
                                }
                                busy.set(false);
                            });
                        },
                        div { class: "task-field-grid",
                            label { class: "form-row",
                                span { class: "label", "Intellectual work (minutes)" }
                                input { class: "field", r#type: "number", min: "1", max: "720",
                                    value: "{session_minutes()}",
                                    oninput: move |event| {
                                        if let Ok(value) = event.value().parse::<u16>() { session_minutes.set(value); }
                                    } }
                            }
                            label { class: "form-row",
                                span { class: "label", "Fascination (0–5)" }
                                input { class: "field", r#type: "number", min: "0", max: "5",
                                    value: "{fascination()}",
                                    oninput: move |event| {
                                        if let Ok(value) = event.value().parse::<u8>() { fascination.set(value); }
                                    } }
                            }
                            label { class: "form-row",
                                span { class: "label", "Well-being (0–5)" }
                                input { class: "field", r#type: "number", min: "0", max: "5",
                                    value: "{session_wellbeing()}",
                                    oninput: move |event| {
                                        if let Ok(value) = event.value().parse::<u8>() { session_wellbeing.set(value); }
                                    } }
                            }
                        }
                        label { class: "form-row",
                            input { r#type: "checkbox", checked: self_initiated(),
                                onchange: move |event| self_initiated.set(event.checked()) }
                            span { class: "label", "I chose to begin this work independently" }
                        }
                        label { class: "form-row",
                            span { class: "label", "Evidence reference (optional)" }
                            input { class: "field", maxlength: "128", value: "{evidence_ref()}",
                                placeholder: "research:proof-note-17",
                                oninput: move |event| evidence_ref.set(event.value()) }
                        }
                        button { class: "btn btn-primary", disabled: busy(), "Record engagement" }
                    }
                }
                Card {
                    title: "Last 28 days".to_owned(),
                    description: "A longitudinal view of voluntary effort and fascination. These observations are not causal proof of a motivational intervention's effectiveness.".to_owned(),
                    match metrics.cloned() {
                        None => rsx! { Loading {} },
                        Some(Err(message)) => rsx! { Notice { message } },
                        Some(Ok(value)) => rsx! {
                            p { {format!("Sessions: {} · Minutes: {} · Self-initiated minutes: {}",
                                api::property(&value, &["sessions"]),
                                api::property(&value, &["minutes"]),
                                api::property(&value, &["self_initiated_minutes"]))} }
                            p { {format!("Holdout minutes: {} · Self-initiated holdout minutes: {}",
                                api::property(&value, &["holdout_minutes"]),
                                api::property(&value, &["holdout_self_initiated_minutes"]))} }
                            p { {format!("Focus completed: {} · Ended early: {} · Logged elapsed focus minutes: {}",
                                api::property(&value, &["focus_completed"]),
                                api::property(&value, &["focus_abandoned"]),
                                api::property(&value, &["focus_elapsed_minutes"]))} }
                            p { {format!("Average fascination: {} / 5 · Average well-being: {} / 5",
                                api::property(&value, &["mean_fascination"]),
                                api::property(&value, &["mean_wellbeing"]))} }
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
