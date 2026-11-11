use dioxus::prelude::*;
use reqwest::Method;
use serde_json::{json, Value};

use crate::api;
use crate::components::Notice;

#[component]
pub fn KnowledgeApproval(run_id: String, proposal: Value, csrf: String) -> Element {
    let mut expanded = use_signal(|| Option::<Value>::None);
    let mut approved = use_signal(|| false);
    let mut busy = use_signal(|| false);
    let mut error = use_signal(String::new);
    let proposal_id = api::property(&proposal, &["id"]);
    let status = api::property(&proposal, &["status"]);
    let title = api::property(&proposal, &["title"]);
    rsx! {
        details { class:"engineering-knowledge-item",
            summary {
                span { "{title}" }
                span { class:"muted", "{status}" }
            }
            div { class:"card-body",
                p { class:"muted", "A proposal is not added to project knowledge until it is approved." }
                if expanded().is_none() {
                    button {
                        class:"btn btn-outline btn-sm",
                        disabled:busy(),
                        onclick: {
                            let id=proposal_id.clone();
                            let run=run_id.clone();
                            move |_|{
                                let url=format!("/v1/engineering/runs/{run}/knowledge/{id}");
                                busy.set(true);
                                error.set(String::new());
                                spawn(async move{
                                    match api::get::<Value>(&url).await {
                                        Ok(value)=>expanded.set(Some(value)),
                                        Err(message)=>error.set(message)
                                    }
                                    busy.set(false);
                                });
                            }
                        },
                        "Inspect evidence and proposal"
                    }
                }
                if let Some(record) = expanded(){
                    h4 { {api::property(&record,&["kind"])} — {api::property(&record,&["title"])} }
                    p { class:"muted", {api::property(&record,&["body"])} }
                    p { class:"mono", {format!("Source: {} @ {}", api::property(&record,&["repository"]), api::property(&record,&["revision"]))} }
                    if let Some(evidence)=record.get("evidence_sha256").and_then(Value::as_object) {
                        for (path,digest) in evidence {
                            p { class:"mono", "{path}: {digest.as_str().unwrap_or("")}" }
                        }
                    }
                    if status=="proposed" && !approved(){
                        button {
                            class:"btn btn-outline btn-sm",
                            disabled:busy(),
                            onclick: {
                                let id=proposal_id.clone();
                                let run=run_id.clone();
                                let token=csrf.clone();
                                move |_|{
                                    let url=format!("/v1/engineering/runs/{run}/knowledge/{id}/approve");
                                    busy.set(true);
                                    error.set(String::new());
                                    spawn(async move{
                                        match api::write::<Value,_>(Method::POST,&url,&token,&json!({})).await {
                                            Ok(_)=>approved.set(true),
                                            Err(message)=>error.set(message)
                                        }
                                        busy.set(false);
                                    });
                                }
                            },
                            "Approve for project knowledge"
                        }
                    }
                }
                if approved(){ p { role:"status", class:"muted", "Contribution published to project knowledge." } }
                if !error().is_empty(){ Notice { message:error() } }
            }
        }
    }
}
