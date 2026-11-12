use dioxus::prelude::*;

#[component]
pub fn Icon(name: String) -> Element {
    let path = match name.as_str() {
        "overview" => "M3 3h7v7H3z M14 3h7v7h-7z M14 14h7v7h-7z M3 14h7v7H3z",
        "projects" => "M3 7a2 2 0 0 1 2-2h4l2 2h8a2 2 0 0 1 2 2v10a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2z",
        "compute" => {
            "M6 6h12v12H6z M9 9h6v6H9z M9 2v4 M15 2v4 M9 18v4 M15 18v4 M2 9h4 M2 15h4 M18 9h4 M18 15h4"
        }
        "software" => "M12 2 3 7l9 5 9-5-9-5z M3 12l9 5 9-5 M3 17l9 5 9-5",
        "tasks" => "M4 4h16v16H4z M8 12l3 3 5-6",
        "calendar" => "M3 6h18v15H3z M7 3v6 M17 3v6 M3 11h18 M8 15h3 M13 15h3",
        "planner" => "M4 4h16v16H4z M8 9h8 M8 13h5 M8 17h3 M17 13l2 2 3-4",
        "learning" => "M3 19V5a2 2 0 0 1 2-2h14v18H5a2 2 0 0 1 0-4h14 M7 7h8",
        "health" => {
            "M20.8 4.6a5.5 5.5 0 0 0-7.8 0L12 5.7l-1.1-1.1a5.5 5.5 0 0 0-7.8 7.8L12 21l8.8-8.6a5.5 5.5 0 0 0 0-7.8z"
        }
        "media" => "M3 3h18v18H3z M7 3v18 M17 3v18 M3 8h4 M17 8h4 M3 16h4 M17 16h4",
        "recipes" => {
            "M4 19.5A2.5 2.5 0 0 1 6.5 17H20 M6.5 2H20v20H6.5A2.5 2.5 0 0 1 4 19.5v-15A2.5 2.5 0 0 1 6.5 2z M9 7h7 M9 11h7"
        }
        "books" => "M3 5a2 2 0 0 1 2-2h6l2 2h8v16H5a2 2 0 0 0 0-4h16 M5 3v14",
        "activity" => "M2 12h5l3-7 4 14 3-7h5",
        "monitoring" => "M2 20h20 M4 14l5-5 4 3 7-8 M4 4v16 M20 4v16",
        "admin" => {
            "M12 2l2.2 2.2 3.1-.3.4 3.1 2.3 2.2-1.4 2.8 1.4 2.8-2.3 2.2-.4 3.1-3.1-.3L12 22l-2.2-2.2-3.1.3-.4-3.1L4 14.8 5.4 12 4 9.2l2.3-2.2.4-3.1 3.1.3z M12 9a3 3 0 1 0 0 6 3 3 0 0 0 0-6z"
        }
        "refresh" => "M20 7v5h-5 M4 17v-5h5 M5.7 9A7 7 0 0 1 18 7l2 5 M4 12l2 5a7 7 0 0 0 12.3-2",
        "sun" => {
            "M12 4V2 M12 22v-2 M4 12H2 M22 12h-2 M5.7 5.7 4.3 4.3 M19.7 19.7l-1.4-1.4 M5.7 18.3l-1.4 1.4 M19.7 4.3l-1.4 1.4 M16 12a4 4 0 1 1-8 0 4 4 0 0 1 8 0"
        }
        "moon" => "M20 15.4A8 8 0 0 1 8.6 4a8.5 8.5 0 1 0 11.4 11.4z",
        "logout" => "M9 21H5a2 2 0 0 1-2-2V5a2 2 0 0 1 2-2h4 M16 17l5-5-5-5 M21 12H9",
        "menu" => "M4 6h16 M4 12h16 M4 18h16",
        "chevron" => "M9 18l6-6-6-6",
        "server" => "M4 4h16v7H4z M4 13h16v7H4z M7 8h.01 M7 17h.01",
        "shield" => "M12 2 3 6v6c0 5 3.4 8.4 9 10 5.6-1.6 9-5 9-10V6z M9 12l2 2 4-4",
        "alert" => "M12 2l10 18H2z M12 9v4 M12 17h.01",
        _ => "M4 4h16v16H4z",
    };
    rsx! {
        svg {
            width: "16",
            height: "16",
            view_box: "0 0 24 24",
            fill: "none",
            stroke: "currentColor",
            stroke_width: "2",
            stroke_linecap: "round",
            stroke_linejoin: "round",
            path { d: "{path}" }
        }
    }
}

#[component]
pub fn Heading(title: String, description: String, children: Element) -> Element {
    rsx! {
        div { class: "page-heading",
            div {
                h1 { "{title}" }
                p { "{description}" }
            }
            div { class: "page-actions", {children} }
        }
    }
}

#[component]
pub fn Empty(title: String, description: String) -> Element {
    rsx! {
        div { class: "empty",
            h3 { "{title}" }
            p { "{description}" }
        }
    }
}

#[component]
pub fn Notice(message: String) -> Element {
    rsx! {
        div { class: "alert error", role: "alert",
            Icon { name: "alert".to_owned() }
            span { "{message}" }
        }
    }
}

#[component]
pub fn Badge(text: String) -> Element {
    rsx! {
        span { class: "badge muted", "{text}" }
    }
}

#[component]
pub fn Card(title: String, description: String, children: Element) -> Element {
    rsx! {
        section { class: "card",
            div { class: "card-header",
                h2 { class: "card-title", "{title}" }
                p { class: "card-description", "{description}" }
            }
            div { class: "card-body", {children} }
        }
    }
}

#[component]
pub fn Loading() -> Element {
    rsx! {
        div { class: "empty", role: "status",
            p { "Loading data…" }
        }
    }
}
