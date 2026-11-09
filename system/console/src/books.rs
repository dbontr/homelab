use dioxus::prelude::*;
use reqwest::Method;
use serde::Deserialize;

use crate::api;
use crate::components::{Empty, Heading, Icon, Loading, Notice};

#[derive(Clone, Debug, Deserialize, PartialEq)]
pub struct Book {
    pub work_id: String,
    pub title: String,
    #[serde(default)]
    pub authors: Vec<String>,
    #[serde(default)]
    pub first_published: i32,
    #[serde(default)]
    pub edition_count: i32,
    pub catalog_url: String,
}

#[derive(Clone, Debug, Deserialize, PartialEq)]
pub struct SearchResult {
    pub query: String,
    pub source: String,
    #[serde(default)]
    pub works: Vec<Book>,
}

#[derive(Clone, Debug, Deserialize, PartialEq)]
pub struct BookRequest {
    pub id: String,
    pub work_id: String,
    pub title: String,
    #[serde(default)]
    pub authors: Vec<String>,
    pub format: String,
    pub status: String,
    pub revision: i32,
}

#[derive(Clone, Debug, Default, Deserialize, PartialEq)]
#[serde(default)]
struct LendingOption {
    provider: String,
    status: String,
    r#match: String,
    edition_id: String,
    work_id: String,
    provider_url: String,
}

#[derive(Clone, Debug, Default, Deserialize, PartialEq)]
#[serde(default)]
struct LendingResult {
    isbn: String,
    source: String,
    options: Vec<LendingOption>,
    note: String,
}

fn encode(value: &str) -> String {
    value.as_bytes().iter().map(|byte| {
        if byte.is_ascii_alphanumeric() || matches!(*byte, b'-' | b'.' | b'_' | b'~') {
            (*byte as char).to_string()
        } else {
            format!("%{byte:02X}")
        }
    }).collect()
}

#[component]
pub fn Books(csrf: String) -> Element {
    let mut phrase = use_signal(String::new);
    let mut submitted = use_signal(String::new);
    let mut request_format = use_signal(|| "ebook".to_owned());
    let mut isbn = use_signal(String::new);
    let mut submitted_isbn = use_signal(String::new);
    let mut notice = use_signal(String::new);
    let mut error = use_signal(String::new);
    let mut busy = use_signal(|| false);

    let mut results = use_resource(move || {
        let query = submitted();
        async move {
            if query.is_empty() {
                return Ok(SearchResult { query: String::new(), source: String::new(), works: vec![] });
            }
            api::get::<SearchResult>(&format!("/v1/books/search?q={}", encode(&query))).await
        }
    });
    let mut lending = use_resource(move || {
        let query = submitted_isbn();
        async move {
            if query.is_empty() {
                return Ok(LendingResult::default());
            }
            api::get::<LendingResult>(&format!("/v1/books/lending?isbn={}", encode(&query))).await
        }
    });
    let mut wishes = use_resource(|| async {
        api::get::<Vec<BookRequest>>("/v1/books/requests").await
    });

    rsx! {
        Heading {
            title: "Books".to_owned(),
            description: "Find books and maintain a personal reading and acquisition wish list.".to_owned(),
            button { class: "btn btn-outline",
                onclick: move |_| { results.restart(); wishes.restart(); lending.restart(); },
                Icon { name: "refresh".to_owned() }
                "Refresh"
            }
        }
        if !error().is_empty() { Notice { message: error() } }
        if !notice().is_empty() { p { class: "small muted", "{notice}" } }
        section { class: "card",
            div { class: "card-header",
                h2 { class: "card-title", "Find a book" }
                p { class: "card-description",
                    "On-demand Open Library catalog search. No ebook, audiobook or copyrighted media is downloaded."
                }
            }
            div { class: "card-body",
                form { class: "book-search-controls",
                    onsubmit: move |event| {
                        event.prevent_default();
                        submitted.set(phrase().trim().to_owned());
                    },
                    label { class: "form-row",
                        "Title, author or subject"
                        input { class: "field", value: "{phrase}", required: true,
                            placeholder: "Search the catalog", oninput: move |event| phrase.set(event.value()) }
                    }
                    label { class: "form-row",
                        "Preferred format"
                        select { class: "field", value: "{request_format}",
                            onchange: move |event| request_format.set(event.value()),
                            option { value: "ebook", "Ebook" }
                            option { value: "print", "Print" }
                            option { value: "audiobook", "Audiobook" }
                        }
                    }
                    button { class: "btn btn-primary", type: "submit", "Search" }
                }
                match results.cloned() {
                    None => rsx! { Loading {} },
                    Some(Err(message)) => rsx! { Notice { message } },
                    Some(Ok(found)) => rsx! {
                        if !found.query.is_empty() {
                            p { class: "small muted", "Results from {found.source} for “{found.query}”." }
                        }
                        if found.works.is_empty() {
                            Empty {
                                title: "No books displayed".to_owned(),
                                description: "Enter a title or author; search is available when the catalog contact is configured.".to_owned()
                            }
                        } else {
                            div { class: "book-list",
                                for book in found.works {
                                    article { class: "book-entry",
                                        div { class: "book-description",
                                            strong { "{book.title}" }
                                            span { class: "small muted", "{book.authors.join(\", \")}" }
                                            span { class: "small muted",
                                                if book.first_published>0 { "First published {book.first_published} · " }
                                                "{book.edition_count} catalog editions"
                                            }
                                            a { class: "small book-catalog-link",
                                                href: "{book.catalog_url}", target: "_blank", rel: "noopener noreferrer",
                                                "View catalog record"
                                            }
                                        }
                                        button { class: "btn btn-outline",
                                            disabled: busy(),
                                            onclick: {
                                                let work_id = book.work_id.clone();
                                                let title = book.title.clone();
                                                let authors = book.authors.clone();
                                                let token = csrf.clone();
                                                move |_| {
                                                    let format = request_format();
                                                    busy.set(true);
                                                    error.set(String::new());
                                                    notice.set(String::new());
                                                    let payload = serde_json::json!({
                                                        "work_id":work_id.clone(),"title":title.clone(),
                                                        "authors":authors.clone(),"format":format
                                                    });
                                                    let token = token.clone();
                                                    spawn(async move {
                                                        match api::write::<BookRequest,_>(
                                                            Method::POST, "/v1/books/requests", &token, &payload
                                                        ).await {
                                                            Ok(_) => {
                                                                wishes.restart();
                                                                notice.set("Book added to your request list.".to_owned());
                                                            }
                                                            Err(message) => error.set(message),
                                                        }
                                                        busy.set(false);
                                                    });
                                                }
                                            },
                                            "Add request"
                                        }
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
                h2 { class: "card-title", "Licensed reading and borrowing" }
                p { class: "card-description",
                    "Check an exact ISBN for provider-managed reading or loan options. Your library account and borrowing decisions remain with the provider."
                }
            }
            div { class: "card-body stack",
                form {
                    class: "book-search-controls",
                    onsubmit: move |event| {
                        event.prevent_default();
                        submitted_isbn.set(isbn().trim().to_owned());
                    },
                    label { class: "form-row",
                        "ISBN-10 or ISBN-13"
                        input {
                            class: "field",
                            value: "{isbn}",
                            placeholder: "9780140328721",
                            required: true,
                            oninput: move |event| isbn.set(event.value()),
                        }
                    }
                    button { class: "btn btn-outline", type: "submit", "Check availability" }
                }
                match lending.cloned() {
                    None => rsx! { Loading {} },
                    Some(Err(message)) => rsx! { Notice { message } },
                    Some(Ok(availability)) => rsx! {
                        if !availability.isbn.is_empty() {
                            p { class: "small muted",
                                "Current provider results for ISBN {availability.isbn}."
                            }
                            if availability.options.is_empty() {
                                Empty {
                                    title: "No online reading match".to_owned(),
                                    description: "This ISBN has no matching reading option reported by Open Library. Check the book's catalog record for other editions.".to_owned()
                                }
                            } else {
                                div { class: "book-list",
                                    for (index, option) in availability.options.iter().enumerate() {
                                        article { class: "book-entry", key: "{index}",
                                            div { class: "book-description",
                                                strong { "{option.provider} · {option.status}" }
                                                span { class: "small muted",
                                                    "Edition match: {option.r#match}"
                                                }
                                                if !option.edition_id.is_empty() {
                                                    span { class: "small muted",
                                                        "Edition: {option.edition_id}"
                                                    }
                                                }
                                                if !option.provider_url.is_empty() {
                                                    a {
                                                        href: "{option.provider_url}",
                                                        target: "_blank",
                                                        rel: "noopener noreferrer",
                                                        "Open provider to read or borrow"
                                                    }
                                                }
                                            }
                                        }
                                    }
                                }
                            }
                            p { class: "small muted", "{availability.note}" }
                        }
                    }
                }
            }
        }
        section { class: "card",
            div { class: "card-header",
                h2 { class: "card-title", "Reading and acquisition requests" }
                p { class: "card-description",
                    "Private requests are not automatic purchases or media downloads. Fulfillment is recorded by the owner."
                }
            }
            div { class: "card-body",
                match wishes.cloned() {
                    None => rsx! { Loading {} },
                    Some(Err(message)) => rsx! { Notice { message } },
                    Some(Ok(records)) => rsx! {
                        if records.is_empty() {
                            Empty {
                                title: "No book requests".to_owned(),
                                description: "Add a book from catalog search to begin.".to_owned()
                            }
                        } else {
                            div { class: "book-list",
                                for record in records {
                                    article { class: "book-entry",
                                        div { class: "book-description",
                                            strong { "{record.title}" }
                                            span { class: "small muted", "{record.authors.join(\", \")}" }
                                            span { class: "small muted", "{record.format} · {record.status}" }
                                        }
                                        if record.status == "requested" {
                                            div { class: "book-actions",
                                                for (status, label) in [
                                                    ("fulfilled", "Mark obtained"),
                                                    ("cancelled", "Cancel")
                                                ] {
                                                    button { class: "btn btn-outline",
                                                        disabled: busy(),
                                                        onclick: {
                                                            let id = record.id.clone();
                                                            let revision = record.revision;
                                                            let token = csrf.clone();
                                                            move |_| {
                                                                let path = format!("/v1/books/requests/{id}");
                                                                let body = serde_json::json!({
                                                                    "status":status,
                                                                    "expected_revision":revision
                                                                });
                                                                busy.set(true);
                                                                error.set(String::new());
                                                                let token = token.clone();
                                                                spawn(async move {
                                                                    match api::write::<BookRequest,_>(
                                                                        Method::PUT,&path,&token,&body
                                                                    ).await {
                                                                        Ok(_) => { wishes.restart(); }
                                                                        Err(message) => error.set(message),
                                                                    }
                                                                    busy.set(false);
                                                                });
                                                            }
                                                        },
                                                        "{label}"
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
