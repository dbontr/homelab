use dioxus::prelude::*;
use reqwest::Method;
use std::collections::HashMap;
use serde::{Deserialize, Serialize};

use crate::api;
use crate::components::{Empty, Heading, Loading, Notice};

#[derive(Clone, Debug, Default, PartialEq, Serialize, Deserialize)]
#[serde(default)]
struct Ingredient {
    name: String,
    amount: f64,
    unit: String,
    notes: String,
}

#[derive(Clone, Debug, Default, PartialEq, Serialize, Deserialize)]
#[serde(default)]
struct Step {
    text: String,
}

#[derive(Clone, Debug, Default, PartialEq, Serialize, Deserialize)]
#[serde(default)]
struct Nutrient {
    name: String,
    amount: f64,
    unit: String,
}

#[derive(Clone, Debug, Default, PartialEq, Serialize, Deserialize)]
#[serde(default)]
struct Nutrition {
    method: String,
    source: String,
    per_serving: Vec<Nutrient>,
}

#[derive(Clone, Debug, Default, PartialEq, Serialize, Deserialize)]
#[serde(default)]
struct Recipe {
    id: String,
    title: String,
    summary: String,
    servings: f64,
    prep_minutes: i32,
    cook_minutes: i32,
    ingredients: Vec<Ingredient>,
    steps: Vec<Step>,
    tags: Vec<String>,
    equipment: Vec<String>,
    source_url: String,
    image_ref: String,
    notes: String,
    last_made: String,
    nutrition: Option<Nutrition>,
    revision: i32,
}

impl Recipe {
    fn blank() -> Self {
        Self {
            servings: 2.0,
            ingredients: vec![Ingredient { amount: 1.0, ..Default::default() }],
            steps: vec![Step::default()],
            ..Default::default()
        }
    }
}

#[derive(Clone, Debug, Default, PartialEq, Serialize, Deserialize)]
#[serde(default)]
struct Meal {
    id: String,
    recipe_id: String,
    date: String,
    meal_type: String,
    servings: f64,
    notes: String,
}

#[derive(Clone, Debug, Default, PartialEq, Deserialize)]
#[serde(default)]
struct GroceryItem {
    key: String,
    checked: bool,
    name: String,
    amount: f64,
    unit: String,
    recipe_ids: Vec<String>,
}

#[derive(Clone, Debug, Default, PartialEq, Deserialize)]
#[serde(default)]
struct GroceryBoard {
    revision: i32,
    items: Vec<GroceryItem>,
}

#[derive(Clone, Debug, Default, PartialEq, Deserialize)]
#[serde(default)]
struct HealthGoal {
    value: f64,
    unit: String,
}

fn current_record(records: &[Recipe], selected: Option<String>, query: &str) -> Option<Recipe> {
    selected.and_then(|id| records.iter().find(|recipe| recipe.id == id).cloned())
        .or_else(|| records.iter().find(|recipe| {
            query.is_empty() || format!("{} {} {}", recipe.title, recipe.summary, recipe.tags.join(" "))
                .to_lowercase().contains(query)
        }).cloned())
}

#[component]
fn RecipeChoice(record: Recipe, active_id: String, on_select: EventHandler<String>) -> Element {
    let id = record.id.clone();
    rsx! {
        button { class: "recipe-list-item",
            aria_pressed: if active_id == record.id { "true" } else { "false" },
            onclick: move |_| on_select.call(id.clone()),
            span { class: "recipe-list-title", "{record.title}" }
            span { class: "small muted",
                "{record.servings} servings · {record.prep_minutes+record.cook_minutes} min"
            }
        }
    }
}

#[derive(Clone, Copy, PartialEq, Eq)]
enum Tab {
    Library,
    Plan,
    Groceries,
    Transfer,
}

fn quantity(value: f64) -> String {
    format!("{value:.2}").trim_end_matches('0').trim_end_matches('.').to_owned()
}

fn nutrients_from_lines(text: &str, method: &str, source: &str) -> Result<Option<Nutrition>, String> {
    if text.trim().is_empty() {
        return Ok(None);
    }
    if method != "measured" && method != "estimated" && method != "reported" {
        return Err("Select how the nutrition values were obtained".to_owned());
    }
    let mut entries = Vec::new();
    for (index, line) in text.lines().enumerate() {
        if line.trim().is_empty() { continue; }
        let parts: Vec<_> = line.splitn(3, '|').map(str::trim).collect();
        if parts.len() != 3 {
            return Err(format!("Nutrition line {} must be name | amount | unit", index + 1));
        }
        let amount: f64 = parts[1].parse().map_err(|_| format!("Invalid nutrition amount on line {}", index + 1))?;
        if !amount.is_finite() || amount < 0.0 || parts[0].is_empty() || parts[2].is_empty() {
            return Err(format!("Invalid nutrient on line {}", index + 1));
        }
        entries.push(Nutrient { name: parts[0].to_owned(), amount, unit: parts[2].to_owned() });
    }
    Ok(Some(Nutrition { method: method.to_owned(), source: source.to_owned(), per_serving: entries }))
}

fn nutrient_lines(recipe: &Recipe) -> String {
    recipe.nutrition.as_ref().map(|n| {
        n.per_serving.iter()
            .map(|entry| format!("{} | {} | {}", entry.name, entry.amount, entry.unit))
            .collect::<Vec<_>>()
            .join("\n")
    }).unwrap_or_default()
}

#[component]
pub fn Recipes(csrf: String) -> Element {
    let csrf = use_signal(|| csrf);
    let mut recipes = use_resource(|| async { api::get::<Vec<Recipe>>("/v1/recipes").await });
    let mut meals = use_resource(|| async { api::get::<Vec<Meal>>("/v1/meals").await });
    let mut groceries = use_resource(|| async { api::get::<GroceryBoard>("/v1/groceries/board").await });
    let mut health_goals = use_resource(|| async { api::get::<HashMap<String, HealthGoal>>("/v1/recipes/health-goals").await });

    let mut tab = use_signal(|| Tab::Library);
    let mut search = use_signal(String::new);
    let mut selected = use_signal(|| Option::<String>::None);
    let mut editing = use_signal(|| false);
    let mut draft = use_signal(Recipe::blank);
    let mut tags = use_signal(String::new);
    let mut equipment = use_signal(String::new);
    let mut nutrients = use_signal(String::new);
    let mut nutrition_method = use_signal(|| "estimated".to_owned());
    let mut nutrition_source = use_signal(String::new);
    let mut busy = use_signal(|| false);
    let mut error = use_signal(String::new);
    let mut notice = use_signal(String::new);
    let mut cooking = use_signal(|| false);
    let mut step_index = use_signal(|| 0usize);
    let mut portion = use_signal(|| 2.0f64);
    let mut plan_date = use_signal(String::new);
    let mut plan_type = use_signal(|| "dinner".to_owned());
    let mut confirm_delete = use_signal(|| false);
    let mut import_text = use_signal(String::new);
    let mut schema_text = use_signal(String::new);

    let records = recipes.cloned().and_then(Result::ok).unwrap_or_default();
    let query = search().trim().to_lowercase();
    let filtered: Vec<Recipe> = records.iter().filter(|recipe| {
        query.is_empty() || format!("{} {} {}", recipe.title, recipe.summary, recipe.tags.join(" "))
            .to_lowercase().contains(&query)
    }).cloned().collect();

    let active = current_record(&records, selected(), &query);
    let draft_value = draft();
    let planned = meals.cloned().and_then(Result::ok).unwrap_or_default();
    let grocery_board = groceries.cloned().and_then(Result::ok).unwrap_or_default();
    let grocery_revision = grocery_board.revision;
    let shopping = grocery_board.items;

    rsx! {
        Heading {
            title: "Recipes".to_owned(),
            description: "Your recipe library, cooking plans and shopping list.".to_owned(),
            button { class: "btn btn-outline", onclick: move |_| {
                recipes.restart(); meals.restart(); groceries.restart(); health_goals.restart();
            }, "Refresh" }
            button { class: "btn btn-primary", onclick: move |_| {
                draft.set(Recipe::blank());
                tags.set(String::new()); equipment.set(String::new());
                nutrients.set(String::new()); nutrition_method.set("estimated".to_owned());
                nutrition_source.set(String::new());
                error.set(String::new()); notice.set(String::new());
                editing.set(true); cooking.set(false); confirm_delete.set(false);
                tab.set(Tab::Library);
            }, "Add recipe" }
        }
        div { class: "tabs", role: "tablist", aria_label: "Recipe sections",
            button { class: "tab", role: "tab", aria_selected: if tab()==Tab::Library {"true"} else {"false"},
                onclick: move |_| tab.set(Tab::Library), "Library" }
            button { class: "tab", role: "tab", aria_selected: if tab()==Tab::Plan {"true"} else {"false"},
                onclick: move |_| tab.set(Tab::Plan), "Meal plan" }
            button { class: "tab", role: "tab", aria_selected: if tab()==Tab::Groceries {"true"} else {"false"},
                onclick: move |_| tab.set(Tab::Groceries), "Groceries" }
            button { class: "tab", role: "tab", aria_selected: if tab()==Tab::Transfer {"true"} else {"false"},
                onclick: move |_| tab.set(Tab::Transfer), "Import / export" }
        }
        if !error().is_empty() { Notice { message: error() } }
        if !notice().is_empty() { div { class: "alert", role: "status", "{notice}" } }
        match tab() {
            Tab::Library => rsx! {
                if editing() {
                    section { class: "card recipe-editor",
                        div { class: "card-header",
                            h2 { class: "card-title",
                                if draft_value.id.is_empty() { "Create recipe" } else { "Edit recipe" }
                            }
                            p { class: "card-description", "Keep amounts, method steps and nutrition evidence structured." }
                        }
                        form {
                            class: "card-body recipe-form",
                            onsubmit: move |event| {
                                event.prevent_default();
                                if busy() { return; }
                                let mut value = draft();
                                value.tags = tags().split(',').map(str::trim).filter(|s| !s.is_empty()).map(str::to_owned).collect();
                                value.equipment = equipment().split(',').map(str::trim).filter(|s| !s.is_empty()).map(str::to_owned).collect();
                                match nutrients_from_lines(&nutrients(), &nutrition_method(), &nutrition_source()) {
                                    Err(message) => { error.set(message); return; }
                                    Ok(n) => value.nutrition = n,
                                }
                                let csrf = csrf();
                                let path = if value.id.is_empty() { "/v1/recipes".to_owned() }
                                           else { format!("/v1/recipes/{}", value.id) };
                                let method = if value.id.is_empty() { Method::POST } else { Method::PUT };
                                busy.set(true); error.set(String::new());
                                spawn(async move {
                                    match api::write::<Recipe, _>(method, &path, &csrf, &value).await {
                                        Ok(saved) => {
                                            selected.set(Some(saved.id));
                                            editing.set(false); cooking.set(false);
                                            notice.set("Recipe saved".to_owned());
                                            recipes.restart();
                                        }
                                        Err(message) => error.set(message),
                                    }
                                    busy.set(false);
                                });
                            },
                            div { class: "recipe-field-grid",
                                div { class: "form-row wide",
                                    label { class: "label", for: "recipe-title", "Title" }
                                    input { id: "recipe-title", class: "field", required: true,
                                        maxlength: "200", value: "{draft_value.title}",
                                        oninput: move |event| { let mut value=draft(); value.title=event.value(); draft.set(value); }
                                    }
                                }
                                div { class: "form-row wide",
                                    label { class: "label", for: "recipe-summary", "Summary" }
                                    textarea { id: "recipe-summary", class: "field textarea",
                                        value: "{draft_value.summary}", rows: "2",
                                        oninput: move |event| { let mut value=draft(); value.summary=event.value(); draft.set(value); }
                                    }
                                }
                                div { class: "form-row",
                                    label { class: "label", for: "recipe-servings", "Yield (servings)" }
                                    input { id: "recipe-servings", class: "field", type: "number", min: "0.01", step: "0.25",
                                        value: "{draft_value.servings}",
                                        oninput: move |event| { let mut value=draft(); value.servings=event.value().parse().unwrap_or(0.0); draft.set(value); }
                                    }
                                }
                                div { class: "form-row",
                                    label { class: "label", for: "recipe-prep", "Preparation (minutes)" }
                                    input { id: "recipe-prep", class: "field", type: "number", min: "0",
                                        value: "{draft_value.prep_minutes}",
                                        oninput: move |event| { let mut value=draft(); value.prep_minutes=event.value().parse().unwrap_or(0); draft.set(value); }
                                    }
                                }
                                div { class: "form-row",
                                    label { class: "label", for: "recipe-cook", "Cooking (minutes)" }
                                    input { id: "recipe-cook", class: "field", type: "number", min: "0",
                                        value: "{draft_value.cook_minutes}",
                                        oninput: move |event| { let mut value=draft(); value.cook_minutes=event.value().parse().unwrap_or(0); draft.set(value); }
                                    }
                                }
                                div { class: "form-row",
                                    label { class: "label", for: "recipe-tags", "Tags (comma-separated)" }
                                    input { id: "recipe-tags", class: "field", value: "{tags}",
                                        oninput: move |event| tags.set(event.value()) }
                                }
                                div { class: "form-row wide",
                                    label { class: "label", for: "recipe-equipment", "Equipment (comma-separated)" }
                                    input { id: "recipe-equipment", class: "field", value: "{equipment}",
                                        oninput: move |event| equipment.set(event.value()) }
                                }
                                div { class: "form-row wide",
                                    label { class: "label", for: "recipe-source", "Source URL (HTTPS)" }
                                    input { id: "recipe-source", class: "field", type: "url",
                                        value: "{draft_value.source_url}",
                                        oninput: move |event| { let mut value=draft(); value.source_url=event.value(); draft.set(value); }
                                    }
                                }
                            }
                            div { class: "inline-row space-between",
                                h3 { class: "section-title", "Ingredients" }
                                button { class: "btn btn-outline btn-sm", type: "button", onclick: move |_| {
                                    let mut value=draft();
                                    value.ingredients.push(Ingredient { amount: 1.0, ..Default::default() });
                                    draft.set(value);
                                }, "Add ingredient" }
                            }
                            div { class: "stack",
                                for (index, ingredient) in draft_value.ingredients.iter().enumerate() {
                                    div { class: "recipe-ingredient-row", key: "{index}",
                                        input { class: "field", aria_label: "Ingredient name", placeholder: "Ingredient",
                                            value: "{ingredient.name}",
                                            oninput: move |event| {
                                                let mut value=draft();
                                                if let Some(row)=value.ingredients.get_mut(index) { row.name=event.value(); }
                                                draft.set(value);
                                            }
                                        }
                                        input { class: "field", aria_label: "Ingredient amount", type: "number", min: "0.001", step: "any",
                                            value: "{ingredient.amount}",
                                            oninput: move |event| {
                                                let mut value=draft();
                                                if let Some(row)=value.ingredients.get_mut(index) { row.amount=event.value().parse().unwrap_or(0.0); }
                                                draft.set(value);
                                            }
                                        }
                                        input { class: "field", aria_label: "Ingredient unit", placeholder: "Unit",
                                            value: "{ingredient.unit}",
                                            oninput: move |event| {
                                                let mut value=draft();
                                                if let Some(row)=value.ingredients.get_mut(index) { row.unit=event.value(); }
                                                draft.set(value);
                                            }
                                        }
                                        button { class: "btn btn-ghost btn-sm", type: "button",
                                            aria_label: "Remove ingredient",
                                            disabled: draft_value.ingredients.len() <= 1,
                                            onclick: move |_| {
                                                let mut value=draft();
                                                if value.ingredients.len()>1 { value.ingredients.remove(index); draft.set(value); }
                                            }, "Remove" }
                                    }
                                }
                            }
                            div { class: "inline-row space-between section",
                                h3 { class: "section-title", "Method" }
                                button { class: "btn btn-outline btn-sm", type: "button", onclick: move |_| {
                                    let mut value=draft(); value.steps.push(Step::default()); draft.set(value);
                                }, "Add step" }
                            }
                            div { class: "stack",
                                for (index, step) in draft_value.steps.iter().enumerate() {
                                    div { class: "recipe-step-row", key: "{index}",
                                        span { class: "muted", "{index+1}." }
                                        textarea { class: "field textarea", rows: "2", aria_label: "Method step",
                                            value: "{step.text}",
                                            oninput: move |event| {
                                                let mut value=draft();
                                                if let Some(row)=value.steps.get_mut(index) { row.text=event.value(); }
                                                draft.set(value);
                                            }
                                        }
                                        button { class: "btn btn-ghost btn-sm", type: "button", aria_label: "Remove step",
                                            disabled: draft_value.steps.len() <= 1,
                                            onclick: move |_| {
                                                let mut value=draft();
                                                if value.steps.len()>1 { value.steps.remove(index); draft.set(value); }
                                            }, "Remove" }
                                    }
                                }
                            }
                            div { class: "recipe-field-grid section",
                                div { class: "form-row",
                                    label { class: "label", for: "recipe-method", "Nutrition evidence" }
                                    select { id: "recipe-method", class: "field", value: "{nutrition_method}",
                                        onchange: move |event| nutrition_method.set(event.value()),
                                        option { value: "estimated", "Estimated" }
                                        option { value: "measured", "Measured" }
                                        option { value: "reported", "Reported" }
                                    }
                                }
                                div { class: "form-row",
                                    label { class: "label", for: "recipe-nutrient-source", "Nutrition source" }
                                    input { id: "recipe-nutrient-source", class: "field",
                                        value: "{nutrition_source}",
                                        oninput: move |event| nutrition_source.set(event.value()) }
                                }
                                div { class: "form-row wide",
                                    label { class: "label", for: "recipe-nutrients", "Nutrients per serving (one name | amount | unit per line)" }
                                    textarea { id: "recipe-nutrients", class: "field textarea", rows: "4",
                                        placeholder: "protein | 20 | g",
                                        value: "{nutrients}", oninput: move |event| nutrients.set(event.value()) }
                                }
                                div { class: "form-row wide",
                                    label { class: "label", for: "recipe-notes", "Notes" }
                                    textarea { id: "recipe-notes", class: "field textarea", rows: "3",
                                        value: "{draft_value.notes}",
                                        oninput: move |event| { let mut value=draft(); value.notes=event.value(); draft.set(value); }
                                    }
                                }
                            }
                            div { class: "inline-row recipe-actions",
                                button { class: "btn btn-primary", type: "submit", disabled: busy(), "Save recipe" }
                                button { class: "btn btn-outline", type: "button", onclick: move |_| {editing.set(false); error.set(String::new());}, "Cancel" }
                            }
                        }
                    }
                } else {
                    div { class: "recipe-layout",
                        section { class: "card recipe-directory",
                            div { class: "card-header",
                                h2 { class: "card-title", "Recipe library" }
                                p { class: "card-description", "{records.len()} saved recipes" }
                            }
                            div { class: "card-body",
                                input { class: "field", type: "search", placeholder: "Search recipes and tags",
                                    aria_label: "Search recipes", value: "{search}",
                                    oninput: move |event| search.set(event.value()) }
                                if let Some(Err(message)) = recipes.cloned() {
                                    Notice { message }
                                } else if recipes.cloned().is_none() {
                                    Loading {}
                                } else if filtered.is_empty() {
                                    Empty { title: "No recipes found".to_owned(),
                                        description: "Create a recipe or change your search.".to_owned() }
                                } else {
                                    div { class: "recipe-list",
                                        for recipe in filtered {
                                            RecipeChoice {
                                                key: "{recipe.id}",
                                                record: recipe,
                                                active_id: active.as_ref().map(|r|r.id.clone()).unwrap_or_default(),
                                                on_select: move |id: String| {
                                                    selected.set(Some(id));
                                                    cooking.set(false); confirm_delete.set(false); notice.set(String::new());
                                                }
                                            }
                                        }
                                    }
                                }
                            }
                        }
                        section { class: "card recipe-detail",
                            if let Some(recipe) = active {
                                div { class: "card-header",
                                    div { class: "inline-row space-between",
                                        h2 { class: "card-title", "{recipe.title}" }
                                        div { class: "inline-row",
                                            button { class: "btn btn-outline btn-sm", onclick: move |_| {
                                                if let Some(record) = current_record(&recipes.cloned().and_then(Result::ok).unwrap_or_default(), selected(), &search().trim().to_lowercase()) {
                                                    tags.set(record.tags.join(", "));
                                                    equipment.set(record.equipment.join(", "));
                                                    nutrients.set(nutrient_lines(&record));
                                                    nutrition_method.set(record.nutrition.as_ref().map(|n| n.method.clone()).unwrap_or_else(|| "estimated".to_owned()));
                                                    nutrition_source.set(record.nutrition.as_ref().map(|n| n.source.clone()).unwrap_or_default());
                                                    draft.set(record); editing.set(true); cooking.set(false);
                                                }
                                            }, "Edit" }
                                            button { class: "btn btn-outline btn-sm", onclick: move |_| {
                                                cooking.set(!cooking()); step_index.set(0);
                                            }, if cooking() { "Exit cooking" } else { "Cook" } }
                                        }
                                    }
                                    p { class: "card-description", "{recipe.summary}" }
                                    div { class: "inline-row muted small",
                                        span { "{recipe.servings} portions" }
                                        span { "Prep {recipe.prep_minutes} min" }
                                        span { "Cook {recipe.cook_minutes} min" }
                                    }
                                }
                                div { class: "card-body stack",
                                    div { class: "inline-row recipe-portion",
                                        label { class: "label", for: "recipe-portion", "Portions to make" }
                                        input { id: "recipe-portion", class: "field", type: "number", min: "0.25", step: "0.25",
                                            value: "{portion}", oninput: move |event| {
                                                portion.set(event.value().parse::<f64>().unwrap_or(0.0));
                                            } }
                                    }
                                    div {
                                        h3 { class: "section-title", "Ingredients" }
                                        ul { class: "recipe-ingredients",
                                            for ingredient in &recipe.ingredients {
                                                li {
                                                    span { class: "recipe-amount",
                                                        {quantity(ingredient.amount * portion() / recipe.servings)}
                                                        " {ingredient.unit}"
                                                    }
                                                    span { "{ingredient.name}" }
                                                    if !ingredient.notes.is_empty() { span { class: "small muted", " — {ingredient.notes}" } }
                                                }
                                            }
                                        }
                                    }
                                    div {
                                        h3 { class: "section-title", "Method" }
                                        if cooking() {
                                            div { class: "recipe-cook-step", aria_live: "polite",
                                                p { class: "small muted", "Step {step_index()+1} of {recipe.steps.len()}" }
                                                p { "{recipe.steps[step_index().min(recipe.steps.len()-1)].text}" }
                                                div { class: "inline-row",
                                                    button { class: "btn btn-outline btn-sm", disabled: step_index()==0,
                                                        onclick: move |_| step_index.set(step_index().saturating_sub(1)), "Previous" }
                                                    button { class: "btn btn-primary btn-sm", disabled: step_index()+1>=recipe.steps.len(),
                                                        onclick: move |_| step_index.set(step_index()+1), "Next step" }
                                                }
                                            }
                                        } else {
                                            ol { class: "recipe-method",
                                                for step in &recipe.steps { li { "{step.text}" } }
                                            }
                                        }
                                    }
                                    if let Some(nutrition) = &recipe.nutrition {
                                        div {
                                            h3 { class: "section-title", "Nutrition for selected portions" }
                                            p { class: "small muted",
                                                "{nutrition.method} · {nutrition.source}"
                                            }
                                            if let Some(Ok(goals)) = health_goals.cloned() {
                                                p { class: "small muted", "Daily Health targets are shown when the nutrient name and unit match." }
                                                div { class: "recipe-nutrient-list",
                                                    for nutrient in &nutrition.per_serving {
                                                        if let Some(goal) = goals.get(&nutrient.name.to_lowercase()) {
                                                            if goal.unit == nutrient.unit {
                                                                div { class: "inline-row space-between",
                                                                    span { "{nutrient.name} goal comparison" }
                                                                    span { class: "small",
                                                                        "{quantity(nutrient.amount * portion())} / {quantity(goal.value)} {goal.unit}"
                                                                    }
                                                                }
                                                            }
                                                        }
                                                    }
                                                }
                                            }
                                            div { class: "recipe-nutrient-list",
                                                for nutrient in &nutrition.per_serving {
                                                    div { class: "inline-row space-between",
                                                        span { "{nutrient.name}" }
                                                        strong { "{quantity(nutrient.amount*portion())} {nutrient.unit}" }
                                                    }
                                                }
                                            }
                                        }
                                    }
                                    div { class: "divider" }
                                    div { class: "recipe-plan-form",
                                        h3 { class: "section-title", "Add to meal plan" }
                                        input { class: "field", type: "date", aria_label: "Planned date",
                                            value: "{plan_date}", oninput: move |event| plan_date.set(event.value()) }
                                        select { class: "field", aria_label: "Meal type", value: "{plan_type}",
                                            onchange: move |event| plan_type.set(event.value()),
                                            option { value: "breakfast", "Breakfast" }
                                            option { value: "lunch", "Lunch" }
                                            option { value: "dinner", "Dinner" }
                                            option { value: "snack", "Snack" }
                                        }
                                        button { class: "btn btn-outline", disabled: busy() || plan_date().is_empty(),
                                            onclick: move |_| {
                                                let Some(record) = current_record(&recipes.cloned().and_then(Result::ok).unwrap_or_default(), selected(), &search().trim().to_lowercase()) else {return};
                                                let meal = Meal { recipe_id: record.id, date: plan_date(),
                                                    meal_type: plan_type(), servings: portion(), ..Default::default() };
                                                let csrf = csrf();
                                                busy.set(true); error.set(String::new());
                                                spawn(async move {
                                                    match api::write::<Meal,_>(Method::POST,"/v1/meals",&csrf,&meal).await {
                                                        Ok(_) => { meals.restart(); groceries.restart(); notice.set("Meal scheduled".to_owned()); }
                                                        Err(message) => error.set(message),
                                                    }
                                                    busy.set(false);
                                                });
                                            }, "Schedule" }
                                    }
                                    if confirm_delete() {
                                        div { class: "inline-row",
                                            span { class: "small", "Delete this recipe permanently?" }
                                            button { class: "btn btn-outline btn-sm", onclick: move |_| confirm_delete.set(false), "Cancel" }
                                            button { class: "btn btn-primary btn-sm", disabled: busy(), onclick: move |_| {
                                                let Some(record) = current_record(&recipes.cloned().and_then(Result::ok).unwrap_or_default(), selected(), &search().trim().to_lowercase()) else {return};
                                                let path = format!("/v1/recipes/{}", record.id);
                                                let revision = record.revision;
                                                let csrf = csrf();
                                                busy.set(true);
                                                spawn(async move {
                                                    match api::remove(&path,&csrf,Some(revision)).await {
                                                        Ok(()) => { selected.set(None); recipes.restart(); confirm_delete.set(false); notice.set("Recipe deleted".to_owned()); }
                                                        Err(message) => error.set(message),
                                                    }
                                                    busy.set(false);
                                                });
                                            }, "Delete" }
                                        }
                                    } else {
                                        button { class: "btn btn-ghost btn-sm recipe-delete", onclick: move |_| confirm_delete.set(true),
                                            "Delete recipe" }
                                    }
                                }
                            } else {
                                div { class: "card-body", Empty {
                                    title: "Select a recipe".to_owned(),
                                    description: "Recipes and their steps appear here.".to_owned()
                                } }
                            }
                        }
                    }
                }
            },
            Tab::Plan => rsx! {
                section { class: "card",
                    div { class: "card-header",
                        h2 { class: "card-title", "Planned meals" }
                        p { class: "card-description", "Scheduled portions determine the shopping list." }
                    }
                    div { class: "card-body",
                        if let Some(Err(message)) = meals.cloned() {
                            Notice { message }
                        } else if meals.cloned().is_none() {
                            Loading {}
                        } else if planned.is_empty() {
                            Empty { title: "No meals scheduled".to_owned(),
                                description: "Choose a recipe and date in the Library.".to_owned() }
                        } else {
                            div { class: "table-clip",
                                table { class: "table",
                                    thead { tr { th { "Date" } th { "Recipe" } th { "Meal" } th { "Portions" } th { " " } } }
                                    tbody {
                                        for meal in planned {
                                            tr { key: "{meal.id}",
                                                td { "{meal.date}" }
                                                td { {records.iter().find(|r|r.id==meal.recipe_id)
                                                    .map(|r|r.title.clone()).unwrap_or_else(||meal.recipe_id.clone())} }
                                                td { class: "muted", "{meal.meal_type}" }
                                                td { "{meal.servings}" }
                                                td {
                                                    button { class: "btn btn-ghost btn-sm", disabled: busy(),
                                                        onclick: move |_| {
                                                            let path = format!("/v1/meals/{}", meal.id);
                                                            let csrf = csrf(); busy.set(true);
                                                            spawn(async move {
                                                                match api::remove(&path,&csrf,None).await {
                                                                    Ok(()) => { meals.restart(); groceries.restart(); }
                                                                    Err(message) => error.set(message),
                                                                }
                                                                busy.set(false);
                                                            });
                                                        }, "Remove" }
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
            Tab::Groceries => rsx! {
                section { class: "card",
                    div { class: "card-header",
                        h2 { class: "card-title", "Shopping list" }
                        p { class: "card-description",
                            "Combined ingredients from scheduled meals. Units are kept separate when they differ." }
                    }
                    div { class: "card-body",
                        if let Some(Err(message)) = groceries.cloned() {
                            Notice { message }
                        } else if groceries.cloned().is_none() {
                            Loading {}
                        } else if shopping.is_empty() {
                            Empty { title: "No groceries required".to_owned(),
                                description: "Schedule meals to produce an ingredient list.".to_owned() }
                        } else {
                            ul { class: "recipe-shopping",
                                for item in shopping {
                                    li { key: "{item.key}",
                                        button {
                                            class: "recipe-grocery-check",
                                            aria_label: format!("Toggle purchased status for {}", item.name),
                                            aria_pressed: if item.checked { "true" } else { "false" },
                                            disabled: busy(),
                                            onclick: {
                                                let key = item.key.clone();
                                                let checked = !item.checked;
                                                move |_| {
                                                    let token=csrf();
                                                    let path=format!("/v1/groceries/items/{key}");
                                                    busy.set(true);error.set(String::new());
                                                    spawn(async move {
                                                        match api::write::<GroceryBoard,_>(Method::PUT,&path,&token,
                                                            &serde_json::json!({"checked":checked,"expected_revision":grocery_revision})).await {
                                                            Ok(_) => groceries.restart(),
                                                            Err(message) => {error.set(message);groceries.restart();},
                                                        }
                                                        busy.set(false);
                                                    });
                                                }
                                            },
                                            if item.checked { "✓" } else { "○" }
                                        }
                                        span { class: if item.checked {"recipe-purchased"} else {""}, "{item.name}" }
                                        strong { "{quantity(item.amount)} {item.unit}" }
                                    }
                                }
                            }
                            p { class: "small muted", "Purchase status is saved privately and synchronized through the recipe store." }
                        }
                    }
                }
            },
            Tab::Transfer => rsx! {
                section { class: "card",
                    div { class: "card-header",
                        h2 { class: "card-title", "Import from recipe markup" }
                        p { class: "card-description",
                            "Paste a page's HTML or schema.org JSON-LD. Review ingredient quantities before saving."
                        }
                    }
                    div { class: "card-body stack",
                        label { class: "form-row",
                            span { class: "label", "Recipe HTML or JSON-LD" }
                            textarea { class: "field textarea", rows: "5",
                                value: "{schema_text}", oninput: move |event| schema_text.set(event.value()) }
                        }
                        button { class: "btn btn-outline", disabled: busy() || schema_text().trim().is_empty(),
                            onclick: move |_| {
                                let source = schema_text();
                                let token = csrf();
                                busy.set(true);error.set(String::new());
                                spawn(async move {
                                    match api::write::<Recipe,_>(Method::POST,"/v1/recipes/parse",&token,
                                        &serde_json::json!({"source":source})).await {
                                        Ok(parsed) => {
                                            tags.set(parsed.tags.join(", "));equipment.set(parsed.equipment.join(", "));
                                            nutrients.set(nutrient_lines(&parsed));
                                            if let Some(ref n)=parsed.nutrition {
                                                nutrition_method.set(n.method.clone());
                                                nutrition_source.set(n.source.clone());
                                            } else {
                                                nutrition_method.set("estimated".to_owned());
                                                nutrition_source.set(String::new());
                                            }
                                            draft.set(parsed);
                                            editing.set(true);
                                            cooking.set(false);
                                            tab.set(Tab::Library);
                                            notice.set("Review all quantities before saving this recipe".to_owned());
                                            schema_text.set(String::new());
                                        }
                                        Err(message) => error.set(message),
                                    }
                                    busy.set(false);
                                });
                            },
                            "Preview imported recipe"
                        }
                    }
                }
                section { class: "card",
                    div { class: "card-header",
                        h2 { class: "card-title", "Portable recipe archive" }
                        p { class: "card-description", "Versioned first-party JSON. Imports preserve identifiers and never overwrite existing recipes." }
                    }
                    div { class: "card-body stack",
                        div { a { class: "btn btn-outline", href: "/v1/recipes/export", download: "recipes.json", "Export JSON" } }
                        div { class: "form-row",
                            label { class: "label", for: "recipe-import", "Paste a JSON archive" }
                            textarea { id: "recipe-import", class: "field textarea", rows: "8",
                                value: "{import_text}", oninput: move |event| import_text.set(event.value()) }
                        }
                        div { button { class: "btn btn-primary", disabled: busy() || import_text().trim().is_empty(),
                            onclick: move |_| {
                                let parsed: Result<serde_json::Value,_> = serde_json::from_str(&import_text());
                                let Ok(document) = parsed else { error.set("Invalid JSON document".to_owned()); return; };
                                let csrf = csrf(); busy.set(true); error.set(String::new());
                                spawn(async move {
                                    match api::write::<serde_json::Value,_>(Method::POST,"/v1/recipes/import",&csrf,&document).await {
                                        Ok(_) => { recipes.restart(); meals.restart(); groceries.restart(); notice.set("Archive imported".to_owned()); import_text.set(String::new()); },
                                        Err(message) => error.set(message),
                                    }
                                    busy.set(false);
                                });
                            }, "Import archive" }
                        }
                    }
                }
            },
        }
    }
}
