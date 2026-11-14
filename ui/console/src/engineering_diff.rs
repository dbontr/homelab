use dioxus::prelude::*;
use serde_json::Value;

use crate::api;
use crate::components::Notice;

#[component]
pub fn SourcePreviews(run_id: String) -> Element {
    let mut previews = use_signal(|| Option::<Vec<Value>>::None);
    let mut busy = use_signal(|| false);
    let mut error = use_signal(String::new);
    rsx! {
        div { class:"engineering-source-previews",
            button {
                class:"btn btn-outline btn-sm",
                disabled:busy(),
                onclick:move |_|{
                    let url=format!("/v1/engineering/runs/{run_id}/diffs");
                    busy.set(true);
                    error.set(String::new());
                    spawn(async move {
                        match api::get::<Value>(&url).await {
                            Ok(result) => previews.set(Some(api::list(&result,&["diffs"]))),
                            Err(message) => error.set(message)
                        }
                        busy.set(false);
                    });
                },
                if busy() { "Loading previews…" } else { "Inspect source patch previews" }
            }
            if let Some(results) = previews() {
                if results.is_empty() {
                    p { class:"muted", "No explicit patch previews are retained for this run." }
                } else {
                    p { class:"muted", "These are agent previews. Review the final GitHub draft for authoritative changes." }
                    for entry in results {
                        details {
                            class:"engineering-diff",
                            summary { {api::property(&entry,&["path"])} }
                            pre { {api::property(&entry,&["diff"])} }
                        }
                    }
                }
            }
            if !error().is_empty() { Notice { message:error() } }
        }
    }
}
