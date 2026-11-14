use dioxus::prelude::*;
use reqwest::Method;
use serde::Deserialize;
use serde_json::{Value, json};

use crate::api::{self, Task};
use crate::components::{Badge, Empty, Heading, Loading, Notice};

#[derive(Clone, Default, PartialEq)]
struct Editor {
    title: String,
    notes: String,
    due: String,
    scheduled: String,
    recurrence: String,
    energy: String,
    location: String,
    minutes: i32,
    priority: i32,
    revision: i32,
}

impl From<&Task> for Editor {
    fn from(task: &Task) -> Self {
        Self {
            title: task.title.clone(),
            notes: task.notes.clone(),
            due: task.due.clone(),
            scheduled: task.scheduled.clone(),
            recurrence: task.recurrence.clone(),
            energy: task.energy.clone(),
            location: task.location.clone(),
            minutes: task.minutes,
            priority: task.priority,
            revision: task.revision,
        }
    }
}

#[derive(Clone, Debug, Deserialize, serde::Serialize, PartialEq)]
struct DecompositionStep {
    title: String,
    minutes: i32,
    #[serde(default)]
    depends_on_steps: Vec<usize>,
}

#[derive(Clone, Debug, Deserialize, PartialEq)]
struct DecompositionPreview {
    parent_id: String,
    expected_revision: i32,
    steps: Vec<DecompositionStep>,
    total_minutes: i32,
    requires_owner_submission: bool,
}

#[derive(Clone, Debug, Deserialize)]
struct DecompositionResult {
    parent: Task,
    steps: Vec<Task>,
}

fn predecessor_labels(steps: &[usize]) -> String {
    steps
        .iter()
        .map(|index| (index + 1).to_string())
        .collect::<Vec<_>>()
        .join(", ")
}

#[component]
pub fn Tasks(csrf: String) -> Element {
    let csrf = use_signal(|| csrf);
    let mut records = use_resource(|| async { api::get::<Vec<Task>>("/v1/tasks").await });
    let mut agenda = use_resource(|| async { api::get::<Vec<Task>>("/v1/tasks/agenda").await });
    let mut filter = use_signal(|| "today".to_owned());
    let mut capture = use_signal(String::new);
    let mut editing = use_signal(|| Option::<String>::None);
    let mut draft = use_signal(Editor::default);
    let mut busy = use_signal(|| false);
    let mut error = use_signal(String::new);
    let mut message = use_signal(String::new);
    let mut conflict_preview = use_signal(|| None::<Value>);
    let mut decomposition_parent = use_signal(|| None::<Task>);
    let mut decomposition_preview = use_signal(|| None::<DecompositionPreview>);
    let mut disclosure_consent = use_signal(|| false);

    let items = records.cloned().and_then(Result::ok).unwrap_or_default();
    let today = agenda.cloned().and_then(Result::ok).unwrap_or_default();
    let visible: Vec<Task> = match filter().as_str() {
        "today" => today,
        "completed" => items
            .into_iter()
            .filter(|task| task.status == "completed")
            .collect(),
        _ => items
            .into_iter()
            .filter(|task| task.status == "open")
            .collect(),
    };
    let editor = draft();

    rsx! {
        Heading {
            title: "Tasks".to_owned(),
            description: "Capture work, plan its time, and keep recurring commitments.".to_owned(),
            button { class: "btn btn-outline", onclick: move |_| { records.restart(); agenda.restart(); },
                "Refresh"
            }
        }
        section { class: "card",
            div { class: "card-header",
                h2 { class: "card-title", "Quick capture" }
                p { class: "card-description",
                    "Use “tomorrow”, “today”, “every weekday”, or “every monday” at the end of a task."
                }
            }
            form { class: "card-body task-capture",
                onsubmit: move |event| {
                    event.prevent_default();
                    if busy() || capture().trim().is_empty() { return; }
                    let input = capture();
                    let token = csrf();
                    busy.set(true); error.set(String::new());
                    spawn(async move {
                        match api::write::<Task, _>(Method::POST, "/v1/tasks/capture", &token, &json!({"text": input})).await {
                            Ok(_) => {
                                capture.set(String::new());
                                message.set("Task added".to_owned());
                                records.restart(); agenda.restart();
                            }
                            Err(value) => error.set(value),
                        }
                        busy.set(false);
                    });
                },
                input {
                    class: "field", aria_label: "Task description", maxlength: "500",
                    placeholder: "Review Fourier series tomorrow",
                    value: "{capture()}",
                    oninput: move |event| capture.set(event.value())
                }
                button { class: "btn btn-primary", disabled: busy(), "Add task" }
            }
        }
        if !error().is_empty() { Notice { message: error() } }
        if !message().is_empty() { div { class: "alert", role: "status", "{message}" } }
        div { class: "tabs", role: "tablist", aria_label: "Task views",
            button { class: "tab", role: "tab", aria_selected: if filter()=="today" {"true"} else {"false"},
                onclick: move |_|filter.set("today".to_owned()), "Today"
            }
            button { class: "tab", role: "tab", aria_selected: if filter()=="open" {"true"} else {"false"},
                onclick: move |_|filter.set("open".to_owned()), "Inbox"
            }
            button { class: "tab", role: "tab", aria_selected: if filter()=="completed" {"true"} else {"false"},
                onclick: move |_|filter.set("completed".to_owned()), "Completed"
            }
        }
        section { class: "card",
            div { class: "card-header",
                h2 { class: "card-title", "Tasks" }
                p { class: "card-description", "Scheduled times and recurrence are explicit; no calendar changes are made." }
            }
            match records.cloned() {
                None => rsx! { Loading {} },
                Some(Err(value)) => rsx! { Notice { message: value } },
                Some(Ok(_)) if visible.is_empty() => rsx! { Empty {
                    title: "Nothing here".to_owned(),
                    description: "Your open tasks, deadlines and planned work appear here.".to_owned()
                } },
                Some(Ok(_)) => rsx! {
                    div { class: "task-items",
                        for task in visible {
                            div { class: "task-item", key: "{task.id}",
                                if task.status == "open" && !task.decomposed {
                                    button {
                                        class: "task-check", aria_label: format!("Complete {}", task.title),
                                        title: "Complete task", disabled: busy(),
                                        onclick: {
                                            let id = task.id.clone();
                                            move |_| {
                                                let id = id.clone();
                                                let token = csrf();
                                                busy.set(true);error.set(String::new());
                                                spawn(async move {
                                                    match api::write::<Task, _>(Method::POST,
                                                        &format!("/v1/tasks/{id}/complete"), &token, &json!({})).await {
                                                        Ok(_) => { records.restart();agenda.restart(); },
                                                        Err(e) => error.set(e),
                                                    }
                                                    busy.set(false);
                                                });
                                            }
                                        },
                                        "○"
                                    }
                                } else if task.status == "open" {
                                    span { class: "badge", "Parent" }
                                } else {
                                    span { class: "task-done", aria_label: "Completed", "✓" }
                                }
                                div { class: "task-information",
                                    span { class: "task-title", "{task.title}" }
                                    div { class: "task-meta",
                                        if !task.due.is_empty() { span { "Due {task.due}" } }
                                        if !task.scheduled.is_empty() { span { "Planned {task.scheduled}" } }
                                        if task.minutes > 0 { span { "{task.minutes} min" } }
                                        if !task.recurrence.is_empty() { Badge { text: task.recurrence.clone() } }
                                        if !task.parent_id.is_empty() { Badge { text: "Subtask".to_owned() } }
                                    }
                                }
                                if task.status == "open" && !task.decomposed {
                                    button {
                                        class: "btn btn-ghost btn-sm",
                                        onclick: {
                                            let selected = task.clone();
                                            move |_| {
                                                decomposition_parent.set(Some(selected.clone()));
                                                decomposition_preview.set(None);
                                                disclosure_consent.set(false);
                                                editing.set(None);
                                                error.set(String::new());
                                            }
                                        },
                                        "Break down"
                                    }
                                }
                                if task.status == "open" {
                                    button {
                                        class: "btn btn-ghost btn-sm",
                                        onclick: {
                                            let selected = task.clone();
                                            move |_| {
                                                draft.set(Editor::from(&selected));
                                                editing.set(Some(selected.id.clone()));
                                                error.set(String::new());
                                            }
                                        },
                                        "Edit"
                                    }
                                }
                            }
                        }
                    }
                }
            }
        }
        if let Some(parent) = decomposition_parent() {
            section { class: "card task-editor",
                div { class: "card-header",
                    h2 { class: "card-title", "Break down task" }
                    p { class: "card-description",
                        "Review a proposed sequence for {parent.title}. No tasks are saved until you approve the breakdown."
                    }
                }
                div { class: "card-body task-editor-fields",
                    label { class: "inline-row",
                        input {
                            r#type: "checkbox",
                            checked: disclosure_consent(),
                            disabled: busy(),
                            onchange: move |event| {
                                disclosure_consent.set(event.checked());
                                decomposition_preview.set(None);
                            }
                        }
                        span { "Allow the configured planning model to read only this task's title and estimated minutes." }
                    }
                    p { class: "small muted",
                        "Private notes, related projects, your task history and credentials are not sent to the model. The preview can be discarded without modifying your records."
                    }
                    div { class: "task-actions",
                        button {
                            class: "btn btn-outline",
                            r#type: "button",
                            disabled: busy() || !disclosure_consent(),
                            onclick: {
                                let task_id = parent.id.clone();
                                let revision = parent.revision;
                                move |_| {
                                    let id = task_id.clone();
                                    let token = csrf();
                                    busy.set(true);
                                    error.set(String::new());
                                    decomposition_preview.set(None);
                                    spawn(async move {
                                        let payload = json!({
                                            "expected_revision": revision,
                                            "share_title_with_model": true
                                        });
                                        match api::write::<DecompositionPreview, _>(
                                            Method::POST,
                                            &format!("/v1/tasks/{id}/decomposition-preview"),
                                            &token, &payload,
                                        ).await {
                                            Ok(draft) => {
                                                if draft.requires_owner_submission {
                                                    decomposition_preview.set(Some(draft));
                                                } else {
                                                    error.set("The model response did not require owner approval.".to_owned());
                                                }
                                            }
                                            Err(problem) => error.set(problem),
                                        }
                                        busy.set(false);
                                    });
                                }
                            },
                            "Generate preview"
                        }
                        button {
                            class: "btn btn-outline",
                            r#type: "button",
                            disabled: busy(),
                            onclick: move |_| {
                                decomposition_parent.set(None);
                                decomposition_preview.set(None);
                                disclosure_consent.set(false);
                            },
                            "Close"
                        }
                    }
                    if let Some(preview) = decomposition_preview() {
                        if preview.parent_id == parent.id &&
                            preview.expected_revision == parent.revision {
                            div { class: "planner-unplanned",
                                p { class: "small muted",
                                    "{preview.steps.len()} proposed subtasks · {preview.total_minutes} estimated minutes"
                                }
                                for (index, step) in preview.steps.iter().enumerate() {
                                    article { class: "planner-review-entry",
                                        div { class: "inline-row",
                                            span { class: "badge", "{index + 1}" }
                                            strong { "{step.title}" }
                                            span { class: "small muted", "{step.minutes} min" }
                                        }
                                        if !step.depends_on_steps.is_empty() {
                                            p { class: "small muted",
                                                "After steps {predecessor_labels(&step.depends_on_steps)}"
                                            }
                                        }
                                    }
                                }
                            }
                            p { class: "small muted",
                                "Confirm only if the steps, estimates and dependencies are suitable. Saving creates all subtasks atomically; an outdated parent revision is rejected."
                            }
                            button {
                                class: "btn btn-primary",
                                r#type: "button",
                                disabled: busy(),
                                onclick: {
                                    let id = preview.parent_id.clone();
                                    let revision = preview.expected_revision;
                                    let steps = preview.steps.clone();
                                    move |_| {
                                        let id = id.clone();
                                        let token = csrf();
                                        let steps = steps.clone();
                                        busy.set(true);
                                        error.set(String::new());
                                        spawn(async move {
                                            let payload = json!({
                                                "expected_revision": revision,
                                                "steps": steps
                                            });
                                            match api::write::<DecompositionResult, _>(
                                                Method::POST,
                                                &format!("/v1/tasks/{id}/decompose"),
                                                &token, &payload,
                                            ).await {
                                                Ok(committed) => {
                                                    message.set(format!(
                                                        "Created {} subtasks", committed.steps.len()
                                                    ));
                                                    decomposition_parent.set(None);
                                                    decomposition_preview.set(None);
                                                    disclosure_consent.set(false);
                                                    records.restart();
                                                    agenda.restart();
                                                }
                                                Err(problem) => error.set(problem),
                                            }
                                            busy.set(false);
                                        });
                                    }
                                },
                                "Save subtasks"
                            }
                        }
                    }
                }
            }
        }
        if let Some(task_id) = editing() {
            section { class: "card task-editor",
                div { class: "card-header",
                    h2 { class: "card-title", "Task details" }
                    p { class: "card-description", "Use an RFC3339 timestamp with a time-zone offset for calendar placement." }
                }
                form { class: "card-body task-editor-fields",
                    onsubmit: move |event| {
                        event.prevent_default();
                        if busy() { return; }
                        let data = draft();
                        let token = csrf();
                        let id = task_id.clone();
                        busy.set(true);error.set(String::new());
                        spawn(async move {
                            let payload = json!({
                                "title": data.title, "notes": data.notes, "due": data.due,
                                "scheduled": data.scheduled, "recurrence": data.recurrence,
                                "energy": data.energy, "location": data.location,
                                "minutes": data.minutes, "priority": data.priority,
                                "expected_revision": data.revision
                            });
                            match api::write::<Task, _>(Method::PUT, &format!("/v1/tasks/{id}"), &token, &payload).await {
                                Ok(_) => {
                                    editing.set(None);
                                    message.set("Task updated".to_owned());
                                    records.restart();agenda.restart();
                                }
                                Err(value) => error.set(value),
                            }
                            busy.set(false);
                        });
                    },
                    label { class: "form-row",
                        span { class: "label", "Title" }
                        input { class: "field", required: true, maxlength: "500",
                            value: "{editor.title}", oninput: move |e|{
                                let mut d=draft();d.title=e.value();draft.set(d);
                            }
                        }
                    }
                    label { class: "form-row",
                        span { class: "label", "Notes" }
                        textarea { class: "field", rows: "3",
                            value: "{editor.notes}", oninput: move |e|{
                                let mut d=draft();d.notes=e.value();draft.set(d);
                            }
                        }
                    }
                    div { class: "task-field-grid",
                        label { class: "form-row",
                            span { class: "label", "Due date" }
                            input { class: "field", placeholder: "YYYY-MM-DD",
                                value: "{editor.due}", oninput: move |e|{
                                    let mut d=draft();d.due=e.value();draft.set(d);
                                }
                            }
                        }
                        label { class: "form-row",
                            span { class: "label", "Scheduled time" }
                            input { class: "field", placeholder: "2026-11-07T09:00:00-06:00",
                                value: "{editor.scheduled}", oninput: move |e|{
                                    let mut d=draft();d.scheduled=e.value();draft.set(d);
                                }
                            }
                        }
                        label { class: "form-row",
                            span { class: "label", "Recurrence" }
                            select { class: "field", value: "{editor.recurrence}",
                                onchange: move |e|{let mut d=draft();d.recurrence=e.value();draft.set(d);},
                                option { value: "", "Does not repeat" }
                                option { value: "daily", "Daily" }
                                option { value: "weekdays", "Weekdays" }
                                option { value: "weekly", "Weekly" }
                                option { value: "monthly", "Monthly" }
                                option { value: "weekly:mon", "Every Monday" }
                                option { value: "weekly:tue", "Every Tuesday" }
                                option { value: "weekly:wed", "Every Wednesday" }
                                option { value: "weekly:thu", "Every Thursday" }
                                option { value: "weekly:fri", "Every Friday" }
                            }
                        }
                        label { class: "form-row",
                            span { class: "label", "Duration (minutes)" }
                            input { class: "field", r#type: "number", min: "0", max: "1440",
                                value: "{editor.minutes}", oninput: move |e|{
                                    if let Ok(n)=e.value().parse::<i32>(){let mut d=draft();d.minutes=n;draft.set(d);}
                                }
                            }
                        }
                        label { class: "form-row",
                            span { class: "label", "Priority (0–5)" }
                            input { class: "field", r#type: "number", min: "0", max: "5",
                                value: "{editor.priority}", oninput: move |e|{
                                    if let Ok(n)=e.value().parse::<i32>(){let mut d=draft();d.priority=n;draft.set(d);}
                                }
                            }
                        }
                    }
                    if let Some(preview) = conflict_preview() {
                        div { class: "alert", role: "status",
                            if let Some(rows)=preview.get("conflicts").and_then(Value::as_array){
                                p { "{rows.len()} scheduling conflict(s) found." }
                                for conflict in rows {
                                    p { class: "small muted",
                                        {api::property(conflict,&["source"])} ": "
                                        {api::property(conflict,&["title"])}
                                    }
                                }
                            }
                            if let Some(notes)=preview.get("limitations").and_then(Value::as_array){
                                for note in notes { p { class:"small muted", "{note}" } }
                            }
                        }
                    }
                    div { class: "task-actions",
                        button { class: "btn btn-outline", r#type: "button",
                            disabled: busy() || editor.scheduled.is_empty() || editor.minutes<1,
                            onclick: move |_| {
                                let data=draft();
                                let id=editing().unwrap_or_default();
                                let timestamp=data.scheduled.replace("+","%2B");
                                let path=format!("/v1/tasks/conflicts?start={timestamp}&minutes={}&exclude_id={id}",data.minutes);
                                busy.set(true);error.set(String::new());
                                spawn(async move {
                                    match api::get::<Value>(&path).await{
                                        Ok(result)=>conflict_preview.set(Some(result)),
                                        Err(e)=>error.set(e),
                                    }
                                    busy.set(false);
                                });
                            },
                            "Check conflicts"
                        }
                        button { class: "btn btn-primary", disabled: busy(), "Save" }
                        button { class: "btn btn-outline", r#type: "button", onclick: move |_|editing.set(None), "Cancel" }
                    }
                }
            }
        }
    }
}

#[cfg(test)]
mod decomposition_tests {
    use super::{DecompositionPreview, DecompositionStep};

    #[test]
    fn preview_is_reviewable_without_implicit_persistence() {
        let input = serde_json::json!({
            "parent_id": "parent",
            "expected_revision": 3,
            "steps": [
                {"title": "Measure", "minutes": 20},
                {"title": "Evaluate", "minutes": 30, "depends_on_steps": [0]}
            ],
            "total_minutes": 50,
            "requires_owner_submission": true
        });
        let preview: DecompositionPreview = serde_json::from_value(input).unwrap();
        assert_eq!(preview.parent_id, "parent");
        assert_eq!(preview.expected_revision, 3);
        assert!(preview.requires_owner_submission);
        assert_eq!(preview.steps.len(), 2);
        assert_eq!(preview.steps[1].depends_on_steps, vec![0]);
        assert_eq!(preview.total_minutes, 50);

        let saved = serde_json::to_value(&preview.steps).unwrap();
        assert_eq!(saved[0]["title"], "Measure");
        assert_eq!(saved[1]["depends_on_steps"], serde_json::json!([0]));
    }

    #[test]
    fn proposed_steps_accept_only_declared_fields() {
        let value = serde_json::json!({
            "title": "Prove lemma", "minutes": 45, "depends_on_steps": [0]
        });
        let step: DecompositionStep = serde_json::from_value(value).unwrap();
        assert_eq!(step.title, "Prove lemma");
        assert_eq!(step.minutes, 45);
        assert_eq!(step.depends_on_steps, vec![0]);
    }
}
