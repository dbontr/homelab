use dioxus::prelude::*;
use serde_json::Value;

use crate::api::{self, Device, Node, Profile, Task, ToolList};
use crate::components::{Badge, Card, Empty, Heading, Icon, Loading, Notice};

fn total<T>(value: Option<Result<Vec<T>, String>>) -> String {
    match value {
        Some(Ok(records)) => records.len().to_string(),
        _ => "—".to_owned(),
    }
}

#[component]
pub fn Overview() -> Element {
    let mut nodes = use_resource(|| async { api::get::<Vec<Node>>("/v1/nodes").await });
    let mut devices = use_resource(|| async { api::get::<Vec<Device>>("/v1/devices").await });
    let mut profiles = use_resource(|| async { api::get::<Vec<Profile>>("/v1/compute/profiles").await });
    let mut tasks = use_resource(|| async { api::get::<Vec<Task>>("/v1/tasks").await });

    let node_count = total(nodes.cloned());
    let device_count = total(devices.cloned());
    let profile_count = total(profiles.cloned());
    let pending_count = match tasks.cloned() {
        Some(Ok(all)) => all.iter().filter(|item| item.status == "open").count().to_string(),
        _ => "—".to_owned(),
    };
    let node_preview = nodes.cloned().and_then(Result::ok).unwrap_or_default();
    let task_preview: Vec<Task> = tasks
        .cloned()
        .and_then(Result::ok)
        .unwrap_or_default()
        .into_iter()
        .filter(|task| task.status == "open")
        .take(5)
        .collect();

    rsx! {
        Heading {
            title: "Overview".to_owned(),
            description: "Your computing environment and current work.".to_owned(),
            button {
                class: "btn btn-outline",
                onclick: move |_| {
                    nodes.restart();
                    devices.restart();
                    profiles.restart();
                    tasks.restart();
                },
                Icon { name: "refresh".to_owned() }
                "Refresh"
            }
        }
        div { class: "metric-grid",
            div { class: "card metric",
                div { class: "metric-title", span { "Enrolled and SSH nodes" } Icon { name: "server".to_owned() } }
                div { class: "metric-value", "{node_count}" }
                div { class: "metric-hint", "Declared inventory, not live status" }
            }
            div { class: "card metric",
                div { class: "metric-title", span { "Devices" } Icon { name: "compute".to_owned() } }
                div { class: "metric-value", "{device_count}" }
                div { class: "metric-hint", "Registered hardware" }
            }
            div { class: "card metric",
                div { class: "metric-title", span { "Build profiles" } Icon { name: "software".to_owned() } }
                div { class: "metric-value", "{profile_count}" }
                div { class: "metric-hint", "Saved configurations" }
            }
            div { class: "card metric",
                div { class: "metric-title", span { "Open tasks" } Icon { name: "tasks".to_owned() } }
                div { class: "metric-value", "{pending_count}" }
                div { class: "metric-hint", "Recorded tasks" }
            }
        }
        div { class: "section-grid",
            Card { title: "Registered nodes".to_owned(), description: "From the declarative hardware inventory.".to_owned(),
                if let Some(Err(error)) = nodes.cloned() {
                    Notice { message: error }
                } else if nodes.cloned().is_none() {
                    Loading {}
                } else if node_preview.is_empty() {
                    Empty { title: "No nodes declared".to_owned(), description: "Add a node through the hardware inventory.".to_owned() }
                } else {
                    div { class: "table-clip",
                        table { class: "table",
                            thead { tr { th { "Node" } th { "Role" } th { "Management" } } }
                            tbody {
                                for node in node_preview.into_iter().take(6) {
                                    tr { key: "{node.id}",
                                        td { "{node.id}" }
                                        td { class: "muted", "{node.role}" }
                                        td { Badge { text: node.mode } }
                                    }
                                }
                            }
                        }
                    }
                }
            }
            Card { title: "Upcoming work".to_owned(), description: "Open tasks in your personal workspace.".to_owned(),
                if let Some(Err(error)) = tasks.cloned() {
                    Notice { message: error }
                } else if tasks.cloned().is_none() {
                    Loading {}
                } else if task_preview.is_empty() {
                    Empty { title: "Nothing pending".to_owned(), description: "No open tasks are recorded.".to_owned() }
                } else {
                    div { class: "stack",
                        for task in task_preview {
                            div { key: "{task.id}", class: "inline-row space-between",
                                div {
                                    div { "{task.title}" }
                                    if task.minutes > 0 {
                                        span { class: "small muted", "{task.minutes} min" }
                                    }
                                }
                                Badge { text: task.status }
                            }
                        }
                    }
                }
            }
        }
    }
}

#[component]
pub fn Projects() -> Element {
    let mut data = use_resource(|| async { api::get::<Value>("/v1/engineering/projects").await });
    let mut filter = use_signal(String::new);
    let mut selected = use_signal(||None::<String>);
    let mut jobs = use_resource(||{
        let project=selected();
        async move{
            match project{
                Some(id)=>api::get::<Value>(&format!("/v1/engineering/projects/{id}/jobs")).await,
                None=>Ok(Value::Null),
            }
        }
    });
    let mut workspaces = use_resource(||{
        let project=selected();
        async move{
            match project{
                Some(id)=>api::get::<Value>(&format!("/v1/engineering/projects/{id}/workspaces")).await,
                None=>Ok(Value::Null),
            }
        }
    });
    let rows = data.cloned().and_then(Result::ok)
        .map(|value| api::list(&value, &["projects", "items", "results"]))
        .unwrap_or_default();
    let search = filter().trim().to_ascii_lowercase();
    let filtered: Vec<Value> = rows.iter().filter(|project| {
        if search.is_empty() { return true; }
        ["name","title","id","description","summary"]
            .iter()
            .any(|field|project.get(*field).and_then(Value::as_str)
                .is_some_and(|text|text.to_ascii_lowercase().contains(&search)))
    }).cloned().collect();
    rsx! {
        Heading {
            title: "Projects".to_owned(),
            description: "Research, engineering and isolated development workspaces.".to_owned(),
            button { class: "btn btn-outline", onclick: move |_| {
                data.restart();jobs.restart();workspaces.restart();
            },
                Icon { name: "refresh".to_owned() } "Refresh"
            }
        }
        div { class: "inline-row", style: "margin-bottom: 1rem; max-width: 20rem;",
            input {
                class: "field",
                r#type: "search",
                placeholder: "Filter projects...",
                aria_label: "Filter projects",
                value: "{filter}",
                oninput: move |event| filter.set(event.value()),
            }
        }
        Card { title: "Project directory".to_owned(),
            description: "Projects may link multiple Git repositories and private research libraries.".to_owned(),
            if let Some(Err(error)) = data.cloned() {
                Notice { message: error }
            } else if data.cloned().is_none() {
                Loading {}
            } else if filtered.is_empty() {
                Empty { title: "No projects match".to_owned(),
                    description: "No project records match your filter, or none have been returned.".to_owned() }
            } else {
                div { class: "table-clip",
                    table { class: "table",
                        thead { tr { th { "Project" } th { "Identifier" } th { "Description" } th { "Inspect" } } }
                        tbody {
                            for (index, project) in filtered.iter().enumerate() {
                                tr { key: "{index}",
                                    td { {api::property(project, &["name", "title", "id"])} }
                                    td { class: "mono muted", {api::property(project, &["id", "project_id"])} }
                                    td { class: "muted", {api::property(project, &["description", "summary"])} }
                                    td {
                                        button { class: "btn btn-outline btn-sm",
                                            onclick: {
                                                let id=api::property(project,&["id","project_id"]);
                                                move |_|selected.set(Some(id.clone()))
                                            },
                                            "Inspect"
                                        }
                                    }
                                }
                            }
                        }
                    }
                }
            }
        }
        if let Some(id) = selected() {
            Card {
                title: "Execution jobs".to_owned(),
                description:format!("Project {id}; observed jobs only. Agent execution is not controlled here."),
                match jobs.cloned(){
                    None=>rsx!{Loading{}},
                    Some(Err(e))=>rsx!{Notice{message:e}},
                    Some(Ok(value))=>{
                        let items=api::list(&value,&["jobs","items","results"]);
                        rsx!{
                            if items.is_empty(){Empty{
                                title:"No jobs reported".to_owned(),
                                description:"Only jobs confirmed by compute appear here.".to_owned()
                            }}
                            else{
                                div{class:"table-clip",
                                    table{class:"table",
                                        thead{tr{th{"Job"}th{"Status"}th{"Worker"}th{"Created"}}}
                                        tbody{
                                            for item in items{
                                                tr{
                                                    td{class:"mono muted",{api::property(&item,&["id","job_id"])}}
                                                    td{Badge{text:api::property(&item,&["status"])}}
                                                    td{class:"muted",{api::property(&item,&["node","worker","node_id"])}}
                                                    td{class:"muted",{api::property(&item,&["created","created_at"])}}
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
            Card {
                title: "Isolated workspaces".to_owned(),
                description:"Workspaces are reported by compute; this view does not execute commands.".to_owned(),
                match workspaces.cloned(){
                    None=>rsx!{Loading{}},
                    Some(Err(e))=>rsx!{Notice{message:e}},
                    Some(Ok(value))=>{
                        let items=api::list(&value,&["workspaces","items","results"]);
                        rsx!{
                            if items.is_empty(){Empty{
                                title:"No workspaces reported".to_owned(),
                                description:"Create and manage isolated environments through Engineering.".to_owned()
                            }}
                            else{
                                div{class:"table-clip",
                                    table{class:"table",
                                        thead{tr{th{"Workspace"}th{"Status"}th{"Node"}th{"Image"}}}
                                        tbody{
                                            for item in items{
                                                tr{
                                                    td{class:"mono muted",{api::property(&item,&["id","workspace_id"])}}
                                                    td{Badge{text:api::property(&item,&["status"])}}
                                                    td{class:"muted",{api::property(&item,&["node","node_id"])}}
                                                    td{class:"muted",{api::property(&item,&["image"])}}
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

#[derive(Clone, Copy, PartialEq, Eq)]
enum ComputeTab { Nodes, Devices, Profiles }

#[component]
pub fn Compute() -> Element {
    let mut tab = use_signal(|| ComputeTab::Nodes);
    let mut nodes = use_resource(|| async { api::get::<Vec<Node>>("/v1/nodes").await });
    let mut devices = use_resource(|| async { api::get::<Vec<Device>>("/v1/devices").await });
    let mut profiles = use_resource(|| async { api::get::<Vec<Profile>>("/v1/compute/profiles").await });
    let mut workers = use_resource(|| async { api::get::<Value>("/v1/engineering/nodes").await });
    rsx! {
        Heading { title: "Compute".to_owned(),
            description: "Node enrollment, attached hardware, and reusable build environments.".to_owned(),
            button { class: "btn btn-outline",
                onclick: move |_| {nodes.restart(); devices.restart(); profiles.restart(); workers.restart();},
                Icon { name: "refresh".to_owned() } "Refresh"
            }
        }
        div { role: "tablist", aria_label: "Compute resources", class: "tabs",
            button { class: "tab", role: "tab", aria_selected: if tab()==ComputeTab::Nodes {"true"} else {"false"},
                onclick: move |_| tab.set(ComputeTab::Nodes), "Nodes" }
            button { class: "tab", role: "tab", aria_selected: if tab()==ComputeTab::Devices {"true"} else {"false"},
                onclick: move |_| tab.set(ComputeTab::Devices), "Devices" }
            button { class: "tab", role: "tab", aria_selected: if tab()==ComputeTab::Profiles {"true"} else {"false"},
                onclick: move |_| tab.set(ComputeTab::Profiles), "Build profiles" }
        }
        match tab() {
            ComputeTab::Nodes => rsx!{
                div { class: "stack",
                    NodeTable { records: nodes.cloned() }
                    WorkerTable { records: workers.cloned() }
                }
            },
            ComputeTab::Devices => rsx!{ DeviceTable { records: devices.cloned() } },
            ComputeTab::Profiles => rsx!{ ProfileTable { records: profiles.cloned() } },
        }
    }
}

#[component]
fn NodeTable(records: Option<Result<Vec<Node>, String>>) -> Element {
    rsx! {
        Card { title: "Machines".to_owned(), description: "Enrolled nodes and agentless SSH connections.".to_owned(),
            match records {
                None => rsx!{Loading {}},
                Some(Err(error)) => rsx!{Notice { message: error }},
                Some(Ok(nodes)) if nodes.is_empty() =>
                    rsx!{Empty {title:"No machines configured".to_owned(),description:"The node inventory is empty.".to_owned()}},
                Some(Ok(nodes)) => rsx!{
                    div { class: "table-clip",
                        table { class: "table",
                            thead { tr { th { "Name" } th { "Mode" } th { "Role" } th { "Architecture" } th { "Address" } } }
                            tbody {
                                for node in nodes {
                                    tr { key: "{node.id}",
                                        td { "{node.id}" }
                                        td { Badge { text: node.mode } }
                                        td { "{node.role}" }
                                        td { class: "muted", "{node.system}" }
                                        td { class: "mono muted", "{node.node_ip}" }
                                    }
                                }
                            }
                        }
                    }
                },
            }
        }
    }
}


#[component]
fn WorkerTable(records: Option<Result<Value, String>>) -> Element {
    let workers = records.as_ref().and_then(|result|result.as_ref().ok())
        .map(|value|api::list(value,&["nodes","workers","results"]))
        .unwrap_or_default();
    let error = records.as_ref().and_then(|result| result.as_ref().err()).cloned();
    let loading = records.is_none();
    rsx! {
        Card { title: "Compute workers".to_owned(), description: "Reported by the compute service, not inferred from inventory.".to_owned(),
            if let Some(error) = error {
                Notice { message: error }
            } else if loading {
                Loading {}
            } else if workers.is_empty() {
                Empty {
                    title: "No workers reported".to_owned(),
                    description: "The compute service returned no worker records.".to_owned()
                }
            } else {
                div { class: "table-clip",
                    table { class: "table",
                        thead { tr { th { "Worker" } th { "Status" } th { "Node" } } }
                        tbody {
                            for (index,worker) in workers.iter().enumerate() {
                                tr { key: "{index}",
                                    td { {api::property(worker,&["id","name","worker_id"])} }
                                    td { Badge { text: api::property(worker,&["status","state"]) } }
                                    td { class: "mono muted", {api::property(worker,&["node","node_id"])} }
                                }
                            }
                        }
                    }
                }
            }
        }
    }
}

#[component]
fn DeviceTable(records: Option<Result<Vec<Device>, String>>) -> Element {
    rsx! {
        Card {title:"Devices".to_owned(),description:"Permissions and eligibility are configured for each device.".to_owned(),
            match records {
                None => rsx!{Loading {}},
                Some(Err(error)) => rsx!{Notice {message:error}},
                Some(Ok(devices)) if devices.is_empty() =>
                    rsx!{Empty {title:"No hardware configured".to_owned(),description:"Add a device to the inventory.".to_owned()}},
                Some(Ok(devices)) => rsx!{
                    div { class: "table-clip",
                        table { class: "table",
                            thead { tr { th { "Device" } th { "Type" } th { "Connection" } th { "Node" } th { "Compute eligible" } } }
                            tbody {
                                for device in devices {
                                    tr {key:"{device.id}",
                                        td {"{device.id}"}
                                        td {Badge{text:device.kind}}
                                        td {"{device.connection}"}
                                        td {class:"mono muted","{device.node}"}
                                        td {
                                            if device.eligible_for_compute {"Yes"} else {"No"}
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

#[component]
fn ProfileTable(records: Option<Result<Vec<Profile>, String>>) -> Element {
    rsx! {
        Card {title:"Build profiles".to_owned(),description:"Saved environment configurations and revisions.".to_owned(),
            match records {
                None => rsx!{Loading {}},
                Some(Err(error)) => rsx!{Notice{message:error}},
                Some(Ok(profiles)) if profiles.is_empty() =>
                    rsx!{Empty {title:"No profiles saved".to_owned(),description:"Profiles define reproducible build steps and requirements.".to_owned()}},
                Some(Ok(profiles)) => rsx!{
                    div { class: "table-clip",
                        table { class: "table",
                            thead { tr { th { "Profile" } th { "Identifier" } th { "Revision" } th { "Environment" } } }
                            tbody {
                                for profile in profiles {
                                    tr {key:"{profile.id}",
                                        td {"{profile.title}"}
                                        td {class:"mono muted","{profile.id}"}
                                        td {"{profile.version}"}
                                        td {class:"muted","{profile.environment}"}
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

#[component]
pub fn Software() -> Element {
    let mut data = use_resource(|| async { api::get::<Value>("/v1/software").await });
    let value = data.cloned().and_then(Result::ok).unwrap_or(Value::Null);
    rsx! {
        Heading { title: "Software library".to_owned(),
            description: "Images, packages, and reviewed program requests from the compute catalog.".to_owned(),
            button { class: "btn btn-outline", onclick: move |_| data.restart(),
                Icon {name:"refresh".to_owned()} "Refresh"
            }
        }
        if let Some(Err(error))=data.cloned(){
            Notice {message:error}
        } else if data.cloned().is_none() {
            Loading {}
        } else {
            div {class:"stack",
                SoftwareCollection {title:"Images".to_owned(),description:"Available OCI image entries.".to_owned(),records:api::list(&value["images"], &["images","items","entries"])}
                SoftwareCollection {title:"Software".to_owned(),description:"Approved packages and toolchains.".to_owned(),records:api::list(&value["software"], &["software","items","entries"])}
                SoftwareCollection {title:"Programs".to_owned(),description:"Review and admission state.".to_owned(),records:api::list(&value["programs"], &["programs","items","entries"])}
            }
        }
    }
}

#[component]
fn SoftwareCollection(title: String, description: String, records: Vec<Value>) -> Element {
    rsx! {
        Card {title: title,description: description,
            if records.is_empty() {
                Empty {title:"No entries available".to_owned(),description:"The software service returned an empty collection.".to_owned()}
            } else {
                div {class:"table-clip",
                    table {class:"table",
                        thead {tr { th {"Name"} th {"Version"} th {"State"} }}
                        tbody {
                            for (index,item) in records.iter().enumerate(){
                                tr {key:"{index}",
                                    td {{api::property(item, &["name", "title", "id", "software_id", "image_id"])}}
                                    td {class:"mono muted",{api::property(item, &["version", "tag", "digest"])}}
                                    td {Badge{text:api::property(item,&["status","state"])}}
                                }
                            }
                        }
                    }
                }
            }
        }
    }
}

#[component]
pub fn Capabilities(title: String, description: String, prefix: String) -> Element {
    let mut data=use_resource(|| async{api::get::<ToolList>("/v1/tools").await});
    let tools: Vec<api::Tool> = data.cloned().and_then(Result::ok)
        .map(|catalog| catalog.tools.into_iter().filter(|tool|tool.name.starts_with(&prefix)).collect())
        .unwrap_or_default();
    rsx! {
        Heading {title: title.clone(),description,
            button {class:"btn btn-outline",onclick:move |_|data.restart(),
                Icon{name:"refresh".to_owned()} "Refresh"
            }
        }
        Card {title: format!("{title} capabilities"),description:"Capabilities reported by the unified API.".to_owned(),
            if let Some(Err(error))=data.cloned(){
                Notice {message:error}
            } else if data.cloned().is_none() {
                Loading {}
            } else if tools.is_empty() {
                Empty {title:"Integration unavailable".to_owned(),description:"No capabilities are currently advertised for this service.".to_owned()}
            } else {
                div {class:"table-clip",
                    table {class:"table",
                        thead {tr {th {"Capability"} th {"Description"} }}
                        tbody {
                            for tool in tools{
                                tr {key:"{tool.name}",
                                    td {class:"mono","{tool.name}"}
                                    td {class:"muted","{tool.description}"}
                                }
                            }
                        }
                    }
                }
            }
        }
    }
}

#[component]
pub fn Media() -> Element {
    let mut data=use_resource(|| async{api::get::<Value>("/v1/media/requests").await});
    let records=data.cloned().and_then(Result::ok).map(|v|api::list(&v,&["results","requests","items"])).unwrap_or_default();
    rsx! {
        Heading {title:"Media".to_owned(),description:"Media requests from your configured Seerr service.".to_owned(),
            button {class:"btn btn-outline",onclick:move |_|data.restart(),
                Icon{name:"refresh".to_owned()} "Refresh"
            }
        }
        Card {title:"Applications".to_owned(),description:"Launch the original media client and diagnostics service.".to_owned(),
            div {class:"inline-row",style:"flex-wrap:wrap",
                a {class:"btn btn-primary",href:"https://media.tail305103.ts.net/",target:"_blank",rel:"noopener noreferrer",
                    Icon{name:"media".to_owned()} "Open Moonfin"
                }
                a {class:"btn btn-outline",href:"https://monitor.dbontr.me/d/homelab-diag/homelab-diagnostics",
                    target:"_blank",rel:"noopener noreferrer",
                    Icon{name:"server".to_owned()} "Diagnostics"
                }
            }
        }
        Card {title:"Requests".to_owned(),description:"Read-only view of recent media requests.".to_owned(),
            if let Some(Err(error))=data.cloned(){
                Notice{message:error}
            } else if data.cloned().is_none() {
                Loading{}
            } else if records.is_empty(){
                Empty{title:"No media requests available".to_owned(),description:"The media service has not returned any requests.".to_owned()}
            } else {
                div {class:"table-clip",
                    table {class:"table",
                        thead {tr {th {"Request"} th {"Media ID"} th {"Status"} }}
                        tbody {
                            for (index,request) in records.iter().enumerate(){
                                tr {key:"{index}",
                                    td {"{api::property(request,&["title","name","mediaType","type"])}"}
                                    td {class:"mono muted","{api::property(request,&["mediaId","id"])}"}
                                    td {Badge{text:api::property(request,&["status","state"])}}
                                }
                            }
                        }
                    }
                }
            }
        }
    }
}

#[component]
pub fn Administration() -> Element {
    let mut data=use_resource(|| async{api::get::<ToolList>("/v1/tools").await});
    rsx! {
        Heading {title:"Administration".to_owned(),description:"Capabilities and protected operations.".to_owned(),
            button {class:"btn btn-outline",onclick:move |_|data.restart(),
                Icon{name:"refresh".to_owned()} "Refresh"
            }
        }
        div {class:"alert",
            Icon{name:"shield".to_owned()}
            span {"Administrative operations require separate owner authorization. The browser session is read-only."}
        }
        Card {title:"Exposed capabilities".to_owned(),description:"Only advertised API tools are listed here.".to_owned(),
            match data.cloned(){
                None=>rsx!{Loading{}},
                Some(Err(error))=>rsx!{Notice{message:error}},
                Some(Ok(tools))=>{
                    let count=tools.tools.len();
                    rsx!{
                        div {class:"inline-pair",
                            strong {class:"metric-value","{count}"}
                            span {class:"muted","capabilities returned by the gateway"}
                        }
                    }
                },
            }
        }
    }
}
