use dioxus::prelude::*;
use reqwest::Method;
use serde::{Deserialize, Serialize};
use serde_json::{json, Value};

use crate::api;
use crate::components::{Empty, Notice};

#[derive(Clone, Debug, Default, PartialEq)]
struct Selection {
    fdc_id: String,
    grams: String,
}

#[derive(Clone, Debug, Serialize)]
struct IngredientSelection {
    ingredient_index: usize,
    fdc_id: i64,
    grams: f64,
}

#[derive(Clone, Debug, Default, PartialEq, Deserialize)]
#[serde(default)]
struct Nutrient {
    name: String,
    amount: f64,
    unit: String,
}

#[derive(Clone, Debug, Default, PartialEq, Deserialize)]
#[serde(default)]
struct SourceFood {
    ingredient_index: usize,
    fdc_id: i64,
    description: String,
    data_type: String,
    grams: f64,
}

#[derive(Clone, Debug, Default, PartialEq, Deserialize)]
#[serde(default)]
struct Estimate {
    source: String,
    method: String,
    servings: f64,
    per_serving: Vec<Nutrient>,
    incomplete: Vec<String>,
    ingredients: Vec<SourceFood>,
    limitations: String,
    sha256: String,
}

fn mapped_ingredients(draft: &[Selection]) -> Result<Vec<IngredientSelection>, String> {
    if draft.is_empty() || draft.len() > 12 {
        return Err("A recipe must contain between 1 and 12 mapped ingredients".to_owned());
    }
    draft.iter().enumerate().map(|(index, entry)| {
        let fdc_id = entry.fdc_id.trim().parse::<i64>()
            .map_err(|_| format!("Ingredient {} requires a USDA food ID", index + 1))?;
        let grams = entry.grams.trim().parse::<f64>()
            .map_err(|_| format!("Ingredient {} requires an edible mass in grams", index + 1))?;
        if fdc_id <= 0 || !grams.is_finite() || grams <= 0.0 || grams > 10000.0 {
            return Err(format!("Ingredient {} has an invalid USDA ID or edible mass", index + 1));
        }
        Ok(IngredientSelection { ingredient_index: index, fdc_id, grams })
    }).collect()
}

#[component]
pub fn USDANutrition(
    recipe_id: String,
    recipe_revision: i32,
    recipe_servings: f64,
    ingredients: Vec<String>,
    csrf: String,
    on_saved: EventHandler<()>,
) -> Element {
    let csrf = use_signal(|| csrf);
    let mut selections = use_signal(|| vec![Selection::default(); ingredients.len()]);
    let mut preview = use_signal(|| Option::<Estimate>::None);
    let mut busy = use_signal(|| false);
    let mut error = use_signal(String::new);
    let mut notice = use_signal(String::new);

    rsx! {
        section { class: "card",
            div { class: "card-header",
                h2 { class: "card-title", "USDA ingredient nutrition" }
                p { class: "card-description",
                    "Match each recipe ingredient to a reviewed FoodData Central food ID and edible weight. This creates a source-linked estimate, not a food-consumption record."
                }
            }
            div { class: "card-body stack",
                if !error().is_empty() { Notice { message: error() } }
                if !notice().is_empty() {
                    p { class: "small", role: "status", "{notice}" }
                }
                p { class: "small muted",
                    "Recipe yield: {recipe_servings} servings. Search matching foods using the Health food-search tool before selecting exact USDA IDs."
                }
                if ingredients.is_empty() || ingredients.len() > 12 {
                    Empty {
                        title: "Unsupported ingredient count".to_owned(),
                        description: "USDA estimates accept recipes with 1–12 explicitly mapped ingredients.".to_owned()
                    }
                } else {
                    div { class: "recipe-field-grid",
                        for (index, name) in ingredients.iter().enumerate() {
                            {
                                let selected = selections().get(index).cloned().unwrap_or_default();
                                rsx! {
                                    div { class: "form-row", key: "{index}",
                                        strong { "{name}" }
                                        label { class: "form-row",
                                            "USDA FoodData Central ID"
                                            input {
                                                class: "field",
                                                inputmode: "numeric",
                                                value: "{selected.fdc_id}",
                                                disabled: busy(),
                                                oninput: move |event| {
                                                    let mut list = selections();
                                                    if let Some(entry) = list.get_mut(index) {
                                                        entry.fdc_id = event.value();
                                                        selections.set(list);
                                                        preview.set(None);
                                                    }
                                                }
                                            }
                                        }
                                        label { class: "form-row",
                                            "Edible weight (grams)"
                                            input {
                                                class: "field", type: "number", step: "any", min: "0.001",
                                                value: "{selected.grams}",
                                                disabled: busy(),
                                                oninput: move |event| {
                                                    let mut list = selections();
                                                    if let Some(entry) = list.get_mut(index) {
                                                        entry.grams = event.value();
                                                        selections.set(list);
                                                        preview.set(None);
                                                    }
                                                }
                                            }
                                        }
                                    }
                                }
                            }
                        }
                    }
                    button { class: "btn btn-primary", disabled: busy(),
                        onclick: {
                            let id = recipe_id.clone();
                            move |_| {
                                if busy() { return; }
                                let entries = match mapped_ingredients(&selections()) {
                                    Ok(value) => value,
                                    Err(message) => { error.set(message); return; }
                                };
                                let token = csrf();
                                let path = format!("/v1/recipes/{id}/usda-preview");
                                busy.set(true);
                                preview.set(None);
                                error.set(String::new());
                                spawn(async move {
                                    match api::write::<Estimate, _>(
                                        Method::POST, &path, &token,
                                        &json!({"expected_revision":recipe_revision,"ingredients":entries})
                                    ).await {
                                        Ok(result) => {
                                            preview.set(Some(result));
                                            notice.set("Review the source evidence and nutrient completeness before saving.".to_owned());
                                        }
                                        Err(message) => error.set(message),
                                    }
                                    busy.set(false);
                                });
                            }
                        },
                        "Preview USDA composition"
                    }
                }
            }
        }
        if let Some(result) = preview() {
            section { class: "card",
                div { class: "card-header",
                    h2 { class: "card-title", "Review estimated nutrients" }
                    p { class: "card-description", "Calculated from exact source foods, not confirmed dietary intake." }
                }
                div { class: "card-body stack",
                    p { class: "small muted", "{result.limitations}" }
                    div { class: "item-list",
                        for food in &result.ingredients {
                            article { class: "list-item", key: "{food.ingredient_index}",
                                strong { "{food.description}" }
                                p { class: "small muted",
                                    "FDC {food.fdc_id} · {food.grams} g · {food.data_type}"
                                }
                            }
                        }
                    }
                    if result.per_serving.is_empty() {
                        Empty {
                            title: "No complete nutrient estimates".to_owned(),
                            description: "Every ingredient must report a nutrient before a whole-recipe value can be saved.".to_owned()
                        }
                    } else {
                        div { class: "item-list",
                            for nutrient in &result.per_serving {
                                p { class: "small", key: "{nutrient.name}",
                                    "{nutrient.name}: {nutrient.amount} {nutrient.unit} per serving"
                                }
                            }
                        }
                    }
                    if !result.incomplete.is_empty() {
                        p { class: "small muted",
                            "Incomplete source coverage (not saved as a total): {result.incomplete.join(", ")}"
                        }
                    }
                    p { class: "small muted", "Evidence SHA-256: {result.sha256}" }
                    button {
                        class: "btn btn-primary",
                        disabled: busy() || result.per_serving.is_empty(),
                        onclick: {
                            let id = recipe_id.clone();
                            let digest = result.sha256.clone();
                            let saved = on_saved.clone();
                            move |_| {
                                if busy() { return; }
                                let entries = match mapped_ingredients(&selections()) {
                                    Ok(value) => value,
                                    Err(message) => { error.set(message); return; }
                                };
                                let token = csrf();
                                let path = format!("/v1/recipes/{id}/usda-apply");
                                busy.set(true);
                                error.set(String::new());
                                spawn(async move {
                                    match api::write::<Value, _>(
                                        Method::POST, &path, &token,
                                        &json!({
                                            "expected_revision":recipe_revision,
                                            "ingredients":entries,
                                            "expected_sha256":digest,
                                            "confirm":true
                                        })
                                    ).await {
                                        Ok(_) => {
                                            notice.set("Source-linked nutrition saved to the recipe. No meal was logged.".to_owned());
                                            preview.set(None);
                                            saved.call(());
                                        }
                                        Err(message) => error.set(message),
                                    }
                                    busy.set(false);
                                });
                            }
                        },
                        "Confirm and save to recipe"
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
    fn ingredient_mass_and_identity_are_explicit() {
        let good = vec![Selection { fdc_id:"1234".to_owned(), grams:"150".to_owned() }];
        assert_eq!(mapped_ingredients(&good).unwrap()[0].grams,150.0);
        for value in ["0","-1","NaN","Infinity","12000",""] {
            let bad = vec![Selection { fdc_id:"1234".to_owned(), grams:value.to_owned() }];
            assert!(mapped_ingredients(&bad).is_err());
        }
        let missing = vec![Selection { fdc_id:"".to_owned(), grams:"100".to_owned() }];
        assert!(mapped_ingredients(&missing).is_err());
    }
}
