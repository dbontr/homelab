use dioxus::prelude::*;
use serde::Deserialize;

use crate::api;
use crate::components::{Empty, Heading, Icon, Loading, Notice};

#[derive(Clone, Debug, Deserialize, PartialEq)]
pub struct PlanSlot {
    pub task_id: String,
    pub title: String,
    pub date: String,
    pub start: String,
    pub end: String,
    pub minutes: i32,
    pub priority: i32,
    #[serde(default)]
    pub due: String,
}
#[derive(Clone, Debug, Deserialize, PartialEq)]
pub struct DeferredTask {
    pub task_id: String,
    pub title: String,
    pub reason: String,
}
#[derive(Clone, Debug, Deserialize, PartialEq)]
pub struct WeeklyPlan {
    pub from: String,
    pub until: String,
    pub daily_minutes: i32,
    #[serde(default)]
    pub daily_capacities: [i32; 7],
    pub start_hour: i32,
    #[serde(default)]
    pub slots: Vec<PlanSlot>,
    #[serde(default)]
    pub unplanned: Vec<DeferredTask>,
    #[serde(default)]
    pub reservations: i32,
    #[serde(default)]
    pub limitations: Vec<String>,
}

#[derive(Clone, Debug, Deserialize, PartialEq)]
pub struct ReviewEntry {
    pub task_id: String,
    pub title: String,
    pub category: String,
    #[serde(default)]
    pub open_since: String,
    #[serde(default)]
    pub due: String,
    #[serde(default)]
    pub dependency_count: i32,
    pub priority: i32,
}
#[derive(Clone, Debug, Deserialize, PartialEq)]
pub struct TaskReview {
    pub at: String,
    pub horizon_days: i32,
    #[serde(default)]
    pub entries: Vec<ReviewEntry>,
    pub note: String,
}

#[derive(Clone, Debug, Deserialize, PartialEq)]
pub struct Delegation {
    pub task_id: String,
    pub task_title: String,
    pub task_status: String,
    pub run_id: String,
    pub run_status: String,
    pub state: String,
    #[serde(default)]
    pub note: String,
}
#[derive(Clone, Debug, Deserialize, PartialEq)]
pub struct DelegationReport {
    #[serde(default)]
    pub delegations: Vec<Delegation>,
    #[serde(default)]
    pub limitations: Vec<String>,
}

fn review_category_label(category: &str) -> &str {
    match category {
        "overdue" => "Overdue",
        "upcoming" => "Upcoming",
        "blocked" => "Blocked",
        "needs_estimate" => "Needs estimate",
        "review" => "Age review",
        other => other,
    }
}

fn short_time(time: &str) -> String {
    time.get(11..16).unwrap_or(time).to_owned()
}

#[component]
pub fn Planner() -> Element {
    let mut daily_minutes = use_signal(|| 180_i32);
    let mut start_hour = use_signal(|| 9_i32);
    let mut day_minutes = use_signal(String::new);
    let mut delegations = use_resource(|| async {
        api::get::<DelegationReport>("/v1/tasks/delegations").await
    });
    let mut review = use_resource(|| async {
        api::get::<TaskReview>("/v1/planning/review?days=7").await
    });
    let mut plan = use_resource(move || {
        let minutes = daily_minutes();
        let hour = start_hour();
        let per_day = day_minutes();
        async move {
            let custom = per_day.trim();
            let day_filter = if custom.is_empty() {
                String::new()
            } else {
                let values: Vec<&str> = custom.split(',').map(str::trim).collect();
                if values.len()!=7 || values.iter().any(|v| {
                    v.parse::<i32>().map_or(true, |n| n != 0 && (n < 30 || n > 480) || n < 0)
                }) {
                    return Err("Provide seven daily capacities: zero or 30–480 minutes each.".to_owned());
                }
                format!("&day_minutes={}", values.join(","))
            };
            api::get::<WeeklyPlan>(&format!(
                "/v1/planning/week?daily_minutes={minutes}&start_hour={hour}{day_filter}"
            )).await
        }
    });
    rsx! {
        Heading {
            title: "Weekly planner".to_owned(),
            description: "Capacity-aware timeboxes drawn from recorded tasks, prerequisites and calendar commitments.".to_owned(),
            button { class: "btn btn-outline", onclick: move |_| { plan.restart(); review.restart(); delegations.restart(); },
                Icon { name: "refresh".to_owned() } "Recalculate"
            }
        }
        section { class: "card",
            div { class: "card-body",
                div { class: "monitoring-controls",
                    label { class: "form-row",
                        "Available task time per day"
                        select { class: "field", value: "{daily_minutes}",
                            onchange: move |event| if let Ok(value) = event.value().parse::<i32>() {
                                daily_minutes.set(value)
                            },
                            option { value: "60", "1 hour" }
                            option { value: "120", "2 hours" }
                            option { value: "180", "3 hours" }
                            option { value: "240", "4 hours" }
                            option { value: "360", "6 hours" }
                            option { value: "480", "8 hours" }
                        }
                    }
                    label { class: "form-row",
                        "Daily availability (optional, seven days)"
                        input {
                            class: "field",
                            placeholder: "0,120,180,60,180,240,0",
                            value: "{day_minutes}",
                            oninput: move |event| day_minutes.set(event.value()),
                        }
                    }
                    label { class: "form-row",
                        "Earliest planning hour"
                        select { class: "field", value: "{start_hour}",
                            onchange: move |event| if let Ok(value) = event.value().parse::<i32>() {
                                start_hour.set(value)
                            },
                            option { value: "7", "7:00" }
                            option { value: "8", "8:00" }
                            option { value: "9", "9:00" }
                            option { value: "10", "10:00" }
                            option { value: "12", "12:00" }
                            option { value: "14", "14:00" }
                        }
                    }
                }
                p { class: "small muted",
                    "This is a proposal, not a calendar change. Use seven comma-separated capacities beginning with the first plan day; zero closes a day. Unfinished prerequisites may be proposed earlier in the week, but completion is not assumed."
                }
            }
        }
        section { class: "card",
            div { class: "card-header",
                h2 { class: "card-title", "Delegated engineering work" }
                p { class: "card-description",
                    "Live engineering status is linked to personal tasks through explicit resource references. Task completion remains an owner decision."
                }
            }
            div { class: "card-body",
                match delegations.cloned() {
                    None => rsx! { Loading {} },
                    Some(Err(message)) => rsx! { Notice { message } },
                    Some(Ok(report)) => rsx! {
                        if report.delegations.is_empty() {
                            p { class: "small muted", "No engineering runs are linked to recorded personal tasks." }
                        }
                        for entry in report.delegations {
                            article { class: "planner-review-entry",
                                div { class: "inline-row",
                                    strong { "{entry.task_title}" }
                                    span { class: "badge", "{entry.state}" }
                                    span { class: "small muted", "Run: {entry.run_status}" }
                                }
                                if !entry.note.is_empty() {
                                    p { class: "small muted", "{entry.note}" }
                                }
                            }
                        }
                        for limitation in report.limitations {
                            p { class: "small muted", "{limitation}" }
                        }
                    }
                }
            }
        }
        section { class: "card",
            div { class: "card-header",
                h2 { class: "card-title", "Deadline and task review" }
                p { class: "card-description",
                    "Review uses recorded dates, prerequisites and estimates. Undated, unscheduled tasks open for 14 days are highlighted for review, not presumed neglected."
                }
            }
            div { class: "card-body",
                match review.cloned() {
                    None => rsx! { Loading {} },
                    Some(Err(message)) => rsx! { Notice { message } },
                    Some(Ok(result)) => rsx! {
                        if result.entries.is_empty() {
                            p { class: "small muted", "No approaching deadlines, blocked tasks, missing estimates or aged undated tasks were found." }
                        } else {
                            div { class: "planner-unplanned",
                                for entry in result.entries {
                                    article { class: "planner-review-entry",
                                        div { class: "inline-row",
                                            span { class: "badge", "{review_category_label(&entry.category)}" }
                                            strong { "{entry.title}" }
                                        }
                                        if !entry.due.is_empty() {
                                            span { class: "small muted", "Due: {entry.due}" }
                                        }
                                        if !entry.open_since.is_empty() {
                                            span { class: "small muted", "Open since: {entry.open_since}" }
                                        }
                                        if entry.dependency_count>0 {
                                            span { class: "small muted",
                                                "{entry.dependency_count} incomplete prerequisites"
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
        match plan.cloned() {
            None => rsx! { Loading {} },
            Some(Err(message)) => rsx! { Notice { message } },
            Some(Ok(schedule)) => rsx! {
                if !schedule.limitations.is_empty() {
                    section { class: "calendar-limitations",
                        strong { "Planning limitations" }
                        for item in &schedule.limitations {
                            p { class: "small muted", "{item}" }
                        }
                    }
                }
                div { class: "metric-grid",
                    div { class: "card metric",
                        div { class: "metric-title", "Proposed task blocks" }
                        div { class: "metric-value", "{schedule.slots.len()}" }
                    }
                    div { class: "card metric",
                        div { class: "metric-title", "Pre-existing reservations" }
                        div { class: "metric-value", "{schedule.reservations}" }
                    }
                    div { class: "card metric",
                        div { class: "metric-title", "Unplanned tasks" }
                        div { class: "metric-value", "{schedule.unplanned.len()}" }
                    }
                }
                div { class: "section-grid planner-sections",
                    section { class: "card",
                        div { class: "card-header",
                            h2 { class: "card-title", "Suggested timeboxes" }
                            p { class: "card-description",
                                "{schedule.from} through {schedule.until}. Existing appointments and scheduled tasks are excluded."
                            }
                        }
                        div { class: "card-body",
                            if schedule.slots.is_empty() {
                                Empty {
                                    title: "No available tasks".to_owned(),
                                    description: "There are no eligible tasks with suitable durations and free time.".to_owned()
                                }
                            } else {
                                div { class: "planner-slots",
                                    for slot in &schedule.slots {
                                        article { class: "planner-slot",
                                            div { class: "planner-slot-time",
                                                strong { "{slot.date}" }
                                                span { class: "small muted",
                                                    "{short_time(&slot.start)}–{short_time(&slot.end)}"
                                                }
                                            }
                                            div { class: "planner-slot-text",
                                                strong { "{slot.title}" }
                                                span { class: "small muted",
                                                    "{slot.minutes} minutes · priority {slot.priority}"
                                                }
                                                if !slot.due.is_empty() {
                                                    span { class: "small muted", "Due {slot.due}" }
                                                }
                                            }
                                        }
                                    }
                                }
                            }
                        }
                    }
                    section { class: "card",
                        div { class: "card-header",
                            h2 { class: "card-title", "Awaiting allocation" }
                            p { class: "card-description", "Tasks excluded for a stated reason." }
                        }
                        div { class: "card-body",
                            if schedule.unplanned.is_empty() {
                                p { class: "small muted", "Every eligible task fits within the available capacity." }
                            } else {
                                div { class: "planner-unplanned",
                                    for item in &schedule.unplanned {
                                        article { class: "planner-deferred",
                                            strong { "{item.title}" }
                                            span { class: "small muted", "{item.reason}" }
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
