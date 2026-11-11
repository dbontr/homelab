use dioxus::prelude::*;
use reqwest::Method;
use serde_json::{json, Value};

use crate::api;
use crate::components::{Card, Notice};

#[derive(Clone, PartialEq)]
struct Allocation {
    host: String,
    project_id: String,
    image_pool_id: String,
    node_id: String,
    architecture: String,
    cpu: String,
    memory_mb: String,
    duration_minutes: String,
    gpu: String,
    npu: String,
    fpga: String,
    min_gpu_memory_mb: String,
    usb_passthrough: bool,
    usb_node: String,
    usb_device_id: String,
    display: bool,
}
impl Default for Allocation {
    fn default() -> Self {
        Self {
            host:String::new(),project_id:String::new(),image_pool_id:String::new(),node_id:String::new(),
            architecture:String::new(),cpu:"1".to_owned(),memory_mb:"2048".to_owned(),
            duration_minutes:"120".to_owned(),gpu:"0".to_owned(),npu:"0".to_owned(),
            fpga:"0".to_owned(),min_gpu_memory_mb:"0".to_owned(),usb_passthrough:false,
            usb_node:String::new(),usb_device_id:String::new(),display:false,
        }
    }
}

fn allocation_request(selection: &Allocation) -> Result<Value,String> {
    let cpu = selection.cpu.parse::<f64>().map_err(|_| "CPU must be a number.")?;
    let memory = selection.memory_mb.parse::<u64>()
        .map_err(|_| "Memory must be a whole number of MiB.")?;
    let duration = selection.duration_minutes.parse::<u64>()
        .map_err(|_| "Duration must be whole minutes.")?;
    let gpu = selection.gpu.parse::<u64>().map_err(|_| "GPU count must be a whole number.")?;
    let npu = selection.npu.parse::<u64>().map_err(|_| "NPU count must be a whole number.")?;
    let fpga = selection.fpga.parse::<u64>().map_err(|_| "FPGA count must be a whole number.")?;
    let gpu_memory = selection.min_gpu_memory_mb.parse::<u64>()
        .map_err(|_| "Minimum GPU memory must be a whole number of MiB.")?;
    if selection.project_id.is_empty() || !cpu.is_finite() || !(0.1..=32.0).contains(&cpu) ||
        !(64..=131072).contains(&memory) || !(1..=1440).contains(&duration) ||
        gpu > 8 || npu > 8 || fpga > 8 || gpu_memory > 1000000 ||
        gpu_memory > 0 && gpu == 0 ||
        selection.usb_passthrough && (selection.usb_node.is_empty()
            || selection.usb_device_id.is_empty()) {
        return Err("The workspace request has invalid resources or missing device identity.".to_owned());
    }
    let mut hardware = serde_json::Map::new();
    if gpu > 0 {hardware.insert("gpu".to_owned(),json!(gpu));}
    if npu > 0 {hardware.insert("npu".to_owned(),json!(npu));}
    if fpga > 0 {hardware.insert("fpga".to_owned(),json!(fpga));}
    if selection.usb_passthrough {hardware.insert("usb".to_owned(),json!(1));}
    Ok(json!({
        "host":selection.host,
        "project_id":selection.project_id,
        "image_pool_id":selection.image_pool_id,
        "node_id":selection.node_id,
        "architecture":selection.architecture,
        "cpu":cpu,"memory_mb":memory,"duration_minutes":duration,
        "hardware":hardware,"min_gpu_memory_mb":gpu_memory,
        "usb_passthrough":selection.usb_passthrough,
        "usb_node":selection.usb_node,"usb_device_id":selection.usb_device_id,
        "display":selection.display,
        "display_width":if selection.display {1440}else{0},
        "display_height":if selection.display {900}else{0},
    }))
}

#[component]
pub fn ComputeManagement(csrf: String) -> Element {
    let mut draft=use_signal(Allocation::default);
    let mut confirm=use_signal(||false);
    let mut busy=use_signal(||false);
    let mut error=use_signal(String::new);
    let mut preview=use_signal(||None::<Value>);
    let mut result=use_signal(||None::<Value>);

    rsx! {
        Card {
            title:"Compute allocation".to_owned(),
            description:"Plan resources without reservation, then request an owner-authorized workspace. Device leases are enforced by Compute.".to_owned(),
            div { class:"card-body stack",
                if !error().is_empty(){ Notice { message:error() } }
                if let Some(value)=result(){
                    div { class:"alert", role:"status",
                        strong { "Workspace request accepted" }
                        p { {api::property(&value,&["id","workspace_id","status"])} }
                    }
                }
                form {
                    class:"task-editor-fields",
                    onsubmit: {
                        let csrf=csrf.clone();
                        move |event| {
                            event.prevent_default();
                            if busy() || !confirm(){return;}
                            let selection=draft();
                            if selection.host.is_empty() || selection.image_pool_id.is_empty(){
                                error.set("Choose the approved executor host and image pool.".to_owned());
                                return;
                            }
                            let request=match allocation_request(&selection){
                                Ok(value)=>value,
                                Err(msg)=>{error.set(msg);return}
                            };
                            let token=csrf.clone();
                            busy.set(true);error.set(String::new());result.set(None);
                            spawn(async move{
                                match api::write::<Value,_>(Method::POST,
                                    "/v1/compute/workspaces",&token,&request).await{
                                    Ok(value)=>{result.set(Some(value));confirm.set(false);}
                                    Err(msg)=>error.set(msg),
                                }
                                busy.set(false);
                            });
                        }
                    },
                    label { class:"form-row",
                        span { class:"label","Independent executor host" }
                        input { class:"field", required:true, maxlength:"63",
                            placeholder:"Configured NixOS host identity",
                            value:"{draft().host}",
                            oninput:move |e|{let mut d=draft();d.host=e.value();draft.set(d)}
                        }
                    }
                    label { class:"form-row",
                        span { class:"label","Project ID" }
                        input { class:"field", required:true, maxlength:"80",
                            value:"{draft().project_id}",
                            oninput:move |e|{let mut d=draft();d.project_id=e.value();draft.set(d)}
                        }
                    }
                    label { class:"form-row",
                        span { class:"label","Approved image pool" }
                        input { class:"field", required:true, maxlength:"80",
                            value:"{draft().image_pool_id}",
                            oninput:move |e|{let mut d=draft();d.image_pool_id=e.value();draft.set(d)}
                        }
                    }
                    div { class:"task-field-grid",
                        label { class:"form-row",
                            span { class:"label","CPU cores" }
                            input { class:"field", r#type:"number", min:"0.1", max:"32", step:"0.1",
                                value:"{draft().cpu}",
                                oninput:move |e|{let mut d=draft();d.cpu=e.value();draft.set(d)}
                            }
                        }
                        label { class:"form-row",
                            span { class:"label","Memory (MiB)" }
                            input { class:"field", r#type:"number", min:"64", max:"131072",
                                value:"{draft().memory_mb}",
                                oninput:move |e|{let mut d=draft();d.memory_mb=e.value();draft.set(d)}
                            }
                        }
                        label { class:"form-row",
                            span { class:"label","Duration (minutes)" }
                            input { class:"field", r#type:"number", min:"1", max:"1440",
                                value:"{draft().duration_minutes}",
                                oninput:move |e|{let mut d=draft();d.duration_minutes=e.value();draft.set(d)}
                            }
                        }
                        label { class:"form-row",
                            span { class:"label","Exact worker (optional)" }
                            input { class:"field", value:"{draft().node_id}",
                                oninput:move |e|{let mut d=draft();d.node_id=e.value();draft.set(d)}
                            }
                        }
                        label { class:"form-row",
                            span { class:"label","Architecture" }
                            select { class:"field", value:"{draft().architecture}",
                                onchange:move |e|{let mut d=draft();d.architecture=e.value();draft.set(d)},
                                option { value:"","Any supported architecture" }
                                option { value:"amd64","AMD64" }
                                option { value:"arm64","ARM64" }
                            }
                        }
                        label { class:"form-row",
                            span { class:"label","GPU count" }
                            input { class:"field", r#type:"number", min:"0", max:"8",
                                value:"{draft().gpu}",
                                oninput:move |e|{let mut d=draft();d.gpu=e.value();draft.set(d)}
                            }
                        }
                        label { class:"form-row",
                            span { class:"label","NPU count" }
                            input { class:"field", r#type:"number", min:"0", max:"8",
                                value:"{draft().npu}",
                                oninput:move |e|{let mut d=draft();d.npu=e.value();draft.set(d)}
                            }
                        }
                        label { class:"form-row",
                            span { class:"label","FPGA count" }
                            input { class:"field", r#type:"number", min:"0", max:"8",
                                value:"{draft().fpga}",
                                oninput:move |e|{let mut d=draft();d.fpga=e.value();draft.set(d)}
                            }
                        }
                        label { class:"form-row",
                            span { class:"label","Minimum GPU memory (MiB)" }
                            input { class:"field", r#type:"number", min:"0", max:"1000000",
                                value:"{draft().min_gpu_memory_mb}",
                                oninput:move |e|{let mut d=draft();d.min_gpu_memory_mb=e.value();draft.set(d)}
                            }
                        }
                    }
                    label { class:"inline-row",
                        input { r#type:"checkbox", checked:draft().usb_passthrough,
                            onchange:move |e|{let mut d=draft();d.usb_passthrough=e.checked();draft.set(d)}
                        }
                        span { "Request one explicitly permitted USB device" }
                    }
                    if draft().usb_passthrough {
                        label { class:"form-row",
                            span { class:"label","USB worker" }
                            input { class:"field", required:true, value:"{draft().usb_node}",
                                oninput:move |e|{let mut d=draft();d.usb_node=e.value();draft.set(d)}
                            }
                        }
                        label { class:"form-row",
                            span { class:"label","Exact USB device ID" }
                            input { class:"field", required:true, value:"{draft().usb_device_id}",
                                oninput:move |e|{let mut d=draft();d.usb_device_id=e.value();draft.set(d)}
                            }
                        }
                    }
                    label { class:"inline-row",
                        input { r#type:"checkbox", checked:draft().display,
                            onchange:move |e|{let mut d=draft();d.display=e.checked();draft.set(d)}
                        }
                        span { "Provide a virtual display" }
                    }
                    div { class:"inline-row",
                        button { class:"btn btn-outline", r#type:"button", disabled:busy(),
                            onclick:{
                                let csrf=csrf.clone();
                                move |_|{
                                    if busy(){return;}
                                    let request=match allocation_request(&draft()){
                                        Ok(value)=>value,
                                        Err(msg)=>{error.set(msg);return;}
                                    };
                                    let token=csrf.clone();
                                    busy.set(true);error.set(String::new());preview.set(None);
                                    spawn(async move{
                                        match api::write::<Value,_>(Method::POST,
                                            "/v1/compute/workspaces/plan",&token,&request).await{
                                            Ok(value)=>preview.set(Some(value)),
                                            Err(msg)=>error.set(msg),
                                        }
                                        busy.set(false);
                                    });
                                }
                            },
                            "Preview allocation"
                        }
                        button { class:"btn btn-primary", disabled:busy()||!confirm(),
                            "Request workspace"
                        }
                    }
                    label { class:"inline-row",
                        input { r#type:"checkbox", checked:confirm(), disabled:busy(),
                            onchange:move |e|confirm.set(e.checked())
                        }
                        span { "I understand this operation reserves resources and requires independent owner authorization." }
                    }
                }
                if let Some(value)=preview(){
                    div { class:"alert", role:"status",
                        strong { "Allocation preview — no resources reserved" }
                        pre { class:"mono small", "{serde_json::to_string_pretty(&value).unwrap_or_default()}" }
                    }
                }
            }
        }
        ComputeCatalog { csrf:csrf.clone() }
        ComputeStop { csrf:csrf.clone() }
    }
}

#[component]
fn ComputeStop(csrf: String) -> Element {
    let mut host=use_signal(String::new);
    let mut project=use_signal(String::new);
    let mut workspace=use_signal(String::new);
    let mut confirmed=use_signal(||false);
    let mut busy=use_signal(||false);
    let mut error=use_signal(String::new);
    let mut message=use_signal(String::new);
    rsx! {
        Card {
            title:"Workspace lifecycle".to_owned(),
            description:"Stopping a workspace is a privileged, independently authorized action.".to_owned(),
            form { class:"card-body task-editor-fields",
                onsubmit:move |event|{
                    event.prevent_default();
                    if busy()||!confirmed(){return;}
                    let id=workspace();
                    if id.len()!=32||!id.bytes().all(|c|c.is_ascii_hexdigit()){
                        error.set("Enter a 32-character workspace ID.".to_owned());return;
                    }
                    let request=json!({"host":host(),"project_id":project(),"confirm":true});
                    let path=format!("/v1/compute/workspaces/{id}/stop");
                    let token=csrf.clone();
                    busy.set(true);error.set(String::new());message.set(String::new());
                    spawn(async move{
                        match api::write::<Value,_>(Method::POST,&path,&token,&request).await{
                            Ok(_)=>{message.set("Stop requested.".to_owned());confirmed.set(false);}
                            Err(msg)=>error.set(msg),
                        }
                        busy.set(false);
                    });
                },
                if !error().is_empty(){ Notice { message:error() } }
                if !message().is_empty(){p{role:"status",class:"small muted","{message()}"}}
                label { class:"form-row",
                    span { class:"label","Independent executor host" }
                    input { class:"field", required:true, maxlength:"63", value:"{host()}",
                        oninput:move |e|host.set(e.value())
                    }
                }
                label { class:"form-row",
                    span { class:"label","Project ID" }
                    input { class:"field", required:true, value:"{project()}",
                        oninput:move |e|project.set(e.value())
                    }
                }
                label { class:"form-row",
                    span { class:"label","Workspace ID" }
                    input { class:"field", required:true, maxlength:"32", value:"{workspace()}",
                        oninput:move |e|workspace.set(e.value())
                    }
                }
                label { class:"inline-row",
                    input { r#type:"checkbox", checked:confirmed(), disabled:busy(),
                        onchange:move |e|confirmed.set(e.checked())
                    }
                    span { "Confirm the exact workspace to stop" }
                }
                button { class:"btn btn-outline", disabled:busy()||!confirmed(),"Request stop" }
            }
        }
    }
}

#[derive(Clone, PartialEq, Default)]
struct CatalogDraft {
    operation: String,
    host: String,
    id: String,
    image: String,
    image_id: String,
    architecture: String,
    description: String,
    packages: String,
    name: String,
    version: String,
    platform: String,
    distribution: String,
    source_url: String,
    license_notes: String,
    kind: String,
    status: String,
    reason: String,
    review_note: String,
    pool_kind: String,
    pool_id: String,
    revision: String,
}

fn catalog_request(draft: &CatalogDraft) -> Result<Value,String> {
    if draft.id.is_empty(){return Err("Enter a catalog ID.".to_owned());}
    let mut payload=json!({"id":draft.id});
    match draft.operation.as_str(){
        "register-image"=>{
            if !draft.image.contains("@sha256:"){return Err("Use a digest-qualified OCI image.".to_owned());}
            payload["image"]=json!(draft.image);
            payload["architecture"]=json!(draft.architecture);
            payload["description"]=json!(draft.description);
        }
        "register-software"=>{
            let packages:Vec<&str>=draft.packages.split(',').map(str::trim)
                .filter(|name|!name.is_empty()).collect();
            if packages.is_empty() || packages.len()>128 {
                return Err("Enter 1 to 128 apt package names.".to_owned());
            }
            if draft.host.is_empty(){
                return Err("Select the independent executor host.".to_owned());
            }
            payload["host"]=json!(draft.host);
            payload["image_id"]=json!(draft.image_id);
            payload["packages"]=json!(packages);
            payload["description"]=json!(draft.description);
        }
        "propose-program"=>{
            for (name,value) in [("name",&draft.name),("version",&draft.version),
                ("platform",&draft.platform),("distribution",&draft.distribution),
                ("source_url",&draft.source_url),("license_notes",&draft.license_notes),
                ("description",&draft.description)]{
                payload[name]=json!(value);
            }
        }
        "review-program"=>{
            let revision=draft.revision.parse::<u32>()
                .map_err(|_|"A current catalog revision is required.".to_owned())?;
            if revision==0 {return Err("Revision must be positive.".to_owned());}
            payload["expected_revision"]=json!(revision);
            payload["status"]=json!(draft.status);
            payload["review_note"]=json!(draft.review_note);
            payload["pool_kind"]=json!(draft.pool_kind);
            payload["pool_id"]=json!(draft.pool_id);
        }
        "update-entry"=>{
            let revision=draft.revision.parse::<u32>()
                .map_err(|_|"A current catalog revision is required.".to_owned())?;
            if revision==0 {return Err("Revision must be positive.".to_owned());}
            payload["expected_revision"]=json!(revision);
            payload["kind"]=json!(draft.kind);
            payload["status"]=json!(draft.status);
            payload["reason"]=json!(draft.reason);
        }
        _=>return Err("Choose a supported catalog action.".to_owned()),
    }
    Ok(payload)
}

#[component]
fn ComputeCatalog(csrf: String) -> Element {
    let mut draft=use_signal(||CatalogDraft{
        operation:"register-software".to_owned(),..CatalogDraft::default()
    });
    let mut busy=use_signal(||false);
    let mut error=use_signal(String::new);
    let mut receipt=use_signal(||None::<Value>);
    rsx! {
        Card {
            title:"Software and image catalog".to_owned(),
            description:"Register immutable images and software, or submit revision-qualified catalog reviews. Modifications require independent authorization.".to_owned(),
            form {
                class:"card-body task-editor-fields",
                onsubmit:move |event|{
                    event.prevent_default();
                    if busy(){return;}
                    let current=draft();
                    let request=match catalog_request(&current){
                        Ok(value)=>value,
                        Err(msg)=>{error.set(msg);return;}
                    };
                    let path=format!("/v1/compute/catalog/{}",current.operation);
                    let token=csrf.clone();
                    busy.set(true);error.set(String::new());receipt.set(None);
                    spawn(async move{
                        match api::write::<Value,_>(Method::POST,&path,&token,&request).await{
                            Ok(value)=>receipt.set(Some(value)),
                            Err(msg)=>error.set(msg),
                        }
                        busy.set(false);
                    });
                },
                if !error().is_empty(){ Notice { message:error() } }
                if let Some(value)=receipt(){
                    p { role:"status",class:"small muted",
                        "Catalog operation accepted: " {api::property(&value,&["id","status","revision"])}
                    }
                }
                label { class:"form-row",
                    span { class:"label","Catalog action" }
                    select { class:"field", value:"{draft().operation}", disabled:busy(),
                        onchange:move |e|{let mut d=draft();d.operation=e.value();draft.set(d)},
                        option{value:"register-image","Register OCI image"}
                        option{value:"register-software","Register apt packages"}
                        option{value:"propose-program","Propose program"}
                        option{value:"review-program","Review program"}
                        option{value:"update-entry","Change entry state"}
                    }
                }
                label { class:"form-row",
                    span { class:"label","Catalog identifier" }
                    input { class:"field", required:true, maxlength:"80", value:"{draft().id}",
                        oninput:move |e|{let mut d=draft();d.id=e.value();draft.set(d)}
                    }
                }
                if draft().operation=="register-image"{
                    label { class:"form-row",
                        span { class:"label","Exact image digest" }
                        input { class:"field", required:true, placeholder:"registry/repository@sha256:...",
                            value:"{draft().image}",
                            oninput:move |e|{let mut d=draft();d.image=e.value();draft.set(d)}
                        }
                    }
                    label { class:"form-row",
                        span { class:"label","Architecture" }
                        input { class:"field", value:"{draft().architecture}",
                            oninput:move |e|{let mut d=draft();d.architecture=e.value();draft.set(d)}
                        }
                    }
                }
                if draft().operation=="register-software"{
                    label { class:"form-row",
                        span { class:"label","Independent executor host" }
                        input { class:"field",required:true,maxlength:"63",
                            value:"{draft().host}",
                            oninput:move |e|{let mut d=draft();d.host=e.value();draft.set(d)}
                        }
                    }
                    label { class:"form-row",
                        span { class:"label","Approved image pool ID" }
                        input { class:"field",required:true,value:"{draft().image_id}",
                            oninput:move |e|{let mut d=draft();d.image_id=e.value();draft.set(d)}
                        }
                    }
                    label { class:"form-row",
                        span { class:"label","APT packages (comma separated)" }
                        textarea { class:"field",required:true,rows:"3",value:"{draft().packages}",
                            oninput:move |e|{let mut d=draft();d.packages=e.value();draft.set(d)}
                        }
                    }
                }
                if draft().operation=="register-image"||draft().operation=="register-software"||
                    draft().operation=="propose-program"{
                    label { class:"form-row",
                        span { class:"label","Description" }
                        textarea { class:"field",rows:"2",value:"{draft().description}",
                            oninput:move |e|{let mut d=draft();d.description=e.value();draft.set(d)}
                        }
                    }
                }
                if draft().operation=="propose-program"{
                    label { class:"form-row",
                        span { class:"label","Program name" }
                        input { class:"field", required:true, value:"{draft().name}",
                            oninput:move |e|{let mut d=draft();d.name=e.value();draft.set(d)}
                        }
                    }
                    label { class:"form-row",
                        span { class:"label","Version" }
                        input { class:"field",  value:"{draft().version}",
                            oninput:move |e|{let mut d=draft();d.version=e.value();draft.set(d)}
                        }
                    }
                    label { class:"form-row",
                        span { class:"label","Platform" }
                        input { class:"field",  value:"{draft().platform}",
                            oninput:move |e|{let mut d=draft();d.platform=e.value();draft.set(d)}
                        }
                    }
                    label { class:"form-row",
                        span { class:"label","Distribution" }
                        input { class:"field",  value:"{draft().distribution}",
                            oninput:move |e|{let mut d=draft();d.distribution=e.value();draft.set(d)}
                        }
                    }
                    label { class:"form-row",
                        span { class:"label","Source URL" }
                        input { class:"field",  value:"{draft().source_url}",
                            oninput:move |e|{let mut d=draft();d.source_url=e.value();draft.set(d)}
                        }
                    }
                    label { class:"form-row",
                        span { class:"label","License notes" }
                        input { class:"field",  value:"{draft().license_notes}",
                            oninput:move |e|{let mut d=draft();d.license_notes=e.value();draft.set(d)}
                        }
                    }
                }
                if draft().operation=="update-entry"||draft().operation=="review-program"{
                    label { class:"form-row",
                        span { class:"label","Expected revision" }
                        input { class:"field",r#type:"number",min:"1",required:true,
                            value:"{draft().revision}",
                            oninput:move |e|{let mut d=draft();d.revision=e.value();draft.set(d)}
                        }
                    }
                    label { class:"form-row",
                        span { class:"label","State" }
                        select { class:"field",value:"{draft().status}",
                            onchange:move |e|{let mut d=draft();d.status=e.value();draft.set(d)},
                            option { value:"","Choose a status" }
                            if draft().operation=="update-entry"{
                                option { value:"active","Active" }
                                option { value:"disabled","Disabled" }
                                option { value:"retired","Retired" }
                            }else{
                                option { value:"reviewed","Reviewed" }
                                option { value:"available","Available" }
                                option { value:"disabled","Disabled" }
                                option { value:"rejected","Rejected" }
                                option { value:"retired","Retired" }
                            }
                        }
                    }
                    if draft().operation=="update-entry"{
                        label { class:"form-row",
                            span { class:"label","Entry kind" }
                            select { class:"field",value:"{draft().kind}",
                                onchange:move |e|{let mut d=draft();d.kind=e.value();draft.set(d)},
                                option { value:"","Choose a pool" }
                                option { value:"image","Image pool" }
                                option { value:"software","Apt package pool" }
                            }
                        }
                        label { class:"form-row",
                            span { class:"label","Reason" }
                            textarea { class:"field",rows:"2",value:"{draft().reason}",
                                oninput:move |e|{let mut d=draft();d.reason=e.value();draft.set(d)}
                            }
                        }
                    }else{
                        label { class:"form-row",
                            span { class:"label","Review note" }
                            textarea { class:"field",rows:"2",value:"{draft().review_note}",
                                oninput:move |e|{let mut d=draft();d.review_note=e.value();draft.set(d)}
                            }
                        }
                        label { class:"form-row",
                            span { class:"label","Approved pool kind (optional)" }
                            select { class:"field",value:"{draft().pool_kind}",
                                onchange:move |e|{let mut d=draft();d.pool_kind=e.value();draft.set(d)},
                                option { value:"","No pool" }
                                option { value:"image","Image" }
                                option { value:"software","Apt packages" }
                            }
                        }
                        label { class:"form-row",
                            span { class:"label","Approved pool ID" }
                            input { class:"field",value:"{draft().pool_id}",
                                oninput:move |e|{let mut d=draft();d.pool_id=e.value();draft.set(d)}
                            }
                        }
                    }
                }
                button { class:"btn btn-primary",disabled:busy(),
                    if busy(){"Submitting…"}else{"Request catalog change"}
                }
                p { class:"small muted",
                    "The independent security authority verifies privileges and any required destructive confirmation before the Compute controller receives this action."
                }
            }
        }
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    #[test]
    fn allocation_refuses_unknown_unsafe_quantities_and_missing_usb_identity(){
        let mut draft=Allocation::default();
        draft.project_id="research".to_owned();
        assert!(allocation_request(&draft).is_ok());
        draft.gpu="not a number".to_owned();
        assert!(allocation_request(&draft).is_err());
        draft.gpu="1".to_owned();
        draft.usb_passthrough=true;
        assert!(allocation_request(&draft).is_err());
        draft.usb_node="jupiter".to_owned();
        draft.usb_device_id="probe".to_owned();
        assert!(allocation_request(&draft).is_ok());
    }
    #[test]
    fn catalog_actions_never_construct_arbitrary_backend_tool_names(){
        let mut draft=CatalogDraft::default();
        draft.id="go-image".to_owned();
        draft.operation="unknown-tool".to_owned();
        assert!(catalog_request(&draft).is_err());
        draft.operation="register-image".to_owned();
        draft.image="registry.example/go:latest".to_owned();
        assert!(catalog_request(&draft).is_err());
    }
}
