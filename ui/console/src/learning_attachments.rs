use dioxus::prelude::*;
use serde_json::Value;

use crate::api;
use crate::components::{Card, Empty, Loading, Notice};

fn valid_owner(kind: &str, id: &str) -> bool {
    matches!(kind, "problem" | "session" | "source")
        && !id.is_empty()
        && id.len() <= 128
        && id
            .bytes()
            .all(|ch| ch.is_ascii_alphanumeric() || ch == b'-' || ch == b'_')
}

#[component]
pub fn LearningAttachments(csrf: String, owner_kind: String, owner_id: String) -> Element {
    let mut busy = use_signal(|| false);
    let mut error = use_signal(String::new);
    let mut notice = use_signal(String::new);
    let mut attachments = use_resource({
        let kind = owner_kind.clone();
        let id = owner_id.clone();
        move || {
            let kind = kind.clone();
            let id = id.clone();
            async move {
                if !valid_owner(&kind, &id) {
                    return Err("No Learning record selected for original files.".to_owned());
                }
                api::get::<Value>(&format!("/v1/learning/attachments/{kind}/{id}")).await
            }
        }
    });
    rsx! {
        Card {
            title: "Original work".to_owned(),
            description: "Keep original photographs, documents and source notes with this Learning record. Attachments do not establish mastery.".to_owned(),
            div { class: "card-body stack",
                if !error().is_empty() {
                    Notice { message: error() }
                }
                if !notice().is_empty() {
                    p { class: "small muted", role: "status", "{notice()}" }
                }
                label { class: "form-row",
                    span { class: "label", "Attach an original file" }
                    input {
                        class: "field",
                        r#type: "file",
                        accept: ".jpg,.jpeg,.png,.webp,.heic,.pdf,.txt,.md,image/jpeg,image/png,image/webp,image/heic,application/pdf,text/plain,text/markdown",
                        disabled: busy(),
                        onchange: {
                            let kind = owner_kind.clone();
                            let id = owner_id.clone();
                            let csrf = csrf.clone();
                            move |event| {
                                if busy() || !valid_owner(&kind,&id) { return; }
                                let Some(file) = event.files().into_iter().next() else {return;};
                                if file.size() == 0 || file.size() > 5 * 1024 * 1024 {
                                    error.set("The selected file must be between 1 byte and 5 MiB.".to_owned());
                                    return;
                                }
                                let filename = file.name().to_owned();
                                let media_type = file.content_type().unwrap_or_default();
                                if filename.is_empty() || filename.len() > 250 ||
                                    !matches!(media_type.as_str(),
                                        "image/jpeg"|"image/png"|"image/webp"|"image/heic"|
                                        "application/pdf"|"text/plain"|"text/markdown") {
                                    error.set("Choose a supported image, PDF, or text document.".to_owned());
                                    return;
                                }
                                let token = csrf.clone();
                                let kind = kind.clone();
                                let id = id.clone();
                                error.set(String::new());
                                busy.set(true);
                                spawn(async move {
                                    let result = match file.read_bytes().await {
                                        Ok(bytes) if !bytes.is_empty() && bytes.len() <= 5 * 1024 * 1024 => {
                                            api::upload_learning_attachment(
                                                &token,&kind,&id,&filename,&media_type,bytes.to_vec()
                                            ).await
                                        }
                                        Ok(_) => Err("The original file exceeds the permitted size.".to_owned()),
                                        Err(_) => Err("Could not read the original file.".to_owned()),
                                    };
                                    match result {
                                        Ok(saved) => {
                                            notice.set(format!(
                                                "Original saved as {}. Interpretation and evaluation remain separate.",
                                                api::property(&saved,&["id"])
                                            ));
                                            attachments.restart();
                                        }
                                        Err(message) => error.set(message),
                                    }
                                    busy.set(false);
                                });
                            }
                        }
                    }
                }
                p { class: "small muted",
                    "JPEG, PNG, WebP, HEIC, PDF, or plain text. Maximum 5 MiB per file. Content is checked by Learning."
                }
                button {
                    class: "btn btn-outline",
                    disabled: busy(), r#type: "button",
                    onclick: move |_| attachments.restart(),
                    "Refresh originals"
                }
                match attachments.cloned() {
                    None => rsx! { Loading {} },
                    Some(Err(message)) => rsx! { Notice { message } },
                    Some(Ok(value)) => {
                        let all = api::list(&value,&["attachments","items"]);
                        rsx! {
                            if all.is_empty() {
                                Empty {
                                    title: "No originals".to_owned(),
                                    description: "Select a file to preserve the original work for this record.".to_owned()
                                }
                            } else {
                                div { class: "table-clip",
                                    table { class: "table",
                                        thead { tr {
                                            th { "Original" }
                                            th { "Type" }
                                            th { "Size" }
                                            th { "Record" }
                                        } }
                                        tbody {
                                            for file in all {
                                                tr {
                                                    td {
                                                        strong { {api::property(&file,&["filename","id"])} }
                                                        if let Some(notes) = file.get("annotations").and_then(Value::as_array) {
                                                            for note in notes.iter().take(5) {
                                                                p { class: "small muted",
                                                                    {format!("{}: {}", api::property(note,&["method"]), api::property(note,&["text"]))}
                                                                }
                                                            }
                                                        }
                                                    }
                                                    td { {api::property(&file,&["mime_type"])} }
                                                    td { class: "muted",
                                                        {format!("{} bytes", api::property(&file,&["size_bytes"]))}
                                                    }
                                                    td {
                                                        if let Some(id) = file.get("id").and_then(Value::as_str).filter(|id|
                                                            !id.is_empty() && id.len() <= 128 &&
                                                            id.bytes().all(|ch| ch.is_ascii_alphanumeric() || ch==b'-' || ch==b'_')
                                                        ) {
                                                            a {
                                                                class: "btn btn-outline btn-sm",
                                                                href: format!("/v1/learning/attachment-originals/{id}"),
                                                                "Download original"
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
                p { class: "small muted",
                    "The browser download is limited to 5 MiB. Larger originals stored through Learning remain available through the Learning service."
                }
            }
        }
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn attachment_owners_are_explicit() {
        for kind in ["problem", "session", "source"] {
            assert!(valid_owner(kind, "abC-123_def"));
        }
        for kind in ["project", "health", ""] {
            assert!(!valid_owner(kind, "record"));
        }
        assert!(!valid_owner("session", "../escape"));
        assert!(!valid_owner("session", "bad/id"));
        assert!(!valid_owner("source", ""));
    }
}
