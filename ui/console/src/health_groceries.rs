use std::collections::{BTreeMap, BTreeSet};

use dioxus::prelude::*;
use serde::{Deserialize, Serialize};
use serde_json::{Value, json};

use crate::components::{Card, Empty, Heading, Loading, Notice};
use crate::health_tools::{self, Tool};

#[derive(Clone, Default, PartialEq, Serialize, Deserialize)]
#[serde(default)]
struct GroceryItem {
    id: String,
    name: String,
    quantity: f64,
    unit: String,
    checked: bool,
    recipe_id: String,
    #[serde(flatten)]
    extra: BTreeMap<String, Value>,
}

#[derive(Clone, Default, PartialEq, Serialize, Deserialize)]
#[serde(default)]
struct GroceryList {
    id: String,
    title: String,
    revision: i32,
    items: Vec<GroceryItem>,
    #[serde(flatten)]
    extra: BTreeMap<String, Value>,
}

fn validate(list: &GroceryList) -> Result<(), String> {
    if list.title.trim().is_empty() || list.title.len() > 200 || list.items.len() > 2000 {
        return Err("Enter a list title and no more than 2000 items.".to_owned());
    }
    let mut ids = BTreeSet::new();
    for item in &list.items {
        if item.id.is_empty()
            || item.id.len() > 128
            || !ids.insert(&item.id)
            || item.name.trim().is_empty()
            || item.name.len() > 200
            || item.unit.trim().is_empty()
            || !item.quantity.is_finite()
            || item.quantity <= 0.0
            || item.quantity > 1_000_000.0
        {
            return Err(
                "Every item requires a unique ID, a name, a positive quantity, and a unit."
                    .to_owned(),
            );
        }
    }
    Ok(())
}

fn add_item(list: &mut GroceryList) {
    let instant = js_sys::Date::now() as u64;
    let mut suffix = list.items.len();
    let id = loop {
        let candidate = format!("item-{instant}-{suffix}");
        if !list.items.iter().any(|item| item.id == candidate) {
            break candidate;
        }
        suffix += 1;
    };
    list.items.push(GroceryItem {
        id,
        quantity: 1.0,
        unit: "each".to_owned(),
        ..GroceryItem::default()
    });
}

#[component]
pub fn HealthGroceries(csrf: String) -> Element {
    let csrf = use_signal(|| csrf);
    let mut lists = use_resource(move || {
        let token = csrf();
        async move { health_tools::call::<Vec<GroceryList>>(&token, Tool::GroceryList, json!({})).await }
    });
    let mut meal_plans = use_resource(move || {
        let token = csrf();
        async move { health_tools::call::<Vec<Value>>(&token, Tool::PlanList, json!({})).await }
    });
    let mut selected_plan = use_signal(String::new);
    let mut draft = use_signal(|| None::<GroceryList>);
    let mut busy = use_signal(|| false);
    let mut error = use_signal(String::new);
    let mut status = use_signal(String::new);

    rsx! {
        Heading {
            title: "Health groceries".to_owned(),
            description: "Review planned ingredients and track shopping with versioned local lists.".to_owned(),
            button {
                class: "btn btn-outline",
                onclick: move |_| {
                    lists.restart();
                    meal_plans.restart();
                },
                "Refresh"
            }
        }
        if !error().is_empty() { Notice { message: error() } }
        Card {
            title: "Choose a grocery list".to_owned(),
            description: "Review and edit local Health lists. Unsaved edits are discarded when another list is selected.".to_owned(),
            div { class: "card-body health-grocery-controls",
                button {
                    class: "btn btn-primary", r#type: "button", disabled: busy(),
                    onclick: move |_| {
                        draft.set(Some(GroceryList {
                            title: "Groceries".to_owned(), ..GroceryList::default()
                        }));
                        error.set(String::new());
                        status.set(String::new());
                    },
                    "Create list"
                }
                match lists.cloned() {
                    None => rsx! { Loading {} },
                    Some(Err(message)) => rsx! { Notice { message } },
                    Some(Ok(records)) => rsx! {
                        if records.is_empty() {
                            Empty {
                                title: "No grocery lists".to_owned(),
                                description: "Create a list or derive one from a meal plan.".to_owned()
                            }
                        } else {
                            div { class: "health-plan-list",
                                for list in records {
                                    button {
                                        key: "{list.id}",
                                        class: if draft().as_ref().is_some_and(|item| item.id == list.id) {
                                            "btn btn-primary"
                                        } else {
                                            "btn btn-outline"
                                        },
                                        r#type: "button", disabled: busy(),
                                        onclick: {
                                            let saved = list.clone();
                                            move |_| {
                                                draft.set(Some(saved.clone()));
                                                error.set(String::new());
                                                status.set(String::new());
                                            }
                                        },
                                        "{list.title} · revision {list.revision}"
                                    }
                                }
                            }
                        }
                    },
                }
            }
        }
        Card {
            title: "From a meal plan".to_owned(),
            description: "Group recorded meal-plan ingredients. Preview and correct amounts before saving a grocery list.".to_owned(),
            div { class: "card-body health-grocery-controls",
                match meal_plans.cloned() {
                    None => rsx! { Loading {} },
                    Some(Err(message)) => rsx! { Notice { message } },
                    Some(Ok(plans)) => {
                        let meal_plans = plans.into_iter()
                            .filter(|plan| plan.get("kind").and_then(Value::as_str) == Some("meal"))
                            .map(|plan| (
                                plan.get("id").and_then(Value::as_str).unwrap_or("").to_owned(),
                                plan.get("title").and_then(Value::as_str).unwrap_or("Untitled").to_owned()
                            ))
                            .collect::<Vec<_>>();
                        rsx! {
                            if meal_plans.is_empty() {
                                Empty {
                                    title: "No meal plans".to_owned(),
                                    description: "Create a meal plan to group its ingredients.".to_owned()
                                }
                            } else {
                                label { class: "form-row",
                                    span { class: "label", "Meal plan" }
                                    select {
                                        class: "field", value: "{selected_plan()}",
                                        onchange: move |event| selected_plan.set(event.value()),
                                        option { value: "", "Choose a meal plan" }
                                        for (plan_id, plan_title) in meal_plans {
                                            option { value: "{plan_id}", "{plan_title}" }
                                        }
                                    }
                                }
                                button {
                                    class: "btn btn-outline", r#type: "button",
                                    disabled: busy() || selected_plan().is_empty(),
                                    onclick: move |_| {
                                        let id = selected_plan();
                                        if id.is_empty() { return; }
                                        let token = csrf();
                                        busy.set(true);
                                        error.set(String::new());
                                        spawn(async move {
                                            match health_tools::call::<Vec<GroceryItem>>(
                                                &token, Tool::GroceryFromPlan, json!({"plan_id":id}),
                                            ).await {
                                                Ok(items) => {
                                                    draft.set(Some(GroceryList {
                                                        title: "Meal plan groceries".to_owned(),
                                                        items, ..GroceryList::default()
                                                    }));
                                                    status.set("Review the ingredient amounts before saving.".to_owned());
                                                }
                                                Err(message) => error.set(message),
                                            }
                                            busy.set(false);
                                        });
                                    },
                                    "Preview ingredients"
                                }
                            }
                        }
                    }
                }
            }
        }
        if let Some(list) = draft() {
            Card {
                title: "List editor".to_owned(),
                description: "Purchase status and quantities remain local drafts until saved.".to_owned(),
                div { class: "card-body health-grocery-editor",
                    label { class: "form-row",
                        span { class: "label", "List title" }
                        input {
                            class: "field", required: true, maxlength: "200",
                            disabled: busy(), value: "{list.title}",
                            oninput: move |event| {
                                if let Some(ref mut list) = *draft.write() {
                                    list.title = event.value();
                                }
                            }
                        }
                    }
                    p { class: "small muted",
                        if list.revision == 0 {
                            "Unsaved list"
                        } else {
                            "Revision {list.revision}"
                        }
                    }
                    for (index, item) in list.items.iter().enumerate() {
                        div { class: "health-grocery-item", key: "{item.id}",
                            label { class: "form-row",
                                span { class: "label", "Purchased" }
                                input {
                                    r#type: "checkbox", checked: item.checked,
                                    disabled: busy(), aria_label: "Mark {item.name} as purchased",
                                    onchange: move |event| {
                                        if let Some(ref mut list) = *draft.write() {
                                            if let Some(item) = list.items.get_mut(index) {
                                                item.checked = event.checked();
                                            }
                                        }
                                    }
                                }
                            }
                            label { class: "form-row",
                                span { class: "label", "Item" }
                                input {
                                    class: "field", required: true, disabled: busy(),
                                    value: "{item.name}",
                                    oninput: move |event| {
                                        if let Some(ref mut list) = *draft.write() {
                                            if let Some(item) = list.items.get_mut(index) {
                                                item.name = event.value();
                                            }
                                        }
                                    }
                                }
                            }
                            label { class: "form-row",
                                span { class: "label", "Quantity" }
                                input {
                                    class: "field", r#type: "number", min: "0.001", step: "any",
                                    required: true, disabled: busy(), value: "{item.quantity}",
                                    oninput: move |event| {
                                        if let Ok(quantity) = event.value().parse::<f64>() {
                                            if let Some(ref mut list) = *draft.write() {
                                                if let Some(item) = list.items.get_mut(index) {
                                                    item.quantity = quantity;
                                                }
                                            }
                                        }
                                    }
                                }
                            }
                            label { class: "form-row",
                                span { class: "label", "Unit" }
                                input {
                                    class: "field", required: true, disabled: busy(),
                                    value: "{item.unit}",
                                    oninput: move |event| {
                                        if let Some(ref mut list) = *draft.write() {
                                            if let Some(item) = list.items.get_mut(index) {
                                                item.unit = event.value();
                                            }
                                        }
                                    }
                                }
                            }
                            label { class: "form-row",
                                span { class: "label", "Recipe ID (optional)" }
                                input {
                                    class: "field", disabled: busy(), value: "{item.recipe_id}",
                                    oninput: move |event| {
                                        if let Some(ref mut list) = *draft.write() {
                                            if let Some(item) = list.items.get_mut(index) {
                                                item.recipe_id = event.value();
                                            }
                                        }
                                    }
                                }
                            }
                            button {
                                class: "btn btn-outline btn-sm", r#type: "button", disabled: busy(),
                                onclick: move |_| {
                                    if let Some(ref mut list) = *draft.write() {
                                        if index < list.items.len() { list.items.remove(index); }
                                    }
                                },
                                "Remove"
                            }
                        }
                    }
                    div { class: "health-plan-actions",
                        button {
                            class: "btn btn-outline", r#type: "button", disabled: busy(),
                            onclick: move |_| {
                                if let Some(ref mut list) = *draft.write() {
                                    add_item(list);
                                }
                            },
                            "Add item"
                        }
                        button {
                            class: "btn btn-primary", r#type: "button", disabled: busy(),
                            onclick: move |_| {
                                let Some(list) = draft() else { return };
                                if let Err(message) = validate(&list) {
                                    error.set(message);
                                    return;
                                }
                                let token = csrf();
                                let payload = json!({
                                    "id": list.id, "title": list.title, "items": list.items,
                                    "expected_revision": list.revision,
                                });
                                error.set(String::new());
                                busy.set(true);
                                spawn(async move {
                                    match health_tools::call::<GroceryList>(
                                        &token, Tool::GrocerySave, payload,
                                    ).await {
                                        Ok(saved) => {
                                            status.set(format!("Grocery list saved at revision {}.", saved.revision));
                                            draft.set(Some(saved));
                                            lists.restart();
                                        }
                                        Err(message) => error.set(format!(
                                            "Could not save this revision: {message}. Refresh before retrying."
                                        )),
                                    }
                                    busy.set(false);
                                });
                            },
                            if busy() { "Saving…" } else { "Save list" }
                        }
                    }
                    p { class: "small muted", role: "status", aria_live: "polite", "{status()}" }
                }
            }
        }
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn prevents_invalid_or_duplicate_shopping_items() {
        let mut list = GroceryList {
            title: "Weekly groceries".to_owned(),
            items: vec![GroceryItem {
                id: "item-1".to_owned(),
                name: "Oats".to_owned(),
                quantity: 1.5,
                unit: "kg".to_owned(),
                ..GroceryItem::default()
            }],
            ..GroceryList::default()
        };
        assert!(validate(&list).is_ok());
        list.items.push(list.items[0].clone());
        assert!(validate(&list).is_err());
        list.items.pop();
        list.items[0].quantity = f64::NAN;
        assert!(validate(&list).is_err());
    }

    #[test]
    fn recipe_links_and_unknown_fields_roundtrip() {
        let item: GroceryItem = serde_json::from_value(json!({
            "id":"item-1", "name":"Oats", "quantity":1.0, "unit":"kg",
            "checked":true, "recipe_id":"recipe-1", "source_note":"manual"
        }))
        .unwrap();
        let encoded = serde_json::to_value(item).unwrap();
        assert_eq!(encoded["source_note"], "manual");
        assert_eq!(encoded["recipe_id"], "recipe-1");
        assert_eq!(encoded["checked"], true);
    }
}
