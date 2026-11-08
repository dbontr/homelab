use dioxus::prelude::*;
use serde::Deserialize;
use std::collections::HashMap;

use crate::api;
use crate::components::{Empty, Heading, Icon, Loading, Notice};

#[derive(Clone, Debug, Deserialize, PartialEq)]
pub struct Point {
    pub at: String,
    pub value: f64,
}
#[derive(Clone, Debug, Deserialize, PartialEq)]
pub struct Series {
    #[serde(default)]
    pub labels: HashMap<String, String>,
    #[serde(default)]
    pub points: Vec<Point>,
}
#[derive(Clone, Debug, Deserialize, PartialEq)]
pub struct Measurement {
    pub key: String,
    pub label: String,
    pub unit: String,
    #[serde(default)]
    pub series: Vec<Series>,
}
#[derive(Clone, Debug, Deserialize, PartialEq)]
pub struct Snapshot {
    pub at: String,
    #[serde(default)]
    pub sources: HashMap<String, bool>,
    #[serde(default)]
    pub metrics: Vec<Measurement>,
}
#[derive(Clone, Debug, Deserialize, PartialEq)]
pub struct Target {
    pub job: String,
    pub instance: String,
    pub health: String,
    #[serde(default)]
    pub last_scrape: String,
    #[serde(default)]
    pub last_error: String,
}
#[derive(Clone, Debug, Deserialize, PartialEq)]
pub struct Alert {
    pub name: String,
    pub state: String,
    pub severity: String,
    pub summary: String,
    #[serde(default)]
    pub active_at: String,
}
#[derive(Clone, Debug, Deserialize, PartialEq)]
pub struct LogEntry {
    pub at: String,
    #[serde(default)]
    pub host: String,
    #[serde(default)]
    pub unit: String,
    pub line: String,
}

#[derive(Clone, Copy, PartialEq, Eq)]
enum Section {
    Overview,
    Charts,
    Targets,
    Alerts,
    Logs,
}

const METRICS: &[(&str, &str)] = &[
    ("cpu", "CPU"), ("memory", "Memory"), ("disk", "Root disk"),
    ("network-rx", "Network received"), ("network-tx", "Network sent"),
    ("disk-read", "Disk reads"), ("disk-write", "Disk writes"),
    ("gpu", "GPU utilization"), ("gpu-vram", "GPU memory"),
    ("gpu-temp", "GPU temperature"), ("gpu-age", "GPU freshness"),
    ("availability", "Collector availability"),
];

fn time_label(value: &str) -> String {
    value.get(..19).unwrap_or(value).replace('T', " ")
}

fn series_name(series: &Series) -> String {
    series.labels.get("node")
        .or_else(|| series.labels.get("instance"))
        .or_else(|| series.labels.get("job"))
        .cloned()
        .unwrap_or_else(|| "Unlabelled series".to_owned())
}

fn value_label(value: f64, unit: &str) -> String {
    if unit == "up/down" {
        if value == 1.0 { return "Up".to_owned(); }
        if value == 0.0 { return "Down".to_owned(); }
        return "Unknown".to_owned();
    }
    if unit == "B/s" {
        return if value.abs() >= 1_048_576.0 {
            format!("{:.1} MiB/s", value / 1_048_576.0)
        } else if value.abs() >= 1024.0 {
            format!("{:.1} KiB/s", value / 1024.0)
        } else { format!("{value:.0} B/s") };
    }
    format!("{value:.1} {unit}")
}

fn line_path(points: &[Point]) -> String {
    if points.is_empty() { return String::new(); }
    let (min, max) = points.iter().fold((f64::INFINITY, f64::NEG_INFINITY), |(low, high), p| {
        (low.min(p.value), high.max(p.value))
    });
    let range = (max - min).max(0.000001);
    let denom = points.len().saturating_sub(1).max(1) as f64;
    points.iter().enumerate().map(|(i, point)| {
        let x = 8.0 + (i as f64 / denom) * 684.0;
        let y = 141.0 - (point.value - min) / range * 122.0;
        format!("{}{x:.1},{y:.1}", if i == 0 {"M"} else {" L"})
    }).collect::<Vec<_>>().join("")
}

#[component]
fn SnapshotCard(measurement: Measurement) -> Element {
    rsx! {
        section { class: "card monitoring-metric",
            div { class: "metric-title", "{measurement.label}" }
            if measurement.series.is_empty() {
                div { class: "metric-value", "—" }
                p { class: "small muted", "No current measurement" }
            } else {
                div { class: "monitoring-readings",
                    for series in &measurement.series {
                        if let Some(point) = series.points.last() {
                            div { class: "monitoring-reading",
                                div { class: "metric-value", {value_label(point.value, &measurement.unit)} }
                                span { class: "small muted", {series_name(series)} }
                                span { class: "small muted", "Sampled {time_label(&point.at)} UTC" }
                            }
                        }
                    }
                }
            }
        }
    }
}

#[component]
fn HistoryChart(measurement: Measurement) -> Element {
    rsx! {
        section { class: "card monitoring-history",
            div { class: "card-header",
                h2 { class: "card-title", "{measurement.label}" }
                p { class: "card-description", "Observed samples in the selected time range." }
            }
            div { class: "card-body",
                if measurement.series.is_empty() {
                    Empty {
                        title: "No samples".to_owned(),
                        description: "The collector returned no numeric samples for this interval.".to_owned()
                    }
                } else {
                    div { class: "monitoring-charts",
                        for series in &measurement.series {
                            div { class: "monitoring-chart",
                                div { class: "monitoring-chart-heading",
                                    strong { {series_name(series)} }
                                    if let Some(latest) = series.points.last() {
                                        span { class: "tabular muted", {value_label(latest.value, &measurement.unit)} }
                                    }
                                }
                                svg {
                                    class: "monitoring-sparkline",
                                    role: "img",
                                    aria_label: "Recorded history for {series_name(series)}",
                                    viewBox: "0 0 700 160",
                                    preserve_aspect_ratio: "none",
                                    line { x1: "8", y1: "141", x2: "692", y2: "141", stroke: "currentColor", stroke_opacity: "0.15" }
                                    path { d: "{line_path(&series.points)}", fill: "none", stroke: "currentColor",
                                        stroke_width: "2.25", stroke_linecap: "round", stroke_linejoin: "round" }
                                }
                                div { class: "monitoring-chart-scale",
                                    span { {series.points.first().map(|p| time_label(&p.at)).unwrap_or_default()} }
                                    span { {series.points.last().map(|p| time_label(&p.at)).unwrap_or_default()} }
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
pub fn Monitoring() -> Element {
    let mut section = use_signal(|| Section::Overview);
    let mut metric = use_signal(|| "cpu".to_owned());
    let mut window = use_signal(|| "6h".to_owned());
    let mut log_window = use_signal(|| "1h".to_owned());
    let mut search = use_signal(String::new);

    let mut overview = use_resource(|| async {
        api::get::<Snapshot>("/v1/monitoring/summary").await
    });
    let mut targets = use_resource(|| async {
        api::get::<Vec<Target>>("/v1/monitoring/targets").await
    });
    let mut alerts = use_resource(|| async {
        api::get::<Vec<Alert>>("/v1/monitoring/alerts").await
    });
    let mut history = use_resource(move || {
        let key = metric();
        let interval = window();
        async move {
            api::get::<Measurement>(&format!("/v1/monitoring/series?metric={key}&range={interval}")).await
        }
    });
    let mut logs = use_resource(move || {
        let interval = log_window();
        async move {
            api::get::<Vec<LogEntry>>(&format!("/v1/monitoring/logs?range={interval}&limit=150")).await
        }
    });

    let query = search().to_lowercase();
    let filtered_logs: Vec<LogEntry> = logs.cloned()
        .and_then(Result::ok).unwrap_or_default()
        .into_iter()
        .filter(|entry| query.is_empty()
            || entry.line.to_lowercase().contains(&query)
            || entry.unit.to_lowercase().contains(&query))
        .collect();

    rsx! {
        Heading {
            title: "Monitoring".to_owned(),
            description: "Live measurements, collection health, alerts and system journals.".to_owned(),
            button { class: "btn btn-outline",
                onclick: move |_| {
                    overview.restart(); targets.restart(); alerts.restart(); history.restart(); logs.restart();
                },
                Icon { name: "refresh".to_owned() }
                "Refresh"
            }
        }
        div { class: "tabs monitoring-tabs", role: "tablist", aria_label: "Monitoring views",
            for (tab, label) in [
                (Section::Overview,"Overview"), (Section::Charts,"History"),
                (Section::Targets,"Collectors"), (Section::Alerts,"Alerts"), (Section::Logs,"Logs")
            ] {
                button {
                    class: if section()==tab {"tab active"} else {"tab"},
                    role: "tab", aria_selected: section()==tab,
                    onclick: move |_| section.set(tab), "{label}"
                }
            }
        }
        match section() {
            Section::Overview => rsx! {
                match overview.cloned() {
                    None => rsx! { Loading {} },
                    Some(Err(message)) => rsx! { Notice { message } },
                    Some(Ok(snapshot)) => rsx! {
                        p { class: "small muted monitoring-timestamp",
                            "Snapshot {time_label(&snapshot.at)} UTC. Missing telemetry is unknown, not zero."
                        }
                        div { class: "metric-grid monitoring-metric-grid",
                            for reading in snapshot.metrics {
                                SnapshotCard { measurement: reading }
                            }
                        }
                        div { class: "section-grid",
                            section { class: "card",
                                div { class: "card-header",
                                    h2 { class: "card-title", "Collector status" }
                                    p { class: "card-description", "The data collectors that Prometheus is scraping." }
                                }
                                div { class: "card-body",
                                    match targets.cloned() {
                                        None => rsx! { Loading {} },
                                        Some(Err(message)) => rsx! { Notice { message } },
                                        Some(Ok(items)) => rsx! {
                                            p { class: "monitoring-compact",
                                                "{items.iter().filter(|v| v.health == \"up\").count()} reporting up; {items.iter().filter(|v| v.health != \"up\").count()} not reporting up."
                                            }
                                            button { class: "btn btn-outline", onclick: move |_| section.set(Section::Targets),
                                                "Inspect collectors" }
                                        }
                                    }
                                }
                            }
                            section { class: "card",
                                div { class: "card-header",
                                    h2 { class: "card-title", "Active alerts" }
                                    p { class: "card-description", "Firing and pending rules reported by Prometheus." }
                                }
                                div { class: "card-body",
                                    match alerts.cloned() {
                                        None => rsx! { Loading {} },
                                        Some(Err(message)) => rsx! { Notice { message } },
                                        Some(Ok(items)) => rsx! {
                                            p { class: "monitoring-compact",
                                                "{items.iter().filter(|v| v.state == \"firing\").count()} firing; {items.iter().filter(|v| v.state == \"pending\").count()} pending."
                                            }
                                            button { class: "btn btn-outline", onclick: move |_| section.set(Section::Alerts),
                                                "Inspect alerts" }
                                        }
                                    }
                                }
                            }
                        }
                    }
                }
            },
            Section::Charts => rsx! {
                section { class: "monitoring-controls",
                    label { class: "form-row", "Metric",
                        select {
                            class: "field",
                            value: "{metric}",
                            onchange: move |event| metric.set(event.value()),
                            for &(key, title) in METRICS {
                                option { value: "{key}", "{title}" }
                            }
                        }
                    }
                    label { class: "form-row", "Time range",
                        select {
                            class: "field",
                            value: "{window}",
                            onchange: move |event| window.set(event.value()),
                            option { value: "1h", "1 hour" }
                            option { value: "6h", "6 hours" }
                            option { value: "24h", "24 hours" }
                            option { value: "7d", "7 days" }
                        }
                    }
                }
                match history.cloned() {
                    None => rsx! { Loading {} },
                    Some(Err(message)) => rsx! { Notice { message } },
                    Some(Ok(measurement)) => rsx! { HistoryChart { measurement } }
                }
            },
            Section::Targets => rsx! {
                section { class: "card",
                    div { class: "card-header",
                        h2 { class: "card-title", "Scrape targets" }
                        p { class: "card-description", "Collector state and last successful inspection; no inventory-derived health assumptions." }
                    }
                    div { class: "card-body",
                        match targets.cloned() {
                            None => rsx! { Loading {} },
                            Some(Err(message)) => rsx! { Notice { message } },
                            Some(Ok(items)) => rsx! {
                                if items.is_empty() {
                                    Empty { title: "No targets".to_owned(),
                                        description: "Prometheus did not return any active targets.".to_owned() }
                                } else {
                                    div { class: "monitoring-table-wrap",
                                        table { class: "table",
                                            thead { tr { th { "Job" } th { "Instance" } th { "Status" } th { "Last scrape" } } }
                                            tbody {
                                                for target in items {
                                                    tr {
                                                        td { "{target.job}" }
                                                        td { class: "mono", "{target.instance}" }
                                                        td {
                                                            span { class: if target.health=="up" {"badge monitoring-up"} else {"badge monitoring-down"},
                                                                "{target.health}" }
                                                            if !target.last_error.is_empty() {
                                                                p { class: "small muted", "{target.last_error}" }
                                                            }
                                                        }
                                                        td { class: "small muted", {time_label(&target.last_scrape)} }
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
            },
            Section::Alerts => rsx! {
                section { class: "card",
                    div { class: "card-header",
                        h2 { class: "card-title", "Alert rules" }
                        p { class: "card-description", "Active and pending alerts supplied by Prometheus; absence of alerts does not certify health." }
                    }
                    div { class: "card-body",
                        match alerts.cloned() {
                            None => rsx! { Loading {} },
                            Some(Err(message)) => rsx! { Notice { message } },
                            Some(Ok(items)) => rsx! {
                                if items.is_empty() {
                                    Empty { title: "No active alerts".to_owned(),
                                        description: "Prometheus returned no firing or pending alerts.".to_owned() }
                                } else {
                                    div { class: "monitoring-alerts",
                                        for alert in items {
                                            article { class: "monitoring-alert",
                                                div { class: "inline-row",
                                                    strong { "{alert.name}" }
                                                    span { class: "badge", "{alert.severity}" }
                                                    span { class: if alert.state=="firing" {"badge monitoring-down"} else {"badge muted"}, "{alert.state}" }
                                                }
                                                p { "{alert.summary}" }
                                                span { class: "small muted", "Active from {time_label(&alert.active_at)} UTC" }
                                            }
                                        }
                                    }
                                }
                            }
                        }
                    }
                }
            },
            Section::Logs => rsx! {
                section { class: "card",
                    div { class: "card-header",
                        h2 { class: "card-title", "System journal" }
                        p { class: "card-description",
                            "Bounded read-only journal entries from Loki. Search filters the downloaded entries locally." }
                    }
                    div { class: "card-body",
                        div { class: "monitoring-controls",
                            label { class: "form-row", "Find in loaded entries",
                                input { class: "field", value: "{search}", placeholder: "Filter message or unit",
                                    oninput: move |event| search.set(event.value()) }
                            }
                            label { class: "form-row", "Time range",
                                select { class: "field", value: "{log_window}",
                                    onchange: move |event| log_window.set(event.value()),
                                    option { value: "1h", "1 hour" }
                                    option { value: "6h", "6 hours" }
                                    option { value: "24h", "24 hours" }
                                }
                            }
                        }
                        match logs.cloned() {
                            None => rsx! { Loading {} },
                            Some(Err(message)) => rsx! { Notice { message } },
                            Some(Ok(_)) => rsx! {
                                if filtered_logs.is_empty() {
                                    Empty { title: "No matching entries".to_owned(),
                                        description: "Try another time range or a different filter.".to_owned() }
                                } else {
                                    div { class: "monitoring-logs",
                                        for entry in filtered_logs {
                                            article { class: "monitoring-log",
                                                div { class: "inline-row",
                                                    time { class: "small muted tabular", "{time_label(&entry.at)}" }
                                                    if !entry.host.is_empty() { span { class: "badge muted", "{entry.host}" } }
                                                    if !entry.unit.is_empty() { span { class: "badge muted", "{entry.unit}" } }
                                                }
                                                pre { "{entry.line}" }
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
