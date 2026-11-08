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

fn short_time(time: &str) -> String {
    time.get(11..16).unwrap_or(time).to_owned()
}

#[component]
pub fn Planner() -> Element {
    let mut daily_minutes = use_signal(|| 180_i32);
    let mut start_hour = use_signal(|| 9_i32);
    let mut plan = use_resource(move || {
        let minutes = daily_minutes();
        let hour = start_hour();
        async move {
            api::get::<WeeklyPlan>(&format!(
                "/v1/planning/week?daily_minutes={minutes}&start_hour={hour}"
            )).await
        }
    });
    rsx! {
        Heading {
            title: "Weekly planner".to_owned(),
            description: "Capacity-aware timeboxes drawn from recorded tasks, prerequisites and calendar commitments.".to_owned(),
            button { class: "btn btn-outline", onclick: move |_| plan.restart(),
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
                    "This is a proposal, not a calendar change. Tasks without durations or with unfinished prerequisites are not silently scheduled."
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
