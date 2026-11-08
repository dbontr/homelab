use dioxus::prelude::*;
use reqwest::Method;
use serde_json::{json, Value};

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
    let mut conflict_preview = use_signal(||None::<Value>);

    let items = records.cloned().and_then(Result::ok).unwrap_or_default();
    let today = agenda.cloned().and_then(Result::ok).unwrap_or_default();
    let visible: Vec<Task> = match filter().as_str() {
        "today" => today,
        "completed" => items.into_iter().filter(|task| task.status == "completed").collect(),
        _ => items.into_iter().filter(|task| task.status == "open").collect(),
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
                                if task.status == "open" {
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
