mod api;
mod components;
mod recipes;
mod tasks;
mod personal;
mod views;
mod monitoring;
mod calendar;
mod planner;
mod books;
mod activity;

use dioxus::prelude::*;
use components::Icon;

static CSS: Asset = asset!("/assets/main.css");

#[derive(Clone, Copy, Debug, PartialEq, Eq)]
enum Page {
    Overview,
    Projects,
    Compute,
    Software,
    Monitoring,
    Calendar,
    Planner,
    Tasks,
    Learning,
    Health,
    Recipes,
    Books,
    Activity,
    Media,
    Administration,
}

impl Page {
    fn label(self) -> &'static str {
        match self {
            Self::Overview => "Overview",
            Self::Projects => "Projects",
            Self::Compute => "Compute",
            Self::Software => "Software library",
            Self::Monitoring => "Monitoring",
            Self::Calendar => "Calendar",
            Self::Planner => "Weekly planner",
            Self::Tasks => "Tasks",
            Self::Learning => "Learning",
            Self::Health => "Health",
            Self::Recipes => "Recipes",
            Self::Books => "Books",
            Self::Activity => "Activity",
            Self::Media => "Media",
            Self::Administration => "Administration",
        }
    }
}

const WORKSPACE: &[(Page, &str, &str)] = &[
    (Page::Overview, "Overview", "overview"),
    (Page::Projects, "Projects", "projects"),
    (Page::Compute, "Compute", "compute"),
    (Page::Software, "Software library", "software"),
];
const PERSONAL: &[(Page, &str, &str)] = &[
    (Page::Tasks, "Tasks", "tasks"),
    (Page::Calendar, "Calendar", "calendar"),
    (Page::Planner, "Weekly planner", "planner"),
    (Page::Learning, "Learning", "learning"),
    (Page::Health, "Health", "health"),
    (Page::Recipes, "Recipes", "recipes"),
    (Page::Books, "Books", "books"),
    (Page::Activity, "Activity", "activity"),
    (Page::Media, "Media", "media"),
];

fn main() {
    dioxus::launch(App);
}

#[allow(non_snake_case)]
fn App() -> Element {
    let mut page = use_signal(|| Page::Overview);
    let mut dark = use_signal(|| false);
    let mut mobile_open = use_signal(|| false);
    let mut passphrase = use_signal(String::new);
    let mut login_error = use_signal(String::new);
    let mut session = use_resource(|| async { api::get::<api::Session>("/ui/session").await });
    let authenticated = matches!(session.cloned(), Some(Ok(api::Session { authenticated: true, .. })));
    let csrf = session
        .cloned()
        .and_then(Result::ok)
        .and_then(|value| value.csrf_token)
        .unwrap_or_default();
    let logout_csrf = csrf.clone();

    rsx! {
        document::Stylesheet { href: CSS }
        div { class: if dark() {"app dark"} else {"app"},
            a { class: "skip-link", href: "#content", "Skip to content" }
            if session.cloned().is_none() {
                div { class: "login-layout", role: "status", "Connecting to console…" }
            } else if !authenticated {
                div { class: "login-layout",
                    section { class: "card login-card",
                        div { class: "card-header",
                            div { class: "brand-mark", Icon { name: "compute".to_owned() } }
                            h1 { class: "card-title", "Homelab Console" }
                            p { class: "card-description",
                                "Sign in to view your computing and personal workspaces."
                            }
                        }
                        div { class: "card-body",
                            if let Some(Err(error)) = session.cloned() {
                                components::Notice { message: error }
                            }
                            if !login_error().is_empty() {
                                components::Notice { message: login_error() }
                            }
                            form {
                                class: "login-form",
                                onsubmit: move |event| {
                                    event.prevent_default();
                                    let password = passphrase();
                                    passphrase.set(String::new());
                                    spawn(async move {
                                        match api::login(&password).await {
                                            Ok(()) => {
                                                login_error.set(String::new());
                                                session.restart();
                                            }
                                            Err(message) => login_error.set(message),
                                        }
                                    });
                                },
                                div { class: "form-row",
                                    label { class: "label", for: "owner-passphrase", "Owner passphrase" }
                                    input {
                                        id: "owner-passphrase",
                                        class: "field",
                                        type: "password",
                                        autocomplete: "current-password",
                                        required: true,
                                        value: "{passphrase}",
                                        oninput: move |event| passphrase.set(event.value()),
                                    }
                                }
                                button { class: "btn btn-primary", type: "submit", "Sign in" }
                            }
                            p { class: "login-footer",
                                "Authentication stays within the trusted homelab service. No MCP token is stored in this browser."
                            }
                        }
                    }
                }
            } else {
                div { class: "shell",
                    if mobile_open() {
                        button {
                            class: "mobile-overlay",
                            aria_label: "Close navigation",
                            onclick: move |_| mobile_open.set(false),
                        }
                    }
                    aside {
                        class: if mobile_open() {"sidebar is-open"} else {"sidebar"},
                        aria_label: "Main navigation",
                        div { class: "brand",
                            div { class: "brand-mark",
                                Icon { name: "compute".to_owned() }
                            }
                            div {
                                span { class: "brand-name", "Homelab" }
                                span { class: "brand-caption", "Personal computing" }
                            }
                        }
                        nav { aria_label: "Workspace",
                            div { class: "nav-group",
                                div { class: "nav-label", "Workspace" }
                                for &(destination,label,icon) in WORKSPACE {
                                    button {
                                        key: "{label}",
                                        class: "nav-item",
                                        aria_current: if page()==destination {"page"} else {"false"},
                                        onclick: move |_| { page.set(destination); mobile_open.set(false); },
                                        Icon { name: icon.to_owned() }
                                        span { "{label}" }
                                    }
                                }
                            }
                        }
                        nav { aria_label: "Personal",
                            div { class: "nav-group",
                                div { class: "nav-label", "Personal" }
                                for &(destination,label,icon) in PERSONAL {
                                    button {
                                        key: "{label}",
                                        class: "nav-item",
                                        aria_current: if page()==destination {"page"} else {"false"},
                                        onclick: move |_| { page.set(destination); mobile_open.set(false); },
                                        Icon { name: icon.to_owned() }
                                        span { "{label}" }
                                    }
                                }
                            }
                        }
                        nav { aria_label: "System",
                            div { class: "nav-group",
                                div { class: "nav-label", "System" }
                                button {
                                    class: "nav-item",
                                    aria_current: if page()==Page::Monitoring {"page"} else {"false"},
                                    onclick: move |_| { page.set(Page::Monitoring); mobile_open.set(false); },
                                    Icon { name: "monitoring".to_owned() }
                                    span { "Monitoring" }
                                }
                                button {
                                    class: "nav-item",
                                    aria_current: if page()==Page::Administration {"page"} else {"false"},
                                    onclick: move |_| { page.set(Page::Administration); mobile_open.set(false); },
                                    Icon { name: "admin".to_owned() }
                                    span { "Administration" }
                                }
                            }
                        }
                        div { class: "sidebar-footer",
                            div { class: "user-avatar", "O" }
                            div {
                                span { class: "user-label", "Owner" }
                                span { class: "user-meta", "Personal recipes · administration locked" }
                            }
                        }
                    }
                    div { class: "workspace",
                        header { class: "topbar",
                            div { class: "inline-row",
                                button {
                                    class: "btn btn-icon btn-ghost mobile-nav-toggle",
                                    aria_label: "Open navigation",
                                    onclick: move |_| mobile_open.set(true),
                                    Icon { name: "menu".to_owned() }
                                }
                                div { class: "crumbs",
                                    span { "Homelab" }
                                    Icon { name: "chevron".to_owned() }
                                    span { class: "crumb-active", "{page().label()}" }
                                }
                            }
                            div { class: "topbar-actions",
                                button {
                                    class: "btn btn-icon btn-ghost",
                                    aria_label: "Toggle color scheme",
                                    title: "Toggle color scheme",
                                    onclick: move |_| dark.set(!dark()),
                                    if dark() {
                                        Icon { name: "sun".to_owned() }
                                    } else {
                                        Icon { name: "moon".to_owned() }
                                    }
                                }
                                button {
                                    class: "btn btn-outline btn-sm",
                                    onclick: move |_| {
                                        let csrf = logout_csrf.clone();
                                        spawn(async move {
                                            match api::logout(&csrf).await {
                                                Ok(()) => session.restart(),
                                                Err(message) => login_error.set(message),
                                            }
                                        });
                                    },
                                    Icon { name: "logout".to_owned() }
                                    "Sign out"
                                }
                            }
                        }
                        main { class: "content", id: "content",
                            match page() {
                                Page::Overview => rsx! { views::Overview {} },
                                Page::Projects => rsx! { views::Projects {} },
                                Page::Compute => rsx! { views::Compute {} },
                                Page::Software => rsx! { views::Software {} },
                                Page::Monitoring => rsx! { monitoring::Monitoring {} },
                                Page::Calendar => rsx! { calendar::Calendar {} },
                                Page::Planner => rsx! { planner::Planner {} },
                                Page::Tasks => rsx! { tasks::Tasks { csrf: csrf.clone() } },
                                Page::Learning => rsx! { personal::Learning { csrf: csrf.clone() } },
                                Page::Health => rsx! { personal::Health { csrf: csrf.clone() } },
                                Page::Recipes => rsx! { recipes::Recipes { csrf: csrf.clone() } },
                                Page::Books => rsx! { books::Books { csrf: csrf.clone() } },
                                Page::Activity => rsx! { activity::Activity {} },
                                Page::Media => rsx! { views::Media {} },
                                Page::Administration => rsx! { views::Administration {} },
                            }
                        }
                    }
                }
            }
        }
    }
}
