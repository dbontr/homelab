use dioxus::prelude::*;
use serde_json::Value;

use crate::api;
use crate::components::{Badge, Card, Empty, Heading, Icon, Loading, Notice};

#[component]
pub fn EngineeringRuns() -> Element {
    let mut runs = use_resource(|| async {
        api::get::<Value>("/v1/engineering/runs").await
    });

    rsx! {
        Heading {
            title: "Engineering runs".to_owned(),
            description: "Isolated source changes, verified checks, and draft publications.".to_owned(),
            button {
                class: "btn btn-outline",
                onclick: move |_| runs.restart(),
                Icon { name: "refresh".to_owned() }
                "Refresh"
            }
        }
        Card {
            title: "Runs".to_owned(),
            description: "Only confirmed execution results are reported. Drafts require separate review.".to_owned(),
            match runs.cloned() {
                None => rsx! { Loading {} },
                Some(Err(err)) => rsx! { Notice { message: err } },
                Some(Ok(value)) => {
                    let items = api::list(&value, &["runs"]);
                    rsx! {
                        if items.is_empty() {
                            Empty {
                                title: "No engineering runs".to_owned(),
                                description: "Submit an engineering objective through the platform's authenticated tools.".to_owned()
                            }
                        } else {
                            div { class: "table-clip",
                                table { class: "table",
                                    thead {
                                        tr {
                                            th { "Run" }
                                            th { "Project" }
                                            th { "Status" }
                                            th { "Completed steps" }
                                            th { "Draft" }
                                        }
                                    }
                                    tbody {
                                        for item in items {
                                            tr {
                                                td { class: "mono muted",
                                                    {api::property(&item, &["id"])}
                                                }
                                                td { {api::property(&item, &["project"])} }
                                                td { Badge { text: api::property(&item, &["status"]) } }
                                                td {
                                                    {item.get("steps")
                                                        .and_then(Value::as_array)
                                                        .map(|steps| steps.iter().filter(|s| s.get("status").and_then(Value::as_str) == Some("succeeded")).count().to_string())
                                                        .unwrap_or_else(|| "—".to_owned())}
                                                }
                                                td {
                                                    if let Some(url) = item.get("steps")
                                                        .and_then(Value::as_array)
                                                        .and_then(|steps| steps.iter().find(|s| s.get("kind").and_then(Value::as_str) == Some("publish")))
                                                        .and_then(|step| step.get("result"))
                                                        .and_then(|result| result.get("url"))
                                                        .and_then(Value::as_str) {
                                                        a { href: "{url}", target: "_blank", rel: "noopener noreferrer", "Review draft" }
                                                    } else {
                                                        span { class: "muted", "—" }
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
        }
    }
}
