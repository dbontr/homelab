use dioxus::prelude::*;
use gloo_timers::future::TimeoutFuture;
use serde_json::{Value, json};

use crate::components::{Card, Empty, Heading, Loading, Notice};
use crate::health_tools::{self, Tool};
use crate::workout_planner::Plan;

fn optional_measurement(value: &str, maximum: f64) -> Result<Option<f64>, String> {
    if value.trim().is_empty() {
        return Ok(None);
    }
    match value.parse::<f64>() {
        Ok(number) if number.is_finite() && (0.0..=maximum).contains(&number) => Ok(Some(number)),
        _ => Err("Training measurements must be finite and within range.".to_owned()),
    }
}

fn current_utc() -> String {
    js_sys::Date::new_0()
        .to_iso_string()
        .as_string()
        .unwrap_or_default()
}

fn start_rest(mut remaining: Signal<u32>, mut generation: Signal<u64>, seconds: u32) {
    let token = generation().wrapping_add(1);
    generation.set(token);
    remaining.set(seconds);
    spawn(async move {
        for left in (0..seconds).rev() {
            TimeoutFuture::new(1000).await;
            if generation() != token {
                return;
            }
            remaining.set(left);
        }
    });
}

#[component]
pub fn WorkoutCompanion(csrf: String) -> Element {
    let csrf = use_signal(|| csrf);
    let mut plans = use_resource(move || {
        let token = csrf();
        async move { health_tools::call::<Vec<Plan>>(&token, Tool::PlanList, json!({})).await }
    });
    let mut selected_plan = use_signal(String::new);
    let mut selected_session = use_signal(String::new);
    let mut exercise = use_signal(String::new);
    let mut activity = use_signal(|| "strength".to_owned());
    let mut set_number = use_signal(|| "1".to_owned());
    let mut repetitions = use_signal(|| "8".to_owned());
    let mut load = use_signal(String::new);
    let mut duration = use_signal(String::new);
    let mut distance = use_signal(String::new);
    let mut rpe = use_signal(String::new);
    let mut completion_minutes = use_signal(|| "0".to_owned());
    let mut completion_notes = use_signal(String::new);
    let mut confirm_completion = use_signal(|| false);
    let mut rest_duration = use_signal(|| "90".to_owned());
    let mut rest_remaining = use_signal(|| 0_u32);
    let mut rest_generation = use_signal(|| 0_u64);
    let mut busy = use_signal(|| false);
    let mut error = use_signal(String::new);
    let mut status = use_signal(String::new);
    let mut history = use_resource(move || {
        let token = csrf();
        let plan_id = selected_plan();
        async move {
            if plan_id.is_empty() {
                return Ok(Vec::<Value>::new());
            }
            health_tools::call::<Vec<Value>>(
                &token,
                Tool::TrainingSetList,
                json!({"plan_id":plan_id,"limit":500}),
            )
            .await
        }
    });
    let plan = plans.cloned().and_then(Result::ok).and_then(|all| {
        all.into_iter()
            .find(|p| p.kind == "workout" && p.id == selected_plan())
    });
    let session = plan
        .as_ref()
        .and_then(|p| p.sessions.iter().find(|s| s.id == selected_session()));

    rsx! {
        Heading {
            title: "Workout companion".to_owned(),
            description: "Log performed sets and confirm completed workouts without inferring missed sessions.".to_owned(),
            button {
                class: "btn btn-outline", onclick: move |_| {
                    plans.restart(); history.restart();
                },
                "Refresh"
            }
        }
        if !error().is_empty() { Notice { message: error() } }
        Card {
            title: "Scheduled workout".to_owned(),
            description: "Select a plan and its exact session before recording performed work.".to_owned(),
            div { class: "card-body health-companion-section",
                match plans.cloned() {
                    None => rsx! { Loading {} },
                    Some(Err(message)) => rsx! { Notice { message } },
                    Some(Ok(records)) => rsx! {
                        label { class: "form-row",
                            span { class: "label", "Workout plan" }
                            select {
                                class: "field", value: "{selected_plan()}",
                                disabled: busy(),
                                onchange: move |event| {
                                    selected_plan.set(event.value());
                                    selected_session.set(String::new());
                                    exercise.set(String::new());
                                },
                                option { value: "", "Select a plan" }
                                for record in records.into_iter().filter(|p| p.kind == "workout") {
                                    option { value: "{record.id}", "{record.title}" }
                                }
                            }
                        }
                    },
                }
                if let Some(plan) = &plan {
                    label { class: "form-row",
                        span { class: "label", "Scheduled session" }
                        select {
                            class: "field", value: "{selected_session()}",
                            disabled: busy(),
                            onchange: move |event| {
                                selected_session.set(event.value());
                                exercise.set(String::new());
                                history.restart();
                            },
                            option { value: "", "Select a session" }
                            for item in &plan.sessions {
                                option {
                                    value: "{item.id}",
                                    "{item.date} — {item.title}"
                                }
                            }
                        }
                    }
                }
                if let Some(session) = session {
                    div { class: "health-companion-exercises",
                        for planned in &session.exercises {
                            button {
                                class: "btn btn-outline", r#type: "button",
                                disabled: busy(),
                                onclick: {
                                    let selected = planned.clone();
                                    move |_| {
                                        exercise.set(selected.name.clone());
                                        repetitions.set(selected.repetitions.to_string());
                                    }
                                },
                                "{planned.name}: {planned.sets} × {planned.repetitions} — {planned.target}"
                            }
                        }
                    }
                }
                match history.cloned() {
                    None => rsx! { Loading {} },
                    Some(Err(message)) => rsx! { Notice { message } },
                    Some(Ok(records)) => {
                        let count = records.into_iter().filter(|item|
                            item.get("session_id").and_then(Value::as_str)
                                == Some(selected_session().as_str())
                        ).count();
                        rsx! {
                            p { class: "small muted",
                                "{count} recorded set or interval entries for this session. A rescheduled session retains its identity."
                            }
                        }
                    }
                }
            }
        }
        if session.is_some() {
            Card {
                title: "Log a performed set".to_owned(),
                description: "Values describe completed work, not planned or inferred performance.".to_owned(),
                form {
                    class: "card-body health-companion-section",
                    onsubmit: move |event| {
                        event.prevent_default();
                        if busy() { return; }
                        let Some(set) = set_number().parse::<i32>().ok()
                            .filter(|value| *value >= 0) else {
                            error.set("Enter a nonnegative set number.".to_owned()); return;
                        };
                        let Some(reps) = repetitions().parse::<i32>().ok()
                            .filter(|value| *value >= 0) else {
                            error.set("Enter nonnegative performed repetitions.".to_owned()); return;
                        };
                        if exercise().trim().is_empty() {
                            error.set("Choose an exercise.".to_owned()); return;
                        }
                        let Ok(weight) = optional_measurement(&load(), 1_000_000.0) else {
                            error.set("Enter a valid load in kilograms.".to_owned()); return;
                        };
                        let Ok(seconds) = optional_measurement(&duration(), 1_000_000.0) else {
                            error.set("Enter a valid duration in seconds.".to_owned()); return;
                        };
                        let Ok(meters) = optional_measurement(&distance(), 1_000_000_000.0) else {
                            error.set("Enter a valid distance in meters.".to_owned()); return;
                        };
                        let Ok(effort) = optional_measurement(&rpe(), 10.0) else {
                            error.set("RPE must be between zero and ten.".to_owned()); return;
                        };
                        let payload = json!({
                            "plan_id": selected_plan(), "session_id": selected_session(),
                            "exercise": exercise(), "activity": activity(),
                            "set_number": set, "repetitions": reps,
                            "load_kg": weight, "duration_seconds": seconds,
                            "distance_meters": meters, "rpe": effort,
                            "source": "manual", "at": current_utc(),
                        });
                        let token = csrf();
                        busy.set(true);
                        error.set(String::new());
                        spawn(async move {
                            match health_tools::call::<Value>(
                                &token, Tool::TrainingSetLog, payload,
                            ).await {
                                Ok(_) => {
                                    set_number.set((set + 1).to_string());
                                    status.set("Performed set recorded.".to_owned());
                                    history.restart();
                                }
                                Err(message) => error.set(message),
                            }
                            busy.set(false);
                        });
                    },
                    div { class: "task-field-grid",
                        label { class: "form-row",
                            span { class: "label", "Exercise" }
                            input { class: "field", required: true, value: "{exercise()}",
                                disabled: busy(), oninput: move |e| exercise.set(e.value()) }
                        }
                        label { class: "form-row",
                            span { class: "label", "Activity" }
                            select { class: "field", value: "{activity()}", disabled: busy(),
                                onchange: move |e| activity.set(e.value()),
                                option { value: "strength", "Strength" }
                                option { value: "cardio", "Cardio" }
                                option { value: "mobility", "Mobility" }
                            }
                        }
                        label { class: "form-row",
                            span { class: "label", "Set number" }
                            input { class: "field", r#type: "number", required: true, min: "0",
                                value: "{set_number()}", disabled: busy(),
                                oninput: move |e| set_number.set(e.value()) }
                        }
                        label { class: "form-row",
                            span { class: "label", "Performed reps" }
                            input { class: "field", r#type: "number", required: true, min: "0",
                                value: "{repetitions()}", disabled: busy(),
                                oninput: move |e| repetitions.set(e.value()) }
                        }
                        label { class: "form-row",
                            span { class: "label", "Load (kg)" }
                            input { class: "field", r#type: "number", min: "0", step: "any",
                                value: "{load()}", disabled: busy(),
                                oninput: move |e| load.set(e.value()) }
                        }
                        label { class: "form-row",
                            span { class: "label", "Duration (seconds)" }
                            input { class: "field", r#type: "number", min: "0", step: "any",
                                value: "{duration()}", disabled: busy(),
                                oninput: move |e| duration.set(e.value()) }
                        }
                        label { class: "form-row",
                            span { class: "label", "Distance (meters)" }
                            input { class: "field", r#type: "number", min: "0", step: "any",
                                value: "{distance()}", disabled: busy(),
                                oninput: move |e| distance.set(e.value()) }
                        }
                        label { class: "form-row",
                            span { class: "label", "RPE (0–10)" }
                            input { class: "field", r#type: "number", min: "0", max: "10", step: "0.5",
                                value: "{rpe()}", disabled: busy(),
                                oninput: move |e| rpe.set(e.value()) }
                        }
                    }
                    button { class: "btn btn-primary", disabled: busy(),
                        if busy() { "Saving…" } else { "Log performed set" }
                    }
                    p { class: "small muted", role: "status", aria_live: "polite", "{status()}" }
                }
            }
            Card {
                title: "Rest interval".to_owned(),
                description: "A local countdown. It does not create or alter a training record.".to_owned(),
                div { class: "card-body health-companion-section",
                    label { class: "form-row",
                        span { class: "label", "Rest seconds (15–600)" }
                        input { class: "field", r#type: "number", min: "15", max: "600",
                            value: "{rest_duration()}",
                            oninput: move |event| rest_duration.set(event.value()) }
                    }
                    div { class: "health-plan-actions",
                        button { class: "btn btn-outline", r#type: "button",
                            onclick: move |_| {
                                let seconds = rest_duration().parse::<u32>().ok()
                                    .filter(|value| (15..=600).contains(value)).unwrap_or(90);
                                start_rest(rest_remaining, rest_generation, seconds);
                            },
                            "Start rest"
                        }
                        button { class: "btn btn-outline", r#type: "button",
                            onclick: move |_| {
                                rest_generation.set(rest_generation().wrapping_add(1));
                                rest_remaining.set(0);
                            },
                            "Stop"
                        }
                        strong { role: "status", aria_live: "polite",
                            if rest_remaining() == 0 { "Ready" }
                            else { "{rest_remaining()} seconds" }
                        }
                    }
                }
            }
            Card {
                title: "Confirm completion".to_owned(),
                description: "Saved sets alone do not establish completion. Explicitly confirm this workout was performed.".to_owned(),
                form {
                    class: "card-body health-companion-section",
                    onsubmit: move |event| {
                        event.prevent_default();
                        if !confirm_completion() || busy() {
                            error.set("Confirm that the scheduled workout was completed.".to_owned());
                            return;
                        }
                        let Ok(minutes) = completion_minutes().parse::<f64>() else {
                            error.set("Enter a session duration.".to_owned()); return;
                        };
                        if !minutes.is_finite() || !(0.0..=1440.0).contains(&minutes) {
                            error.set("Duration must be between zero and 1440 minutes.".to_owned());
                            return;
                        }
                        let token = csrf();
                        let args = json!({
                            "plan_id": selected_plan(), "session_id": selected_session(),
                            "duration_minutes": minutes, "notes": completion_notes(),
                            "at": current_utc(), "confirm": true,
                        });
                        busy.set(true);
                        error.set(String::new());
                        spawn(async move {
                            match health_tools::call::<Value>(
                                &token, Tool::WorkoutLog, args,
                            ).await {
                                Ok(_) => {
                                    status.set("Completed session recorded.".to_owned());
                                    confirm_completion.set(false);
                                }
                                Err(message) => error.set(message),
                            }
                            busy.set(false);
                        });
                    },
                    label { class: "form-row",
                        span { class: "label", "Elapsed session minutes" }
                        input { class: "field", r#type: "number", min: "0", max: "1440", step: "any",
                            value: "{completion_minutes()}", disabled: busy(),
                            oninput: move |event| completion_minutes.set(event.value()) }
                    }
                    label { class: "form-row",
                        span { class: "label", "Notes (optional)" }
                        textarea { class: "field", value: "{completion_notes()}",
                            disabled: busy(), oninput: move |event| completion_notes.set(event.value()) }
                    }
                    label { class: "health-completion-confirm",
                        input { r#type: "checkbox", checked: confirm_completion(), disabled: busy(),
                            onchange: move |event| confirm_completion.set(event.checked()) }
                        span { "I confirm this workout was completed." }
                    }
                    button { class: "btn btn-primary",
                        disabled: busy() || !confirm_completion(),
                        "Record completed workout"
                    }
                }
            }
        } else if plan.is_none() {
            Card {
                title: "No selected plan".to_owned(),
                description: "Choose a workout plan to inspect its exercises and log actual sets.".to_owned(),
                Empty {
                    title: "Select a workout".to_owned(),
                    description: "A plan and session are required before logging.".to_owned()
                }
            }
        }
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn training_measurements_are_bounded() {
        assert_eq!(optional_measurement("", 10.0).unwrap(), None);
        assert_eq!(optional_measurement("7.5", 10.0).unwrap(), Some(7.5));
        assert!(optional_measurement("-1", 10.0).is_err());
        assert!(optional_measurement("10.5", 10.0).is_err());
        assert!(optional_measurement("NaN", 10.0).is_err());
        assert!(optional_measurement("infinity", 10.0).is_err());
    }
}
