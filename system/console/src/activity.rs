use dioxus::prelude::*;
use serde::Deserialize;
use serde_json::Value;

use crate::api;
use crate::components::{Empty, Heading, Icon, Loading, Notice};

fn date_label(value: &str) -> String {
    value.get(..19).unwrap_or(value).replace('T', " ")
}

#[derive(Clone, Debug, Deserialize, PartialEq)]
pub struct ActivityEvent {
    pub id: String,
    pub at: String,
    pub action: String,
    pub reference: String,
    pub kind: String,
    pub summary: String,
}
#[derive(Clone, Debug, Deserialize, PartialEq)]
pub struct Feed {
    pub at: String,
    #[serde(default)]
    pub events: Vec<ActivityEvent>,
    pub note: String,
}
#[derive(Clone, Debug, Deserialize, PartialEq)]
pub struct ResourceLink {
    pub relation: String,
    pub reference: String,
}
#[derive(Clone, Debug, Deserialize, PartialEq)]
pub struct Resource {
    pub reference: String,
    pub kind: String,
    pub id: String,
    pub owner: String,
    #[serde(default)]
    pub links: Vec<ResourceLink>,
    pub data: Value,
}

#[component]
pub fn Activity() -> Element {
    let mut filter = use_signal(|| "all".to_owned());
    let mut selected = use_signal(|| Option::<(String,String)>::None);
    let mut feed = use_resource(|| async {
        api::get::<Feed>("/v1/activity?limit=200").await
    });
    let resource = use_resource(move || {
        let selected = selected();
        async move {
            match selected {
                None => Ok(None),
                Some((kind,id)) => {
                    let path = format!("/v1/resources/{kind}/{id}");
                    api::get::<Resource>(&path).await.map(Some)
                }
            }
        }
    });

    rsx! {
        Heading {
            title: "Activity".to_owned(),
            description: "Events derived from completed, created and updated records across the platform.".to_owned(),
            button { class: "btn btn-outline", onclick: move |_| { feed.restart(); },
                Icon { name: "refresh".to_owned() } "Refresh"
            }
        }
        div { class: "monitoring-controls",
            label { class: "form-row",
                "Resource type"
                select { class: "field", value: "{filter}",
                    onchange: move |event| filter.set(event.value()),
                    option { value: "all", "All recorded activity" }
                    option { value: "task", "Tasks" }
                    option { value: "goal", "Goals" }
                    option { value: "recipe", "Recipes" }
                    option { value: "meal", "Meals" }
                    option { value: "book", "Books" }
                }
            }
        }
        div { class: "activity-layout",
            section { class: "card",
                div { class: "card-header",
                    h2 { class: "card-title", "Recent activity" }
                    p { class: "card-description", "Recorded actions only. Missing activity does not mean inactivity." }
                }
                div { class: "card-body",
                    match feed.cloned() {
                        None => rsx! { Loading {} },
                        Some(Err(message)) => rsx! { Notice { message } },
                        Some(Ok(result)) => rsx! {
                            if result.events.is_empty() {
                                Empty { title: "No recorded events".to_owned(),
                                    description: "Completed tasks, book requests and other saved records appear here.".to_owned() }
                            } else {
                                div { class: "activity-items",
                                    for event in result.events.iter().filter(|record| filter()=="all" || record.kind==filter()).cloned() {
                                        button { class: "activity-item",
                                            title: "Inspect the record",
                                            onclick: {
                                                let id = event.reference.rsplit(':').next().unwrap_or("").to_owned();
                                                let kind = event.kind.clone();
                                                move |_| selected.set(Some((kind.clone(),id.clone())))
                                            },
                                            div { class: "activity-item-top",
                                                span { class: "badge muted", "{event.kind}" }
                                                span { class: "small muted", "{event.action}" }
                                                time { class: "small muted tabular",
                                                    {date_label(&event.at)}
                                                }
                                            }
                                            strong { "{event.summary}" }
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
                    h2 { class: "card-title", "Record details" }
                    p { class: "card-description", "Source-owned identity and cross-record relationships." }
                }
                div { class: "card-body",
                    match resource.cloned() {
                        None => rsx! { Loading {} },
                        Some(Err(message)) => rsx! { Notice { message } },
                        Some(Ok(None)) => rsx! {
                            Empty { title: "Select an activity".to_owned(),
                                description: "Select a saved event to inspect its canonical resource identity.".to_owned() }
                        },
                        Some(Ok(Some(entry))) => rsx! {
                            div { class: "activity-resource",
                                span { class: "badge muted", "{entry.kind}" }
                                strong { {entry.data.get("title").and_then(Value::as_str).unwrap_or("Recorded resource")} }
                                p { class: "small muted mono", "{entry.reference}" }
                                p { class: "small muted", "Owner: {entry.owner}" }
                                if !entry.links.is_empty() {
                                    h3 { class: "section-title", "Linked resources" }
                                    for link in entry.links {
                                        div { class: "activity-link",
                                            span { "{link.relation}" }
                                            span { class: "small mono", "{link.reference}" }
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
