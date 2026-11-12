use std::collections::BTreeSet;

use dioxus::prelude::*;
use serde::Deserialize;
use serde_json::{Value, json};

use crate::api;
use crate::components::{Card, Empty, Heading, Loading, Notice};
use crate::health_tools::{self, Tool};

#[derive(Clone, Debug, Default, PartialEq, Deserialize)]
#[serde(default)]
struct MetricPoint {
    at: String,
    value: f64,
    unit: String,
    source: String,
    method: String,
}

#[derive(Clone, Debug, Default, PartialEq, Deserialize)]
#[serde(default)]
struct MetricSeries {
    points: Vec<MetricPoint>,
}

#[derive(Clone, Debug, PartialEq)]
struct Filter {
    domain: String,
    metric: String,
    start: String,
    end: String,
}

fn utc_day(days_ago: f64) -> String {
    let date = js_sys::Date::new_0();
    date.set_time(js_sys::Date::now() - days_ago * 86_400_000.0);
    date.to_iso_string()
        .as_string()
        .and_then(|value| value.get(..10).map(str::to_owned))
        .unwrap_or_default()
}

fn valid_metric(metric: &str) -> bool {
    !metric.is_empty()
        && metric.len() <= 64
        && metric.starts_with(|ch: char| ch.is_ascii_lowercase())
        && metric
            .bytes()
            .all(|b| b.is_ascii_lowercase() || b.is_ascii_digit() || b == b'_')
}

#[derive(Clone)]
struct ChartDot {
    x: f64,
    y: f64,
    label: String,
}

// Actual observation timestamps determine the horizontal coordinate.
// Missing dates are never interpolated, and incompatible units are rejected.
fn chart(points: &[MetricPoint]) -> Result<(String, Vec<ChartDot>), &'static str> {
    if points.is_empty() {
        return Err("No observations in this interval");
    }
    let unit = &points[0].unit;
    if points.iter().any(|point| point.unit != *unit) {
        return Err("Mixed units: select a series with consistent units");
    }
    let values = points
        .iter()
        .filter_map(|point| {
            let timestamp = js_sys::Date::parse(&point.at);
            (timestamp.is_finite() && point.value.is_finite()).then_some((timestamp, point))
        })
        .collect::<Vec<_>>();
    if values.is_empty() {
        return Err("No plottable observations");
    }
    let start = values
        .iter()
        .map(|(time, _)| *time)
        .fold(f64::INFINITY, f64::min);
    let end = values
        .iter()
        .map(|(time, _)| *time)
        .fold(f64::NEG_INFINITY, f64::max);
    let low = values
        .iter()
        .map(|(_, point)| point.value)
        .fold(f64::INFINITY, f64::min);
    let high = values
        .iter()
        .map(|(_, point)| point.value)
        .fold(f64::NEG_INFINITY, f64::max);
    let padding = if high == low {
        low.abs().max(1.0) * 0.1
    } else {
        0.0
    };
    let span_x = (end - start).max(1.0);
    let span_y = (high - low + padding * 2.0).max(1.0e-9);
    let normalized = values
        .iter()
        .map(|(time, point)| {
            let x = 35.0 + (time - start) / span_x * 730.0;
            let y = 175.0 - (point.value - low + padding) / span_y * 145.0;
            (x, y, point)
        })
        .collect::<Vec<_>>();
    let line = normalized
        .iter()
        .enumerate()
        .map(|(i, (x, y, _))| format!("{}{x:.2},{y:.2}", if i == 0 { "M" } else { " L" }))
        .collect::<String>();
    let stride = normalized.len().div_ceil(100).max(1);
    let dots = normalized
        .iter()
        .enumerate()
        .filter(|(i, _)| i % stride == 0 || i + 1 == normalized.len())
        .map(|(_, (x, y, point))| ChartDot {
            x: *x,
            y: *y,
            label: format!(
                "{}: {} {} ({}, {})",
                point.at, point.value, point.unit, point.method, point.source
            ),
        })
        .collect();
    Ok((line, dots))
}

struct NutrientRow {
    name: String,
    logged: String,
    target: String,
    coverage: &'static str,
}

fn nutrient_rows(nutrition: &Value, goals: &Value) -> Vec<NutrientRow> {
    let observed = nutrition.get("units_and_totals").and_then(Value::as_object);
    let targets = goals.as_object();
    let names = observed
        .into_iter()
        .flat_map(|map| map.keys())
        .chain(targets.into_iter().flat_map(|map| map.keys()))
        .cloned()
        .collect::<BTreeSet<_>>();
    names
        .into_iter()
        .map(|name| {
            let recorded = observed
                .and_then(|map| map.get(&name))
                .and_then(Value::as_object);
            let logged = recorded
                .map(|units| {
                    units
                        .iter()
                        .map(|(unit, value)| {
                            value
                                .as_f64()
                                .map(|number| format!("{number:.2} {unit}"))
                                .unwrap_or_else(|| "Unknown".to_owned())
                        })
                        .collect::<Vec<_>>()
                        .join(", ")
                })
                .unwrap_or_else(|| "Unknown".to_owned());
            let target = targets.and_then(|map| map.get(&name));
            let target_label = target
                .map(|goal| {
                    let quantity = goal.get("value").map(Value::to_string).unwrap_or_default();
                    let unit = goal.get("unit").and_then(Value::as_str).unwrap_or("");
                    format!("{quantity} {unit}")
                })
                .unwrap_or_else(|| "Not set".to_owned());
            NutrientRow {
                name,
                logged,
                target: target_label,
                coverage: if recorded.is_some() {
                    "Logged, may be incomplete"
                } else {
                    "Missing"
                },
            }
        })
        .collect()
}

#[component]
pub fn HealthDashboard(csrf: String) -> Element {
    let csrf = use_signal(|| csrf);
    let mut domain = use_signal(|| "biometric".to_owned());
    let mut metric = use_signal(|| "weight".to_owned());
    let mut start = use_signal(|| utc_day(30.0));
    let mut end = use_signal(|| utc_day(0.0));
    let mut day = use_signal(|| utc_day(0.0));
    let mut chosen = use_signal(|| Filter {
        domain: "biometric".to_owned(),
        metric: "weight".to_owned(),
        start: utc_day(30.0),
        end: utc_day(0.0),
    });
    let mut goal_metric = use_signal(String::new);
    let mut goal_value = use_signal(String::new);
    let mut goal_unit = use_signal(String::new);
    let mut busy = use_signal(|| false);
    let mut error = use_signal(String::new);
    let mut status = use_signal(String::new);

    let mut series = use_resource(move || {
        let filter = chosen();
        let token = csrf();
        async move {
            health_tools::call::<MetricSeries>(
                &token,
                Tool::MetricSeries,
                json!({
                    "domain": filter.domain, "metric": filter.metric,
                    "start_date": filter.start, "end_date": filter.end, "limit": 10000,
                }),
            )
            .await
        }
    });
    let mut nutrition = use_resource(move || {
        let selected_day = day();
        let token = csrf();
        async move {
            health_tools::call::<Value>(
                &token,
                Tool::NutrientDaily,
                json!({
                    "date": selected_day,
                }),
            )
            .await
        }
    });
    let mut goals = use_resource(|| async { api::get::<Value>("/v1/health/goals").await });

    rsx! {
        Heading {
            title: "Health metrics".to_owned(),
            description: "Dated observations, logged nutrient intake, and editable targets.".to_owned(),
            button {
                class: "btn btn-outline",
                onclick: move |_| {
                    series.restart();
                    nutrition.restart();
                    goals.restart();
                },
                "Refresh"
            }
        }
        if !error().is_empty() { Notice { message: error() } }
        Card {
            title: "Measurements over time".to_owned(),
            description: "Time and recorded values are shown without filling gaps or mixing units.".to_owned(),
            form {
                class: "card-body health-metrics-form",
                onsubmit: move |event| {
                    event.prevent_default();
                    if !valid_metric(&domain()) || !valid_metric(&metric())
                        || start().is_empty() || end().is_empty() || start() > end() {
                        error.set("Choose a valid metric and an ordered date interval.".to_owned());
                        return;
                    }
                    error.set(String::new());
                    chosen.set(Filter {
                        domain: domain(), metric: metric(), start: start(), end: end(),
                    });
                    series.restart();
                },
                div { class: "task-field-grid",
                    label { class: "form-row",
                        span { class: "label", "Domain" }
                        select {
                            class: "field", value: "{domain()}",
                            onchange: move |event| {
                                let selected = event.value();
                                let suggested = match selected.as_str() {
                                    "sleep" => "duration_hours",
                                    "biometric" => "weight",
                                    "nutrition" => "protein",
                                    "activity" => "steps_interval",
                                    "cognition" => "simple_reaction_ms",
                                    "workout" => "session",
                                    _ => "",
                                };
                                domain.set(selected);
                                metric.set(suggested.to_owned());
                            },
                            option { value: "biometric", "Biometric" }
                            option { value: "sleep", "Sleep" }
                            option { value: "nutrition", "Nutrition" }
                            option { value: "activity", "Activity" }
                            option { value: "cognition", "Cognition" }
                            option { value: "workout", "Workout" }
                        }
                    }
                    label { class: "form-row",
                        span { class: "label", "Metric" }
                        input {
                            class: "field", required: true, value: "{metric()}",
                            oninput: move |event| metric.set(event.value())
                        }
                    }
                    label { class: "form-row",
                        span { class: "label", "Start (UTC)" }
                        input {
                            class: "field", r#type: "date", required: true,
                            value: "{start()}", oninput: move |event| start.set(event.value())
                        }
                    }
                    label { class: "form-row",
                        span { class: "label", "End (UTC)" }
                        input {
                            class: "field", r#type: "date", required: true,
                            value: "{end()}", oninput: move |event| end.set(event.value())
                        }
                    }
                }
                button { class: "btn btn-primary", "Load measurements" }
            }
            div { class: "card-body",
                match series.cloned() {
                    None => rsx! { Loading {} },
                    Some(Err(message)) => rsx! { Notice { message } },
                    Some(Ok(data)) => {
                        if data.points.is_empty() {
                            rsx! { Empty {
                                title: "No measurements".to_owned(),
                                description: "The Health service returned no samples in this date range.".to_owned()
                            } }
                        } else {
                            match chart(&data.points) {
                                Err(message) => rsx! {
                                    Notice { message: message.to_owned() }
                                },
                                Ok((line, dots)) => rsx! {
                                    svg {
                                        class: "health-metric-plot",
                                        role: "img",
                                        "aria-label": "Observed health metric plotted against recorded timestamps",
                                        view_box: "0 0 800 200",
                                        preserve_aspect_ratio: "xMidYMid meet",
                                        line {
                                            x1: "35", y1: "175", x2: "765", y2: "175",
                                            stroke: "currentColor", stroke_opacity: "0.2"
                                        }
                                        path {
                                            d: "{line}", fill: "none",
                                            stroke: "currentColor", stroke_width: "2.5",
                                            stroke_linecap: "round", stroke_linejoin: "round"
                                        }
                                        for dot in dots {
                                            circle {
                                                cx: "{dot.x:.2}", cy: "{dot.y:.2}",
                                                r: "3.5", fill: "currentColor",
                                                title { "{dot.label}" }
                                            }
                                        }
                                    }
                                    p { class: "small muted",
                                        "{data.points.len()} observations. Dates reflect actual sample times; no missing days are interpolated."
                                    }
                                }
                            }
                        }
                    }
                }
            }
        }
        Card {
            title: "Daily nutrition".to_owned(),
            description: "Recorded intake may be incomplete. A missing nutrient is unknown, not zero.".to_owned(),
            div { class: "card-body health-metrics-form",
                label { class: "form-row",
                    span { class: "label", "UTC date" }
                    input {
                        class: "field", r#type: "date", value: "{day()}",
                        oninput: move |event| day.set(event.value())
                    }
                }
                match (nutrition.cloned(), goals.cloned()) {
                    (Some(Ok(nutrition_data)), Some(Ok(goal_data))) => {
                        let rows = nutrient_rows(&nutrition_data, &goal_data);
                        rsx! {
                            if rows.is_empty() {
                                Empty {
                                    title: "No logged nutrients".to_owned(),
                                    description: "There are no nutrient logs or targets for this day.".to_owned()
                                }
                            } else {
                                div { class: "table-clip",
                                    table { class: "table",
                                        thead { tr {
                                            th { "Nutrient" }
                                            th { "Logged" }
                                            th { "Target" }
                                            th { "Coverage" }
                                        } }
                                        tbody {
                                            for row in rows {
                                                tr {
                                                    th { scope: "row", "{row.name}" }
                                                    td { "{row.logged}" }
                                                    td { "{row.target}" }
                                                    td { class: "muted", "{row.coverage}" }
                                                }
                                            }
                                        }
                                    }
                                }
                            }
                        }
                    }
                    (Some(Err(message)), _) | (_, Some(Err(message))) => rsx! {
                        Notice { message }
                    },
                    _ => rsx! { Loading {} },
                }
            }
        }
        Card {
            title: "Set a health target".to_owned(),
            description: "Targets are explicit owner-entered values. They are not inferred dietary requirements.".to_owned(),
            form {
                class: "card-body health-metrics-form",
                onsubmit: move |event| {
                    event.prevent_default();
                    let Some(value) = goal_value().parse::<f64>().ok()
                        .filter(|number| number.is_finite()) else {
                        error.set("Enter a finite target value.".to_owned());
                        return;
                    };
                    if !valid_metric(&goal_metric()) || goal_unit().trim().is_empty()
                        || goal_unit().len() > 64 || busy() {
                        error.set("Enter a valid metric name and unit.".to_owned());
                        return;
                    }
                    let token = csrf();
                    let args = json!({
                        "metric": goal_metric(), "value": value, "unit": goal_unit(),
                    });
                    error.set(String::new());
                    busy.set(true);
                    spawn(async move {
                        match health_tools::call::<Value>(&token, Tool::GoalSet, args).await {
                            Ok(_) => {
                                status.set("Target saved.".to_owned());
                                goals.restart();
                                nutrition.restart();
                            }
                            Err(message) => error.set(message),
                        }
                        busy.set(false);
                    });
                },
                div { class: "task-field-grid",
                    label { class: "form-row",
                        span { class: "label", "Metric" }
                        input {
                            class: "field", required: true, value: "{goal_metric()}",
                            oninput: move |event| goal_metric.set(event.value())
                        }
                    }
                    label { class: "form-row",
                        span { class: "label", "Target" }
                        input {
                            class: "field", required: true, r#type: "number",
                            step: "any", value: "{goal_value()}",
                            oninput: move |event| goal_value.set(event.value())
                        }
                    }
                    label { class: "form-row",
                        span { class: "label", "Unit" }
                        input {
                            class: "field", required: true, value: "{goal_unit()}",
                            oninput: move |event| goal_unit.set(event.value())
                        }
                    }
                }
                button { class: "btn btn-primary", disabled: busy(),
                    if busy() { "Saving…" } else { "Save target" }
                }
                p { class: "small muted", role: "status", aria_live: "polite", "{status()}" }
            }
        }
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn metric_names_have_bounded_lowercase_identifiers() {
        assert!(valid_metric("protein"));
        assert!(valid_metric("simple_reaction_ms"));
        assert!(!valid_metric(""));
        assert!(!valid_metric("Weight"));
        assert!(!valid_metric("../invalid"));
        assert!(!valid_metric(&"a".repeat(65)));
    }

    #[test]
    fn missing_nutrients_do_not_become_zero() {
        let goals = json!({"protein": {"value": 100.0, "unit": "g"}});
        let nutrients = json!({"units_and_totals": {}});
        let rows = nutrient_rows(&nutrients, &goals);
        assert_eq!(rows.len(), 1);
        assert_eq!(rows[0].logged, "Unknown");
        assert_eq!(rows[0].coverage, "Missing");
    }
}
