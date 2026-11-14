mod activity;
mod api;
mod books;
mod calendar;
mod cognitive;
mod components;
mod compute_manage;
#[cfg(feature = "embedded-app")]
mod embedded_learning;
#[cfg(feature = "embedded-app")]
mod embedded_learning_attachments;
#[cfg(feature = "embedded-app")]
mod embedded_learning_graph;
#[cfg(feature = "embedded-app")]
mod embedded_learning_insights;
#[cfg(feature = "embedded-app")]
mod embedded_learning_workflows;
mod engineering;
mod engineering_create;
mod engineering_diff;
mod engineering_knowledge;
mod engineering_orchestration;
mod health_dashboard;
mod health_groceries;
mod health_tools;
mod knowledge;
mod learning_attachments;
mod learning_library;
mod learning_route;
#[cfg(feature = "embedded-app")]
mod mcp_app;
mod monitoring;
mod motivation;
mod pantry;
mod personal;
mod planner;
mod recipes;
mod tasks;
mod usda_nutrition;
mod views;
mod workflows;
mod workout_companion;
mod workout_planner;

use components::Icon;
use dioxus::prelude::*;

static CSS: Asset = asset!("/assets/main.css");

#[derive(Clone, Copy, Debug, PartialEq, Eq)]
enum Page {
    Overview,
    Projects,
    EngineeringRuns,
    Workflows,
    Knowledge,
    Compute,
    Software,
    Monitoring,
    Calendar,
    Planner,
    Tasks,
    Motivation,
    Learning,
    LearningLibrary,
    Health,
    ReactionTime,
    HealthMetrics,
    WorkoutPlans,
    HealthGroceries,
    WorkoutCompanion,
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
            Self::EngineeringRuns => "Engineering runs",
            Self::Workflows => "Workflows",
            Self::Knowledge => "Knowledge",
            Self::Compute => "Compute",
            Self::Software => "Software library",
            Self::Monitoring => "Monitoring",
            Self::Calendar => "Calendar",
            Self::Planner => "Weekly planner",
            Self::Tasks => "Tasks",
            Self::Motivation => "Motivation",
            Self::Learning => "Learning",
            Self::LearningLibrary => "Learning library",
            Self::Health => "Health",
            Self::ReactionTime => "Reaction time",
            Self::HealthMetrics => "Health metrics",
            Self::WorkoutPlans => "Workout plans",
            Self::HealthGroceries => "Health groceries",
            Self::WorkoutCompanion => "Workout companion",
            Self::Recipes => "Recipes",
            Self::Books => "Books",
            Self::Activity => "Activity",
            Self::Media => "Media",
            Self::Administration => "Administration",
        }
    }
}

const ENGINEERING: &[(Page, &str, &str)] = &[
    (Page::Overview, "Overview", "overview"),
    (Page::Projects, "Projects", "projects"),
    (Page::EngineeringRuns, "Engineering runs", "tasks"),
    (Page::Workflows, "Workflows", "activity"),
    (Page::Knowledge, "Knowledge", "books"),
    (Page::Compute, "Compute", "compute"),
    (Page::Software, "Software library", "software"),
];
const PERSONAL: &[(Page, &str, &str)] = &[
    (Page::Tasks, "Tasks", "tasks"),
    (Page::Motivation, "Motivation", "activity"),
    (Page::Calendar, "Calendar", "calendar"),
    (Page::Planner, "Weekly planner", "planner"),
    (Page::Learning, "Learning", "learning"),
    (Page::LearningLibrary, "Learning library", "books"),
    (Page::Health, "Health", "health"),
    (Page::ReactionTime, "Reaction time", "activity"),
    (Page::HealthMetrics, "Health metrics", "monitoring"),
    (Page::WorkoutPlans, "Workout plans", "health"),
    (Page::HealthGroceries, "Health groceries", "recipes"),
    (Page::WorkoutCompanion, "Workout companion", "activity"),
    (Page::Recipes, "Recipes", "recipes"),
    (Page::Books, "Books", "books"),
    (Page::Activity, "Activity", "activity"),
    (Page::Media, "Media", "media"),
];

#[cfg(not(feature = "embedded-app"))]
fn main() {
    dioxus::launch(App);
}

#[cfg(feature = "embedded-app")]
fn main() {
    dioxus::launch(mcp_app::Embedded);
}

#[allow(non_snake_case)]
fn App() -> Element {
    let mut page = use_signal(|| Page::Overview);
    let mut dark = use_signal(|| false);
    let mut mobile_open = use_signal(|| false);
    let mut session = use_resource(|| async { api::get::<api::Session>("/ui/session").await });
    let authenticated = matches!(
        session.cloned(),
        Some(Ok(api::Session {
            authenticated: true,
            ..
        }))
    );
    let csrf = session
        .cloned()
        .and_then(Result::ok)
        .and_then(|value| value.csrf_token)
        .unwrap_or_default();

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
                            div { class: "brand-mark", Icon { name: "shield".to_owned() } }
                            h1 { class: "card-title", "Access required" }
                            p { class: "card-description",
                                "Your Cloudflare Access identity must be verified to use the Homelab console."
                            }
                        }
                        div { class: "card-body",
                            if let Some(Err(error)) = session.cloned() {
                                components::Notice { message: error }
                            }
                            a { class: "btn btn-primary", href: "/", "Continue with Cloudflare Access" }
                            p { class: "login-footer",
                                "Privileged infrastructure operations require separate administrator authorization."
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
                        nav { aria_label: "Engineering",
                            div { class: "nav-group",
                                div { class: "nav-label", "Engineering" }
                                for &(destination,label,icon) in ENGINEERING {
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
                                a {
                                    class: "btn btn-outline btn-sm",
                                    href: "/cdn-cgi/access/logout",
                                    title: "End Cloudflare Access session",
                                    Icon { name: "logout".to_owned() }
                                    "Sign out"
                                }
                            }
                        }
                        main { class: "content", id: "content",
                            match page() {
                                Page::Overview => rsx! { views::Overview {} },
                                Page::Projects => rsx! { views::Projects {} },
                                Page::EngineeringRuns => rsx! { engineering::EngineeringRuns { csrf: csrf.clone() } },
                                Page::Workflows => rsx! { workflows::Workflows { csrf: csrf.clone() } },
                                Page::Knowledge => rsx! { knowledge::ProjectKnowledge {} },
                                Page::Compute => rsx! { views::Compute { csrf: csrf.clone() } },
                                Page::Software => rsx! { views::Software {} },
                                Page::Monitoring => rsx! { monitoring::Monitoring {} },
                                Page::Calendar => rsx! { calendar::Calendar {} },
                                Page::Planner => rsx! { planner::Planner {} },
                                Page::Tasks => rsx! { tasks::Tasks { csrf: csrf.clone() } },
                                Page::Motivation => rsx! { motivation::Motivation { csrf: csrf.clone() } },
                                Page::Learning => rsx! { personal::Learning { csrf: csrf.clone() } },
                                Page::LearningLibrary => rsx! { learning_library::LearningLibrary { csrf: csrf.clone() } },
                                Page::Health => rsx! { personal::Health { csrf: csrf.clone() } },
                                Page::ReactionTime => rsx! { cognitive::ReactionTime { csrf: csrf.clone() } },
                                Page::HealthMetrics => rsx! { health_dashboard::HealthDashboard { csrf: csrf.clone() } },
                                Page::WorkoutPlans => rsx! { workout_planner::WorkoutPlanner { csrf: csrf.clone() } },
                                Page::HealthGroceries => rsx! { health_groceries::HealthGroceries { csrf: csrf.clone() } },
                                Page::WorkoutCompanion => rsx! { workout_companion::WorkoutCompanion { csrf: csrf.clone() } },
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
