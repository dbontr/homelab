//! Original file custody through a bounded, host-mediated MCP Apps tool.
//! This module uses neither a browser owner cookie nor an MCP bearer token.
use crate::components::{Card, Empty, Notice};
use crate::mcp_app;
use dioxus::prelude::*;
use serde_json::{Value, json};
use wasm_bindgen::JsValue;

const ORIGINAL_LIMIT: usize = 5 * 1024 * 1024;
const ALPHABET: &[u8; 64] = b"ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789+/";

fn to_base64(bytes: &[u8]) -> String {
    let mut encoded = String::with_capacity((bytes.len() + 2) / 3 * 4);
    for chunk in bytes.chunks(3) {
        let a = chunk[0];
        let b = chunk.get(1).copied().unwrap_or(0);
        let c = chunk.get(2).copied().unwrap_or(0);
        encoded.push(ALPHABET[(a >> 2) as usize] as char);
        encoded.push(ALPHABET[(((a & 3) << 4) | (b >> 4)) as usize] as char);
        encoded.push(if chunk.len() > 1 {
            ALPHABET[(((b & 15) << 2) | (c >> 6)) as usize] as char
        } else {
            '='
        });
        encoded.push(if chunk.len() > 2 {
            ALPHABET[(c & 63) as usize] as char
        } else {
            '='
        });
    }
    encoded
}
fn digit(b: u8) -> Option<u8> {
    match b {
        b'A'..=b'Z' => Some(b - b'A'),
        b'a'..=b'z' => Some(b - b'a' + 26),
        b'0'..=b'9' => Some(b - b'0' + 52),
        b'+' => Some(62),
        b'/' => Some(63),
        _ => None,
    }
}
fn from_base64(text: &str) -> Result<Vec<u8>, String> {
    let bytes = text.as_bytes();
    if bytes.is_empty() || bytes.len() % 4 != 0 || bytes.len() > (ORIGINAL_LIMIT + 2) / 3 * 4 + 4 {
        return Err("Original file exceeds the bounded transfer limit.".to_owned());
    }
    let mut output = Vec::with_capacity(bytes.len() / 4 * 3);
    for (index, group) in bytes.chunks_exact(4).enumerate() {
        let final_chunk = index + 1 == bytes.len() / 4;
        let padding = group.iter().rev().take_while(|&&b| b == b'=').count();
        if padding > 2 || (!final_chunk && padding > 0) {
            return Err("Invalid base64 padding.".to_owned());
        }
        let a = digit(group[0]).ok_or("Invalid base64 original.")?;
        let b = digit(group[1]).ok_or("Invalid base64 original.")?;
        let c = if padding >= 2 {
            0
        } else {
            digit(group[2]).ok_or("Invalid base64 original.")?
        };
        let d = if padding >= 1 {
            0
        } else {
            digit(group[3]).ok_or("Invalid base64 original.")?
        };
        if padding == 2 && (b & 15) != 0 || padding == 1 && (c & 3) != 0 {
            return Err("Noncanonical base64 original.".to_owned());
        }
        output.push((a << 2) | (b >> 4));
        if padding < 2 {
            output.push((b << 4) | (c >> 2));
        }
        if padding < 1 {
            output.push((c << 6) | d);
        }
        if output.len() > ORIGINAL_LIMIT {
            return Err("Original file exceeds 5 MiB.".to_owned());
        }
    }
    Ok(output)
}
fn allowed_mime(value: &str) -> bool {
    matches!(
        value,
        "image/jpeg"
            | "image/png"
            | "image/webp"
            | "image/heic"
            | "application/pdf"
            | "text/plain"
            | "text/markdown"
    )
}

fn original_url(data: &[u8], mime: &str) -> Result<String, String> {
    let blob_bytes = js_sys::Uint8Array::from(data);
    let chunks = js_sys::Array::new();
    chunks.push(&JsValue::from(blob_bytes));
    let mut options = web_sys::BlobPropertyBag::new();
    options.set_type(mime);
    let blob = web_sys::Blob::new_with_u8_array_sequence_and_options(&chunks, &options)
        .map_err(|_| "Could not create a local original file.".to_owned())?;
    web_sys::Url::create_object_url_with_blob(&blob)
        .map_err(|_| "Browser refused the original download.".to_owned())
}

#[component]
pub fn EmbeddedAttachments(owner_kind: String, owner_id: String) -> Element {
    let mut busy = use_signal(|| false);
    let mut error = use_signal(String::new);
    let mut notice = use_signal(String::new);
    let mut files = use_signal(|| None::<Value>);
    let mut download_url = use_signal(String::new);
    let mut download_name = use_signal(String::new);
    let mut preview_mime = use_signal(String::new);
    rsx! {
        Card{
            title:"Original Learning files".to_owned(),
            description:"Preserve original evidence separately from any model interpretation. Maximum 5 MiB per browser transfer.".to_owned(),
            div{class:"card-body stack",
                if !error().is_empty(){Notice{message:error()}}
                if !notice().is_empty(){p{role:"status",class:"small muted","{notice()}"}}
                button{class:"btn btn-outline",disabled:busy(),r#type:"button",
                    onclick:{
                        let kind=owner_kind.clone();
                        let id=owner_id.clone();
                        move |_|{
                            if busy(){return;}
                            busy.set(true);error.set(String::new());
                            let args=json!({"owner_kind":kind,"owner_id":id});
                            spawn(async move{
                                match mcp_app::call_learning_tool::<Value>(
                                    "list_attachments",args).await{
                                    Ok(data)=>files.set(Some(data)),
                                    Err(message)=>error.set(message),
                                }
                                busy.set(false);
                            });
                        }
                    },
                    "List original files"
                }
                label{class:"form-row",
                    span{class:"label","Upload a photograph, PDF or text original"}
                    input{class:"field",r#type:"file",disabled:busy(),
                        accept:".jpg,.jpeg,.png,.webp,.heic,.pdf,.txt,.md",
                        onchange:{
                            let kind=owner_kind.clone();
                            let id=owner_id.clone();
                            move |event|{
                                if busy(){return}
                                let Some(file)=event.files().into_iter().next()else{return};
                                if file.size()==0||(file.size() as u64)>ORIGINAL_LIMIT as u64{
                                    error.set("Original file must be between 1 byte and 5 MiB.".to_owned());
                                    return;
                                }
                                let filename=file.name().to_owned();
                                let media_type=file.content_type().unwrap_or_default();
                                if filename.is_empty()||filename.len()>250||
                                    filename.contains('/')||filename.contains('\\')||
                                    !allowed_mime(&media_type){
                                    error.set("Choose a supported original file type and name.".to_owned());
                                    return;
                                }
                                let kind=kind.clone();
                                let id=id.clone();
                                error.set(String::new());busy.set(true);
                                spawn(async move{
                                    let response=match file.read_bytes().await{
                                        Ok(bytes) if !bytes.is_empty()&&bytes.len()<=ORIGINAL_LIMIT=>{
                                            let params=json!({
                                                "owner_kind":kind,"owner_id":id,
                                                "filename":filename,"mime_type":media_type,
                                                "base64":to_base64(&bytes),"note":"",
                                            });
                                            mcp_app::call_learning_tool::<Value>(
                                                "upload_attachment",params).await
                                        }
                                        Ok(_)=>Err("Original file is too large.".to_owned()),
                                        Err(_)=>Err("Could not read the selected file.".to_owned()),
                                    };
                                    match response{
                                        Ok(record)=>{
                                            notice.set(format!("Saved original {}. No mastery was awarded.",
                                                record.get("id").and_then(Value::as_str).unwrap_or("record")));
                                            files.set(None);
                                        }
                                        Err(message)=>error.set(message),
                                    }
                                    busy.set(false);
                                });
                            }
                        }
                    }
                }
                if let Some(data)=files(){
                    let records=data.get("attachments").and_then(Value::as_array)
                        .or_else(||data.get("items").and_then(Value::as_array))
                        .cloned().unwrap_or_default();
                    if records.is_empty(){
                        Empty{title:"No originals".to_owned(),
                            description:"Attach a file to preserve the source evidence.".to_owned()}
                    }else{
                        div{class:"task-items",
                            for (index,file) in records.iter().enumerate(){
                                div{class:"task-item",key:"{index}",
                                    div{class:"task-information",
                                        strong{ {file.get("filename").and_then(Value::as_str)
                                            .unwrap_or("Original file").to_owned()} }
                                        span{class:"small muted",
                                            "{file.get("mime_type").and_then(Value::as_str).unwrap_or("unknown")} · "
                                            "{file.get("size_bytes").and_then(Value::as_u64).unwrap_or(0)} bytes"
                                        }
                                        if let Some(annotations)=file.get("annotations").and_then(Value::as_array){
                                            for (index,annotation) in annotations.iter().enumerate(){
                                                p{key:"annotation-{index}",class:"small muted",
                                                    {annotation.get("method").and_then(Value::as_str).unwrap_or("Annotation")}
                                                    ": "
                                                    {annotation.get("text").and_then(Value::as_str).unwrap_or("")}
                                                    if let Some(uncertainty)=annotation.get("uncertainty").and_then(Value::as_str){
                                                        " · uncertainty: {uncertainty}"
                                                    }
                                                }
                                            }
                                        }
                                        if let Some(id)=file.get("id").and_then(Value::as_str){
                                            button{class:"btn btn-outline btn-sm",r#type:"button",disabled:busy(),
                                                onclick:{
                                                    let attachment_id=id.to_owned();
                                                    move |_|{
                                                        if busy(){return}
                                                        let id=attachment_id.clone();
                                                        busy.set(true);error.set(String::new());
                                                        spawn(async move{
                                                            let response=mcp_app::call_learning_tool::<Value>(
                                                                "get_attachment",json!({
                                                                    "attachment_id":id,"include_original":true
                                                                })).await;
                                                            match response{
                                                                Ok(original)=>{
                                                                    if original.get("integrity_verified").and_then(Value::as_bool)!=Some(true){
                                                                        error.set("Learning did not verify the original content.".to_owned());
                                                                    }else{
                                                                        let mime=original.get("mime_type").and_then(Value::as_str)
                                                                            .unwrap_or("");
                                                                        let size=original.get("size_bytes").and_then(Value::as_u64)
                                                                            .unwrap_or(u64::MAX);
                                                                        let encoded=original.get("base64").and_then(Value::as_str)
                                                                            .unwrap_or("");
                                                                        match from_base64(encoded){
                                                                            Ok(decoded) if allowed_mime(mime)&&
                                                                                size==decoded.len() as u64=>{
                                                                                match original_url(&decoded,mime){
                                                                                    Ok(url)=>{
                                                                                        if !download_url().is_empty(){
                                                                                            let _=web_sys::Url::revoke_object_url(&download_url());
                                                                                        }
                                                                                        download_url.set(url);
                                                                                        preview_mime.set(mime.to_owned());
                                                                                        download_name.set(original.get("filename")
                                                                                            .and_then(Value::as_str).unwrap_or("original").to_owned());
                                                                                        notice.set("Original is ready for download.".to_owned());
                                                                                    }
                                                                                    Err(msg)=>error.set(msg),
                                                                                }
                                                                            }
                                                                            _=>error.set("Original file size, type or encoding is invalid.".to_owned()),
                                                                        }
                                                                    }
                                                                }
                                                                Err(msg)=>error.set(msg),
                                                            }
                                                            busy.set(false);
                                                        });
                                                    }
                                                },
                                                "Prepare original download"
                                            }
                                        }
                                    }
                                }
                            }
                        }
                    }
                }
                if !download_url().is_empty(){
                    if preview_mime().starts_with("image/"){
                        img{src:"{download_url()}",alt:"{download_name()}",
                            style:"display:block;max-width:100%;max-height:420px;object-fit:contain"}
                    }
                    a{class:"btn btn-outline",href:"{download_url()}",download:"{download_name()}",
                        "Download verified original"}
                    button{class:"btn btn-outline",r#type:"button",
                        onclick:move |_|{
                            let _=web_sys::Url::revoke_object_url(&download_url());
                            download_url.set(String::new());
                            download_name.set(String::new());
                            preview_mime.set(String::new());
                        },
                        "Close original preview"
                    }
                }
            }
        }
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    #[test]
    fn bounded_codec_uses_canonical_base64() {
        for original in [b"A".as_slice(), b"AB", b"ABC", b"Hello!"] {
            assert_eq!(from_base64(&to_base64(original)).unwrap(), original);
        }
        assert!(from_base64("TWFu=").is_err());
        assert!(from_base64("TW=F").is_err());
        assert!(from_base64("TQ==").is_ok());
        assert!(!allowed_mime("text/html"));
    }
}
