use dioxus::prelude::*;
use serde::Deserialize;

use crate::api;
use crate::components::{Empty, Heading, Icon, Loading, Notice};

#[derive(Clone, Debug, Deserialize, PartialEq)]
pub struct Occurrence {
    pub uid: String,
    pub summary: String,
    pub start: String,
    pub end: String,
    #[serde(default)]
    pub location: String,
    #[serde(default)]
    pub all_day: bool,
}

#[derive(Clone, Debug, Deserialize, PartialEq)]
pub struct CalendarWindow {
    pub start: String,
    pub end: String,
    #[serde(default)]
    pub occurrences: Vec<Occurrence>,
    #[serde(default)]
    pub limitations: Vec<String>,
}

fn date_label(instant: &str) -> String {
    instant.get(..10).unwrap_or(instant).to_owned()
}

fn clock_label(instant: &str) -> String {
    instant.get(11..16).unwrap_or("").to_owned()
}

#[component]
pub fn Calendar() -> Element {
    let mut window =
        use_resource(|| async { api::get::<CalendarWindow>("/v1/calendar/occurrences").await });
    let mut search = use_signal(String::new);
    let needle = search().to_lowercase();
    rsx! {
        Heading {
            title: "Calendar".to_owned(),
            description: "Upcoming appointments and recurring events from your read-only calendar feed.".to_owned(),
            button {
                class: "btn btn-outline",
                onclick: move |_| window.restart(),
                Icon { name: "refresh".to_owned() }
                "Refresh"
            }
        }
        section { class: "card",
            div { class: "card-header",
                h2 { class: "card-title", "Upcoming events" }
                p { class: "card-description", "A rolling 30-day window. No calendar event is modified." }
            }
            div { class: "card-body",
                label { class: "form-row calendar-filter",
                    "Find an event"
                    input {
                        class: "field",
                        placeholder: "Search title or location",
                        value: "{search}",
                        oninput: move |event| search.set(event.value()),
                    }
                }
                match window.cloned() {
                    None => rsx! { Loading {} },
                    Some(Err(message)) => rsx! { Notice { message } },
                    Some(Ok(result)) => rsx! {
                        if !result.limitations.is_empty() {
                            section { class: "calendar-limitations",
                                strong { "Calendar limitations" }
                                for limitation in &result.limitations {
                                    p { class: "small muted", "{limitation}" }
                                }
                            }
                        }
                        div { class: "calendar-agenda",
                            for event in result.occurrences.iter().cloned().filter(|entry| {
                                needle.is_empty() ||
                                entry.summary.to_lowercase().contains(&needle) ||
                                entry.location.to_lowercase().contains(&needle)
                            }) {
                                article { class: "calendar-event",
                                    div { class: "calendar-date",
                                        strong { {date_label(&event.start)} }
                                        if event.all_day {
                                            span { class: "small muted", "All day" }
                                        } else {
                                            span { class: "small muted", "{clock_label(&event.start)}–{clock_label(&event.end)}" }
                                        }
                                    }
                                    div { class: "calendar-details",
                                        strong { if event.summary.is_empty() { "Untitled event" } else { "{event.summary}" } }
                                        if !event.location.is_empty() {
                                            p { class: "small muted", "{event.location}" }
                                        }
                                    }
                                }
                            }
                        }
                        if result.occurrences.is_empty() {
                            Empty {
                                title: "No occurrences returned".to_owned(),
                                description: "The calendar contains no dated events in this window.".to_owned()
                            }
                        }
                    }
                }
            }
        }
    }
}
