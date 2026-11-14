use dioxus::prelude::*;
use reqwest::Method;
use serde::{Deserialize, Serialize};
use serde_json::json;

use crate::api;
use crate::components::{Empty, Loading, Notice};

#[derive(Clone, Debug, Default, PartialEq, Deserialize)]
#[serde(default)]
struct PantryItem {
    key: String,
    name: String,
    unit: String,
    amount: f64,
    expires: String,
}

#[derive(Clone, Debug, Default, PartialEq, Deserialize)]
#[serde(default)]
struct PantryBoard {
    revision: i32,
    items: Vec<PantryItem>,
}

#[derive(Clone, Debug, Serialize)]
struct NutrientTarget {
    name: String,
    unit: String,
    remaining: f64,
}

#[derive(Clone, Debug, Default, PartialEq, Deserialize)]
#[serde(default)]
struct Shortage {
    name: String,
    unit: String,
    shortage: f64,
    reason: String,
}

#[derive(Clone, Debug, Default, PartialEq, Deserialize)]
#[serde(default)]
struct Recommendation {
    recipe_id: String,
    title: String,
    ingredient_coverage: f64,
    nutrient_fit: f64,
    known_targets: usize,
    unknown_targets: Vec<String>,
    nutrition_method: String,
    missing: Vec<Shortage>,
}

#[derive(Clone, Debug, Default, PartialEq, Deserialize)]
#[serde(default)]
struct RecommendationResult {
    matches: Vec<Recommendation>,
    note: String,
}

fn parse_nutrient_targets(text: &str) -> Result<Vec<NutrientTarget>, String> {
    let mut targets = Vec::new();
    for (index, line) in text.lines().enumerate() {
        if line.trim().is_empty() {
            continue;
        }
        let fields: Vec<&str> = line.split('|').map(str::trim).collect();
        if fields.len() != 3 || fields[0].is_empty() || fields[1].is_empty() {
            return Err(format!(
                "Nutrient target line {} must be name | unit | remaining",
                index + 1
            ));
        }
        let remaining: f64 = fields[2]
            .parse()
            .map_err(|_| format!("Invalid remaining nutrient amount on line {}", index + 1))?;
        if !remaining.is_finite() || remaining < 0.0 {
            return Err(format!(
                "Remaining nutrient amount must be nonnegative on line {}",
                index + 1
            ));
        }
        targets.push(NutrientTarget {
            name: fields[0].to_owned(),
            unit: fields[1].to_owned(),
            remaining,
        });
        if targets.len() > 32 {
            return Err("Use no more than 32 nutrient targets".to_owned());
        }
    }
    Ok(targets)
}

fn percentage(value: f64) -> String {
    format!("{:.0}%", value * 100.0)
}

#[component]
pub fn Pantry(csrf: String) -> Element {
    let csrf = use_signal(|| csrf);
    let mut pantry = use_resource(|| async { api::get::<PantryBoard>("/v1/pantry").await });
    let mut name = use_signal(String::new);
    let mut unit = use_signal(String::new);
    let mut amount = use_signal(|| "1".to_owned());
    let mut expires = use_signal(String::new);
    let mut target_text = use_signal(String::new);
    let mut serving_text = use_signal(|| "1".to_owned());
    let mut recommendations = use_signal(|| Option::<RecommendationResult>::None);
    let mut busy = use_signal(|| false);
    let mut error = use_signal(String::new);
    let mut notice = use_signal(String::new);

    let board = pantry.cloned().and_then(Result::ok).unwrap_or_default();
    let revision = board.revision;

    rsx! {
        section { class: "card",
            div { class: "card-header",
                h2 { class: "card-title", "Pantry inventory" }
                p { class: "card-description",
                    "Record exact ingredient quantities and units. Inventory is separate from grocery purchase status."
                }
            }
            div { class: "card-body stack",
                if !error().is_empty() { Notice { message: error() } }
                if !notice().is_empty() {
                    p { class: "small", role: "status", "{notice}" }
                }
                if let Some(Err(message)) = pantry.cloned() {
                    Notice { message }
                } else if pantry.cloned().is_none() {
                    Loading {}
                } else if board.items.is_empty() {
                    Empty {
                        title: "No pantry quantities recorded".to_owned(),
                        description: "Add an ingredient to make inventory-aware recipe recommendations.".to_owned()
                    }
                } else {
                    ul { class: "recipe-shopping",
                        for item in &board.items {
                            {
                                let key = item.key.clone();
                                let name = item.name.clone();
                                let unit = item.unit.clone();
                                let expires = item.expires.clone();
                                let count = item.amount;
                                rsx! {
                                    li { key: "{key}",
                                        span { "{name}" }
                                        strong { "{count} {unit}" }
                                        if !expires.is_empty() {
                                            span { class: "small muted", "Expires {expires}" }
                                        }
                                        button {
                                            class: "btn btn-outline btn-sm",
                                            disabled: busy(),
                                            aria_label: format!("Remove {} from pantry", name),
                                            onclick: move |_| {
                                                if busy() { return; }
                                                let path = format!("/v1/pantry/items/{key}");
                                                let token = csrf();
                                                busy.set(true);
                                                error.set(String::new());
                                                spawn(async move {
                                                    match api::write::<PantryBoard, _>(
                                                        Method::DELETE, &path, &token,
                                                        &json!({"expected_revision": revision})
                                                    ).await {
                                                        Ok(_) => {
                                                            pantry.restart();
                                                            recommendations.set(None);
                                                            notice.set("Pantry entry removed".to_owned());
                                                        }
                                                        Err(message) => {
                                                            error.set(message);
                                                            pantry.restart();
                                                        }
                                                    }
                                                    busy.set(false);
                                                });
                                            },
                                            "Remove"
                                        }
                                    }
                                }
                            }
                        }
                    }
                }
                form {
                    class: "recipe-form",
                    onsubmit: move |event| {
                        event.prevent_default();
                        if busy() { return; }
                        let quantity = amount().parse::<f64>().unwrap_or(0.0);
                        if name().trim().is_empty() || !quantity.is_finite() || quantity <= 0.0 {
                            error.set("Enter an ingredient and a positive amount".to_owned());
                            return;
                        }
                        let payload = json!({
                            "item": {
                                "name": name().trim(),
                                "unit": unit().trim(),
                                "amount": quantity,
                                "expires": expires().trim(),
                            },
                            "expected_revision": revision,
                        });
                        let token = csrf();
                        busy.set(true);
                        error.set(String::new());
                        spawn(async move {
                            match api::write::<PantryBoard, _>(
                                Method::PUT, "/v1/pantry", &token, &payload
                            ).await {
                                Ok(_) => {
                                    pantry.restart();
                                    recommendations.set(None);
                                    name.set(String::new());
                                    unit.set(String::new());
                                    amount.set("1".to_owned());
                                    expires.set(String::new());
                                    notice.set("Pantry quantity saved".to_owned());
                                }
                                Err(message) => {
                                    error.set(message);
                                    pantry.restart();
                                }
                            }
                            busy.set(false);
                        });
                    },
                    div { class: "recipe-field-grid",
                        label { class: "form-row",
                            "Ingredient"
                            input { class: "field", required: true, value: "{name}",
                                oninput: move |event| name.set(event.value()) }
                        }
                        label { class: "form-row",
                            "Unit (must match the recipe)"
                            input { class: "field", placeholder: "g, ml, count",
                                value: "{unit}", oninput: move |event| unit.set(event.value()) }
                        }
                        label { class: "form-row",
                            "Available amount"
                            input { class: "field", type: "number", min: "0.000001",
                                step: "any", required: true, value: "{amount}",
                                oninput: move |event| amount.set(event.value()) }
                        }
                        label { class: "form-row",
                            "Expiry date (optional)"
                            input { class: "field", type: "date", value: "{expires}",
                                oninput: move |event| expires.set(event.value()) }
                        }
                    }
                    button { class: "btn btn-primary", type: "submit", disabled: busy(),
                        "Save quantity" }
                }
                p { class: "small muted",
                    "Editing an existing ingredient and unit replaces its recorded quantity. A missing item is unknown, not confirmed absent."
                }
            }
        }
        section { class: "card",
            div { class: "card-header",
                h2 { class: "card-title", "Recipes from available supplies" }
                p { class: "card-description",
                    "Compare recorded ingredients against recipes. Optional nutrient targets are entered explicitly and do not alter Health records."
                }
            }
            div { class: "card-body stack",
                label { class: "form-row",
                    "Portions to prepare"
                    input { class: "field", type: "number", min: "0.01",
                        step: "any", value: "{serving_text}",
                        oninput: move |event| serving_text.set(event.value()) }
                }
                label { class: "form-row",
                    "Remaining nutrient targets (one per line: name | unit | remaining)"
                    textarea { class: "field textarea", rows: "3",
                        placeholder: "protein | g | 40",
                        value: "{target_text}",
                        oninput: move |event| target_text.set(event.value()) }
                }
                button {
                    class: "btn btn-primary", disabled: busy(),
                    onclick: move |_| {
                        if busy() { return; }
                        let servings = serving_text().parse::<f64>().unwrap_or(0.0);
                        if !servings.is_finite() || servings <= 0.0 {
                            error.set("Enter a positive serving quantity".to_owned());
                            return;
                        }
                        let targets = match parse_nutrient_targets(&target_text()) {
                            Ok(value) => value,
                            Err(message) => { error.set(message); return; }
                        };
                        let token = csrf();
                        busy.set(true);
                        error.set(String::new());
                        spawn(async move {
                            match api::write::<RecommendationResult, _>(
                                Method::POST, "/v1/recipes/recommend", &token,
                                &json!({"servings":servings,"targets":targets,"limit":20})
                            ).await {
                                Ok(result) => recommendations.set(Some(result)),
                                Err(message) => error.set(message),
                            }
                            busy.set(false);
                        });
                    },
                    "Find matching recipes"
                }
                if let Some(result) = recommendations() {
                    p { class: "small muted", "{result.note}" }
                    if result.matches.is_empty() {
                        Empty {
                            title: "No saved recipes".to_owned(),
                            description: "Add recipes to compare them against the pantry.".to_owned()
                        }
                    } else {
                        div { class: "item-list",
                            for candidate in result.matches {
                                article { class: "list-item", key: "{candidate.recipe_id}",
                                    div {
                                        strong { "{candidate.title}" }
                                        p { class: "small muted",
                                            "Ingredient coverage: {percentage(candidate.ingredient_coverage)}"
                                        }
                                        if candidate.known_targets > 0 {
                                            p { class: "small muted",
                                                "Recorded nutrient target fit: {percentage(candidate.nutrient_fit)} ({candidate.known_targets} known targets)"
                                            }
                                        }
                                        if !candidate.nutrition_method.is_empty() {
                                            p { class: "small muted", "Nutrition: {candidate.nutrition_method}" }
                                        }
                                        for ingredient in candidate.missing {
                                            p { class: "small muted",
                                                "{ingredient.name}: need {ingredient.shortage} {ingredient.unit} ({ingredient.reason})"
                                            }
                                        }
                                        if !candidate.unknown_targets.is_empty() {
                                            p { class: "small muted",
                                                {format!("Unknown nutrients: {}", candidate.unknown_targets.join(", "))}
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

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn nutrient_targets_are_explicit_and_bounded() {
        let values = parse_nutrient_targets("protein | g | 40\nfiber | g | 28").unwrap();
        assert_eq!(values.len(), 2);
        assert_eq!(values[0].remaining, 40.0);
        assert_eq!(values[1].name, "fiber");

        for malformed in [
            "protein | 40",
            "protein | g | -3",
            "protein | g | NaN",
            "protein | g | infinity",
        ] {
            assert!(parse_nutrient_targets(malformed).is_err());
        }
        assert!(parse_nutrient_targets(&"protein | g | 1\n".repeat(33)).is_err());
    }
}
