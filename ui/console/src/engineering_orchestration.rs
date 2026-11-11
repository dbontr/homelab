use dioxus::prelude::*;
use reqwest::Method;
use serde_json::{json, Value};

use crate::api;
use crate::components::{Card, Notice};

fn entries(value: &str) -> Vec<String> {
    value.lines().map(str::trim).filter(|line| !line.is_empty()).map(str::to_owned).collect()
}

#[component]
pub fn TeamCreate(csrf: String) -> Element {
    let mut project = use_signal(String::new);
    let mut repository = use_signal(String::new);
    let mut revision = use_signal(String::new);
    let mut objective = use_signal(String::new);
    let mut editable_paths = use_signal(String::new);
    let mut checks = use_signal(String::new);
    let mut image_pool = use_signal(|| "debian-bookworm-slim".to_owned());
    let mut submitting = use_signal(|| false);
    let mut error = use_signal(String::new);
    let mut created = use_signal(String::new);
    rsx! {
        Card {
            title: "Coordinate engineering agents".to_owned(),
            description: "The architect allocates up to four isolated tasks within the approved file scope.".to_owned(),
            form {
                class: "form-stack",
                onsubmit: move |event| {
                    event.prevent_default();
                    if submitting() { return; }
                    let files = entries(&editable_paths());
                    let commands = entries(&checks());
                    if revision().len()!=40 || files.len()<2 || files.len()>16 ||
                        commands.is_empty() || commands.len()>8 ||
                        project().is_empty() || repository().is_empty() || objective().trim().is_empty() {
                        error.set("Specify a project, repository, exact revision, objective, two or more editable files, and approved checks.".to_owned());
                        return;
                    }
                    let request = json!({
                        "base": {
                            "project": project(), "repository": repository(),
                            "revision": revision(), "expected_head": revision(), "base_branch":"main",
                            "title":"Cooperative engineering", "objective": objective(),
                            "allowed_paths":files, "checks":commands,
                            "image_pool_id":image_pool(), "cpu":1, "memory_mb":2048,
                            "duration_minutes":60, "autonomous":true, "prepare_only":true,
                            "include_knowledge":true, "review":true
                        },
                        "auto_plan":true, "tasks":[]
                    });
                    error.set(String::new());
                    created.set(String::new());
                    submitting.set(true);
                    let token=csrf.clone();
                    spawn(async move {
                        match api::write::<Value,_>(Method::POST,"/v1/engineering/teams",&token,&request).await {
                            Ok(result) => created.set(api::property(&result,&["id"])),
                            Err(message) => error.set(message)
                        }
                        submitting.set(false);
                    });
                },
                label { class:"form-row", span { class:"label","Project ID" }
                    input { class:"field", required:true, maxlength:"80",
                        value:"{project()}", oninput:move |e| project.set(e.value()) }
                }
                label { class:"form-row", span { class:"label","Source repository" }
                    input { class:"field", required:true, placeholder:"owner/repository",
                        value:"{repository()}", oninput:move |e| repository.set(e.value()) }
                }
                label { class:"form-row", span { class:"label","Exact source revision" }
                    input { class:"field", required:true, maxlength:"40",
                        value:"{revision()}", oninput:move |e| revision.set(e.value()) }
                }
                label { class:"form-row", span { class:"label","Engineering objective" }
                    textarea { class:"field", required:true, rows:"3",
                        value:"{objective()}", oninput:move |e| objective.set(e.value()) }
                }
                label { class:"form-row", span { class:"label","Approved source files · one per line" }
                    textarea { class:"field", required:true, rows:"3",
                        value:"{editable_paths()}", oninput:move |e| editable_paths.set(e.value()) }
                }
                label { class:"form-row", span { class:"label","Approved verification commands · one per line" }
                    textarea { class:"field", required:true, rows:"2",
                        value:"{checks()}", oninput:move |e| checks.set(e.value()) }
                }
                label { class:"form-row", span { class:"label","Compute image" }
                    input { class:"field", required:true,
                        value:"{image_pool()}", oninput:move |e| image_pool.set(e.value()) }
                }
                button { class:"btn btn-primary", disabled:submitting(),
                    if submitting() { "Creating tasks…" } else { "Plan and start cooperating agents" }
                }
            }
            if !error().is_empty(){ Notice { message: error() } }
            if !created().is_empty(){
                p { class:"muted", role:"status", "Engineering team created: {created()}. Refresh to inspect dependencies." }
            }
        }
    }
}

#[component]
pub fn DependencyGraph(members: Vec<Value>) -> Element {
    let mut selected = use_signal(String::new);
    let chosen = members.iter().find(|item| api::property(item,&["key"]) == selected()).cloned();
    rsx! {
        div { class:"engineering-graph", role:"group", aria_label:"Agent dependency graph",
            for member in &members {
                button {
                    class:"engineering-graph-node",
                    r#type:"button",
                    aria_pressed: if selected() == api::property(member,&["key"]) {"true"} else {"false"},
                    onclick: {
                        let key = api::property(member,&["key"]);
                        move |_| selected.set(key.clone())
                    },
                    span { class:"engineering-graph-role", {api::property(member,&["role"])} }
                    span { class:"engineering-graph-key", {api::property(member,&["key"])} }
                    span { class:"engineering-graph-deps",
                        "Depends on: "
                        {
                            let predecessors = member.get("depends_on").and_then(Value::as_array);
                            predecessors.map(|items| {
                                if items.is_empty() { "none".to_owned() } else {
                                    items.iter().filter_map(Value::as_str).collect::<Vec<_>>().join(", ")
                                }
                            }).unwrap_or_else(|| "none".to_owned())
                        }
                    }
                    span { class:"engineering-graph-state",
                        {member.get("run").map(|run|api::property(run,&["status"])).unwrap_or_default()}
                    }
                }
            }
        }
        if let Some(chosen) = chosen {
            div { class:"engineering-graph-detail",
                strong { {api::property(&chosen,&["role"])} }
                span { class:"muted", {format!("Run: {}", chosen.get("run").map(|run|api::property(run,&["id"])).unwrap_or_default())} }
                span { class:"muted", "Dependencies must complete before this member begins." }
            }
        }
    }
}

#[component]
pub fn TeamIntegration(team: Value, csrf: String) -> Element {
    let mut title = use_signal(|| "Combine verified engineering changes".to_owned());
    let mut body = use_signal(String::new);
    let mut submitting = use_signal(|| false);
    let mut result = use_signal(String::new);
    let mut error = use_signal(String::new);
    let id = api::property(&team,&["id"]);
    let branch = api::property(&team,&["base_branch"]);
    let revision = api::property(&team,&["revision"]);
    rsx! {
        form { class:"form-stack",
            onsubmit:move |event|{
                event.prevent_default();
                if submitting(){return}
                let request=json!({"title":title(),"body":body(),
                    "base_branch":branch.clone(),"expected_head":revision.clone()});
                let url=format!("/v1/engineering/teams/{id}/integrate");
                let token=csrf.clone();
                submitting.set(true);
                error.set(String::new());
                spawn(async move {
                    match api::write::<Value,_>(Method::POST,&url,&token,&request).await {
                        Ok(value)=>result.set(api::property(&value,&["id"])),
                        Err(message)=>error.set(message)
                    }
                    submitting.set(false);
                });
            },
            label { class:"form-row", span { class:"label","Draft title" }
                input { class:"field", value:"{title()}", oninput:move |e| title.set(e.value()) }
            }
            label { class:"form-row", span { class:"label","Review notes" }
                textarea { class:"field", rows:"2", value:"{body()}", oninput:move |e| body.set(e.value()) }
            }
            button { class:"btn btn-outline", disabled:submitting(),
                if submitting() { "Scheduling integration…" } else { "Verify combined changes and prepare draft" }
            }
        }
        if !error().is_empty(){ Notice { message:error() } }
        if !result().is_empty() {
            p { role:"status", class:"muted", "Integration submitted. Refresh to inspect verification and draft publication." }
        }
    }
}
