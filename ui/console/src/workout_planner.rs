use std::collections::{BTreeMap, BTreeSet};

use dioxus::prelude::*;
use serde::{Deserialize, Serialize};
use serde_json::{Value, json};

use crate::components::{Card, Empty, Heading, Loading, Notice};
use crate::health_tools::{self, Tool};

#[derive(Clone, Default, PartialEq, Serialize, Deserialize)]
#[serde(default)]
pub(crate) struct Exercise {
    pub(crate) name: String,
    pub(crate) sets: i32,
    pub(crate) repetitions: i32,
    pub(crate) target: String,
    #[serde(flatten)]
    extra: BTreeMap<String, Value>,
}

#[derive(Clone, Default, PartialEq, Serialize, Deserialize)]
#[serde(default)]
pub(crate) struct Session {
    pub(crate) id: String,
    pub(crate) date: String,
    pub(crate) title: String,
    pub(crate) exercises: Vec<Exercise>,
    pub(crate) items: Vec<Value>,
    #[serde(flatten)]
    extra: BTreeMap<String, Value>,
}

#[derive(Clone, Default, PartialEq, Serialize, Deserialize)]
#[serde(default)]
pub(crate) struct Plan {
    pub(crate) id: String,
    pub(crate) kind: String,
    pub(crate) title: String,
    pub(crate) revision: i32,
    updated_at: String,
    pub(crate) sessions: Vec<Session>,
    #[serde(flatten)]
    extra: BTreeMap<String, Value>,
}

fn validate(plan: &Plan) -> Result<(), String> {
    if plan.kind != "workout" || plan.title.trim().is_empty() || plan.title.len() > 200 {
        return Err("Enter a workout plan title of at most 200 characters.".to_owned());
    }
    if plan.sessions.len() > 2000 {
        return Err("The plan contains too many sessions.".to_owned());
    }
    let mut ids = BTreeSet::new();
    for session in &plan.sessions {
        if session.id.is_empty() || !ids.insert(&session.id) {
            return Err("Session IDs must be unique and nonempty.".to_owned());
        }
        if session.title.trim().is_empty() ||
            session.date.len() != 10 || !session.date.as_bytes().get(4).is_some_and(|b| *b == b'-') ||
            !session.date.as_bytes().get(7).is_some_and(|b| *b == b'-') {
            return Err("Each session requires a title and a calendar date.".to_owned());
        }
        if session.exercises.iter().any(|exercise|
            exercise.name.trim().is_empty() || exercise.sets < 0 || exercise.repetitions < 0)
        {
            return Err("Every exercise needs a name and nonnegative sets and repetitions.".to_owned());
        }
    }
    Ok(())
}

fn add_session(plan: &mut Plan) {
    let moment = js_sys::Date::now() as u64;
    let mut count = plan.sessions.len();
    let id = loop {
        let id = format!("session-{moment}-{count}");
        if !plan.sessions.iter().any(|session| session.id == id) {
            break id;
        }
        count += 1;
    };
    plan.sessions.push(Session {
        id, date: String::new(), title: String::new(), ..Session::default()
    });
}

#[component]
pub fn WorkoutPlanner(csrf: String) -> Element {
    let csrf = use_signal(|| csrf);
    let mut plans = use_resource(move || {
        let token = csrf();
        async move {
            health_tools::call::<Vec<Plan>>(&token, Tool::PlanList, json!({})).await
        }
    });
    let mut active = use_signal(|| None::<Plan>);
    let mut busy = use_signal(|| false);
    let mut error = use_signal(String::new);
    let mut status = use_signal(String::new);

    rsx! {
        Heading {
            title: "Workout plans".to_owned(),
            description: "Edit scheduled sessions and exercises with revision-fenced saves.".to_owned(),
            button {
                class: "btn btn-outline",
                onclick: move |_| plans.restart(),
                "Refresh plans"
            }
        }
        if !error().is_empty() { Notice { message: error() } }
        Card {
            title: "Select a plan".to_owned(),
            description: "Select an existing workout plan or create a new one. Changes are local until saved.".to_owned(),
            div { class: "card-body health-plan-picker",
                button {
                    class: "btn btn-primary",
                    r#type: "button", disabled: busy(),
                    onclick: move |_| {
                        active.set(Some(Plan {
                            kind: "workout".to_owned(), ..Plan::default()
                        }));
                        error.set(String::new());
                        status.set(String::new());
                    },
                    "Create workout plan"
                }
                match plans.cloned() {
                    None => rsx! { Loading {} },
                    Some(Err(message)) => rsx! { Notice { message } },
                    Some(Ok(records)) => {
                        let workouts = records.into_iter()
                            .filter(|plan| plan.kind == "workout")
                            .collect::<Vec<_>>();
                        rsx! {
                            if workouts.is_empty() {
                                Empty {
                                    title: "No workout plans".to_owned(),
                                    description: "Create your first plan above.".to_owned()
                                }
                            } else {
                                div { class: "health-plan-list",
                                    for plan in workouts {
                                        button {
                                            key: "{plan.id}",
                                            class: if active().as_ref().is_some_and(|p| p.id == plan.id) {
                                                "btn btn-primary"
                                            } else {
                                                "btn btn-outline"
                                            },
                                            r#type: "button",
                                            disabled: busy(),
                                            onclick: {
                                                let selected = plan.clone();
                                                move |_| {
                                                    active.set(Some(selected.clone()));
                                                    error.set(String::new());
                                                    status.set(String::new());
                                                }
                                            },
                                            "{plan.title} · revision {plan.revision}"
                                        }
                                    }
                                }
                            }
                        }
                    }
                }
            }
        }
        if let Some(plan) = active() {
            Card {
                title: "Plan editor".to_owned(),
                description: "The Health service checks the expected revision before it stores edits.".to_owned(),
                div { class: "card-body health-plan-editor",
                    label { class: "form-row",
                        span { class: "label", "Plan title" }
                        input {
                            class: "field", required: true, maxlength: "200",
                            disabled: busy(), value: "{plan.title}",
                            oninput: move |event| {
                                if let Some(ref mut plan) = *active.write() {
                                    plan.title = event.value();
                                }
                            }
                        }
                    }
                    p { class: "small muted",
                        if plan.revision == 0 {
                            "Unsaved plan"
                        } else {
                            "Current revision: {plan.revision}"
                        }
                    }
                    for (index, session) in plan.sessions.iter().enumerate() {
                        section { class: "health-session-editor", key: "{session.id}",
                            div { class: "health-session-heading",
                                h3 { {format!("Session {}", index + 1)} }
                                button {
                                    class: "btn btn-outline btn-sm", r#type: "button",
                                    disabled: busy(),
                                    onclick: move |_| {
                                        if let Some(ref mut plan) = *active.write() {
                                            if index < plan.sessions.len() {
                                                plan.sessions.remove(index);
                                            }
                                        }
                                    },
                                    "Remove session"
                                }
                            }
                            div { class: "task-field-grid",
                                label { class: "form-row",
                                    span { class: "label", "Session date" }
                                    input {
                                        class: "field", r#type: "date", required: true,
                                        disabled: busy(), value: "{session.date}",
                                        oninput: move |event| {
                                            if let Some(ref mut plan) = *active.write() {
                                                if let Some(session) = plan.sessions.get_mut(index) {
                                                    session.date = event.value();
                                                }
                                            }
                                        }
                                    }
                                }
                                label { class: "form-row",
                                    span { class: "label", "Session title" }
                                    input {
                                        class: "field", required: true,
                                        disabled: busy(), value: "{session.title}",
                                        oninput: move |event| {
                                            if let Some(ref mut plan) = *active.write() {
                                                if let Some(session) = plan.sessions.get_mut(index) {
                                                    session.title = event.value();
                                                }
                                            }
                                        }
                                    }
                                }
                            }
                            for (exercise_index, exercise) in session.exercises.iter().enumerate() {
                                div { class: "health-exercise-editor",
                                    key: "{session.id}-{exercise_index}",
                                    label { class: "form-row",
                                        span { class: "label", "Exercise" }
                                        input {
                                            class: "field", required: true, disabled: busy(),
                                            value: "{exercise.name}",
                                            oninput: move |event| {
                                                if let Some(ref mut plan) = *active.write() {
                                                    if let Some(exercise) = plan.sessions.get_mut(index)
                                                        .and_then(|session| session.exercises.get_mut(exercise_index)) {
                                                        exercise.name = event.value();
                                                    }
                                                }
                                            }
                                        }
                                    }
                                    label { class: "form-row",
                                        span { class: "label", "Sets" }
                                        input {
                                            class: "field", r#type: "number", min: "0", max: "1000",
                                            disabled: busy(), value: "{exercise.sets}",
                                            oninput: move |event| {
                                                if let Ok(value) = event.value().parse::<i32>() {
                                                    if let Some(ref mut plan) = *active.write() {
                                                        if let Some(exercise) = plan.sessions.get_mut(index)
                                                            .and_then(|session| session.exercises.get_mut(exercise_index)) {
                                                            exercise.sets = value;
                                                        }
                                                    }
                                                }
                                            }
                                        }
                                    }
                                    label { class: "form-row",
                                        span { class: "label", "Repetitions" }
                                        input {
                                            class: "field", r#type: "number", min: "0", max: "10000",
                                            disabled: busy(), value: "{exercise.repetitions}",
                                            oninput: move |event| {
                                                if let Ok(value) = event.value().parse::<i32>() {
                                                    if let Some(ref mut plan) = *active.write() {
                                                        if let Some(exercise) = plan.sessions.get_mut(index)
                                                            .and_then(|session| session.exercises.get_mut(exercise_index)) {
                                                            exercise.repetitions = value;
                                                        }
                                                    }
                                                }
                                            }
                                        }
                                    }
                                    label { class: "form-row",
                                        span { class: "label", "Load or pace target" }
                                        input {
                                            class: "field", disabled: busy(),
                                            value: "{exercise.target}",
                                            oninput: move |event| {
                                                if let Some(ref mut plan) = *active.write() {
                                                    if let Some(exercise) = plan.sessions.get_mut(index)
                                                        .and_then(|session| session.exercises.get_mut(exercise_index)) {
                                                        exercise.target = event.value();
                                                    }
                                                }
                                            }
                                        }
                                    }
                                    button {
                                        class: "btn btn-outline btn-sm", r#type: "button",
                                        disabled: busy(),
                                        onclick: move |_| {
                                            if let Some(ref mut plan) = *active.write() {
                                                if let Some(session) = plan.sessions.get_mut(index) {
                                                    if exercise_index < session.exercises.len() {
                                                        session.exercises.remove(exercise_index);
                                                    }
                                                }
                                            }
                                        },
                                        "Remove exercise"
                                    }
                                }
                            }
                            button {
                                class: "btn btn-outline", r#type: "button", disabled: busy(),
                                onclick: move |_| {
                                    if let Some(ref mut plan) = *active.write() {
                                        if let Some(session) = plan.sessions.get_mut(index) {
                                            session.exercises.push(Exercise::default());
                                        }
                                    }
                                },
                                "Add exercise"
                            }
                        }
                    }
                    div { class: "health-plan-actions",
                        button {
                            class: "btn btn-outline", r#type: "button", disabled: busy(),
                            onclick: move |_| {
                                if let Some(ref mut plan) = *active.write() {
                                    add_session(plan);
                                }
                            },
                            "Add session"
                        }
                        button {
                            class: "btn btn-primary", r#type: "button", disabled: busy(),
                            onclick: move |_| {
                                let Some(plan) = active() else { return };
                                if let Err(message) = validate(&plan) {
                                    error.set(message);
                                    return;
                                }
                                let token = csrf();
                                let expected = plan.revision;
                                let payload = json!({
                                    "id": plan.id,
                                    "kind": "workout",
                                    "title": plan.title,
                                    "sessions": plan.sessions,
                                    "expected_revision": expected,
                                });
                                busy.set(true);
                                error.set(String::new());
                                spawn(async move {
                                    match health_tools::call::<Plan>(
                                        &token, Tool::PlanSave, payload,
                                    ).await {
                                        Ok(saved) => {
                                            status.set(format!(
                                                "Plan saved at revision {}.", saved.revision
                                            ));
                                            active.set(Some(saved));
                                            plans.restart();
                                        }
                                        Err(message) => error.set(format!(
                                            "Could not save this revision: {message}"
                                        )),
                                    }
                                    busy.set(false);
                                });
                            },
                            if busy() { "Saving…" } else { "Save plan" }
                        }
                    }
                    p { class: "small muted", role: "status", aria_live: "polite",
                        "{status()}"
                    }
                    p { class: "small muted",
                        "Selecting another plan discards unsaved edits. No workout is marked completed or missed by this editor."
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
    fn validates_revision_safe_workout_structure() {
        let mut plan = Plan { kind: "workout".to_owned(), title: "Training".to_owned(),
            ..Plan::default() };
        assert!(validate(&plan).is_ok());
        plan.sessions.push(Session { id: "session-1".to_owned(), ..Session::default() });
        assert!(validate(&plan).is_err());
        plan.sessions[0].date = "2026-11-10".to_owned();
        plan.sessions[0].title = "Lower body".to_owned();
        assert!(validate(&plan).is_ok());
        plan.sessions[0].exercises.push(Exercise::default());
        assert!(validate(&plan).is_err());
        plan.sessions[0].exercises[0].name = "Squat".to_owned();
        assert!(validate(&plan).is_ok());
    }

    #[test]
    fn unknown_session_fields_survive_roundtrip() {
        let original = json!({
            "id":"session-1", "date":"2026-11-10", "title":"Lower body",
            "exercises":[{"name":"Squat","sets":3,"repetitions":5,"target":"100 kg"}],
            "items":[{"name":"Water","quantity":1.0}],
            "extension":"retained"
        });
        let session: Session = serde_json::from_value(original.clone()).unwrap();
        let encoded = serde_json::to_value(&session).unwrap();
        assert_eq!(encoded["extension"], original["extension"]);
        assert_eq!(encoded["items"], original["items"]);
    }
}
