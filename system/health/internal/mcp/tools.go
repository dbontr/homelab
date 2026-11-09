package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/netip"
	"net/url"
	"strings"
	"time"

	"github.com/dbontr/homelab-new/system/health/internal/recipesage"
)

type Tool struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	InputSchema map[string]any `json:"inputSchema"`
	Annotations map[string]any `json:"annotations,omitempty"`
 Meta map[string]any `json:"_meta,omitempty"`
}
type toolEntry struct {
	Tool
	run func(context.Context, map[string]json.RawMessage) (json.RawMessage, error)
}

func field(typ, desc string) map[string]any { return map[string]any{"type": typ, "description": desc} }
func strs(desc string) map[string]any {
	return map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "description": desc}
}
func selectField(desc string, options ...string) map[string]any {
	return map[string]any{"type": "string", "description": desc, "enum": options}
}
func schema(required []string, fields map[string]any) map[string]any {
	if required == nil {
		required = []string{}
	}
	return map[string]any{"type": "object", "properties": fields, "required": required, "additionalProperties": false}
}
func (s *Server) add(name, description string, readOnly, destructive bool, required []string, fields map[string]any, run func(context.Context, map[string]json.RawMessage) (json.RawMessage, error)) {
	s.tools[name] = toolEntry{Tool: Tool{Name: name, Description: description, InputSchema: schema(required, fields), Annotations: map[string]any{"readOnlyHint": readOnly, "destructiveHint": destructive, "openWorldHint": false}}, run: run}
}
func (s *Server) post(ctx context.Context, path string, body any) (json.RawMessage, error) {
	return s.client.Request(ctx, http.MethodPost, path, body)
}
func (s *Server) get(ctx context.Context, path, param string, args map[string]json.RawMessage) (json.RawMessage, error) {
	id, err := requiredString(args, param)
	if err != nil {
		return nil, err
	}
	return s.client.Request(ctx, http.MethodGet, path+"?"+param+"="+url.QueryEscape(id), nil)
}
func requiredString(args map[string]json.RawMessage, name string) (string, error) {
	var value string
	if err := json.Unmarshal(args[name], &value); err != nil || strings.TrimSpace(value) == "" {
		return "", fmt.Errorf("%s must be a nonempty string", name)
	}
	return value, nil
}
func optionalString(args map[string]json.RawMessage, name, def string) string {
	raw, ok := args[name]
	if !ok {
		return def
	}
	var value string
	if err := json.Unmarshal(raw, &value); err != nil {
		return def
	}
	return value
}
func boundedInt(args map[string]json.RawMessage, name string, def, min, max int) (int, error) {
	raw, ok := args[name]
	if !ok {
		return def, nil
	}
	var n int
	if err := json.Unmarshal(raw, &n); err != nil || n < min || n > max {
		return 0, fmt.Errorf("%s must be between %d and %d", name, min, max)
	}
	return n, nil
}
func object(args map[string]json.RawMessage, name string) (map[string]json.RawMessage, error) {
	var result map[string]json.RawMessage
	if err := json.Unmarshal(args[name], &result); err != nil || result == nil {
		return nil, fmt.Errorf("%s must be an object", name)
	}
	return result, nil
}
func stringArray(args map[string]json.RawMessage, name string, min, max int) ([]string, error) {
	var values []string
	if err := json.Unmarshal(args[name], &values); err != nil || len(values) < min || len(values) > max {
		return nil, fmt.Errorf("%s must contain %d to %d strings", name, min, max)
	}
	for _, v := range values {
		if strings.TrimSpace(v) == "" {
			return nil, fmt.Errorf("%s contains a blank string", name)
		}
	}
	return values, nil
}
func confirmed(args map[string]json.RawMessage) error {
	var ok bool
	if err := json.Unmarshal(args["confirm"], &ok); err != nil || !ok {
		return errors.New("deletion requires confirm=true")
	}
	return nil
}
func nestedResponse(a string, left json.RawMessage, b string, right json.RawMessage) (json.RawMessage, error) {
	return json.Marshal(map[string]json.RawMessage{a: left, b: right})
}
func safeURL(args map[string]json.RawMessage) (string, error) {
	link, err := requiredString(args, "url")
	if err != nil {
		return "", err
	}
	u, err := url.Parse(link)
	if err != nil || u.Host == "" || (u.Scheme != "https" && u.Scheme != "http") || u.User != nil {
		return "", errors.New("url must be an HTTP(S) URL without credentials")
	}
	host := strings.ToLower(u.Hostname())
	if host == "localhost" || strings.HasSuffix(host, ".localhost") || strings.HasSuffix(host, ".local") || strings.HasSuffix(host, ".internal") {
		return "", errors.New("url must not point to a private address")
	}
	if ip, err := netip.ParseAddr(host); err == nil && (ip.IsPrivate() || ip.IsLoopback() || ip.IsLinkLocalUnicast() || ip.IsUnspecified() || ip.IsMulticast()) {
		return "", errors.New("url must not point to a private address")
	}
	return link, nil
}
func (s *Server) registerTools() {
	id := field("string", "RecipeSage ID")
	txt := func(desc string) map[string]any { return field("string", desc) }
	folder := selectField("Folder", "main", "inbox")
	recipeFields := map[string]any{"title": txt("Recipe title"), "description": txt("Description"), "yield": txt("Yield or servings"), "ingredients": txt("Newline-separated ingredients"), "instructions": txt("Cooking instructions"), "notes": txt("Personal notes"), "activeTime": txt("Active preparation time"), "totalTime": txt("Total time"), "source": txt("Source"), "url": txt("Source URL"), "folder": folder, "rating": map[string]any{"type": []string{"number", "null"}}, "labelIds": strs("Recipe label IDs"), "imageIds": strs("Existing image IDs"), "linkedRecipeIds": strs("Linked recipe IDs"), "lastMadeAt": txt("RFC3339 timestamp")}
	// Keep the input schema aligned with the official RecipeSage update fields.
	for _, key := range []string{"nutritionCalories", "nutritionTotalFat", "nutritionSaturatedFat", "nutritionTransFat", "nutritionPolyunsaturatedFat", "nutritionMonounsaturatedFat", "nutritionCholesterol", "nutritionSodium", "nutritionTotalCarbs", "nutritionDietaryFiber", "nutritionTotalSugars", "nutritionAddedSugars", "nutritionProtein", "nutritionVitaminD", "nutritionCalcium", "nutritionIron", "nutritionPotassium"} {
		recipeFields[key] = map[string]any{"type": []string{"number", "null"}}
	}
	for _, key := range []string{"nutritionServingSize", "nutritionOtherDetails"} {
		recipeFields[key] = map[string]any{"type": []string{"string", "null"}}
	}
	s.add("recipe_get", "Retrieve the full recipe with ingredients and instructions.", true, false, []string{"id"}, map[string]any{"id": id}, func(ctx context.Context, a map[string]json.RawMessage) (json.RawMessage, error) {
		return s.get(ctx, "/compat/v2/recipes/getRecipe", "id", a)
	})
	s.add("recipe_list", "List recipes by folder, sorted and paginated.", true, false, nil, map[string]any{"folder": folder, "limit": field("integer", "1 to 200"), "offset": field("integer", "Zero-based offset"), "orderBy": selectField("Sort field", "title", "createdAt", "updatedAt", "lastMadeAt"), "orderDirection": selectField("Sort order", "asc", "desc")}, func(ctx context.Context, a map[string]json.RawMessage) (json.RawMessage, error) {
		limit, e := boundedInt(a, "limit", 50, 1, 200)
		if e != nil {
			return nil, e
		}
		offset, e := boundedInt(a, "offset", 0, 0, 100000000)
		if e != nil {
			return nil, e
		}
		return s.post(ctx, "/compat/v2/recipes/getRecipes", map[string]any{"folder": optionalString(a, "folder", "main"), "limit": limit, "offset": offset, "orderBy": optionalString(a, "orderBy", "updatedAt"), "orderDirection": optionalString(a, "orderDirection", "desc")})
	})
	s.add("recipe_search", "Find recipes by text.", true, false, []string{"query"}, map[string]any{"query": txt("Search term"), "folder": folder}, func(ctx context.Context, a map[string]json.RawMessage) (json.RawMessage, error) {
		q, e := requiredString(a, "query")
		if e != nil {
			return nil, e
		}
		return s.post(ctx, "/compat/v2/recipes/searchRecipes", map[string]any{"searchTerm": q, "folder": optionalString(a, "folder", "main")})
	})
	s.add("recipe_find_by_ingredients", "Find recipes containing the given ingredients.", true, false, []string{"ingredients"}, map[string]any{"ingredients": strs("One to 30 ingredients")}, func(ctx context.Context, a map[string]json.RawMessage) (json.RawMessage, error) {
		list, e := stringArray(a, "ingredients", 1, 30)
		if e != nil {
			return nil, e
		}
		return s.post(ctx, "/compat/v2/recipes/searchRecipesByIngredients", map[string]any{"ingredients": list})
	})
	s.add("recipe_manifest", "List visible recipe IDs with modification timestamps for synchronization.", true, false, nil, map[string]any{}, func(ctx context.Context, a map[string]json.RawMessage) (json.RawMessage, error) {
		return s.client.Request(ctx, http.MethodGet, "/compat/v2/recipes/getAllVisibleRecipesManifest", nil)
	})
	s.add("recipe_count", "Count recipes in a specified folder.", true, false, nil, map[string]any{"folder": folder}, func(ctx context.Context, a map[string]json.RawMessage) (json.RawMessage, error) {
		return s.client.Request(ctx, http.MethodGet, "/compat/v2/recipes/getRecipeCount?folder="+url.QueryEscape(optionalString(a, "folder", "main")), nil)
	})
	s.add("recipe_create", "Create a recipe. Missing optional text fields default to empty strings.", false, false, []string{"title"}, recipeFields, func(ctx context.Context, a map[string]json.RawMessage) (json.RawMessage, error) {
		payload, e := recipesage.PrepareRecipe(a)
		if e != nil {
			return nil, e
		}
		return s.post(ctx, "/compat/v2/recipes/createRecipe", payload)
	})
	s.add("recipe_edit", "Change only specified recipe fields. Other values, images, links, and labels are preserved.", false, false, []string{"id", "changes"}, map[string]any{"id": id, "changes": map[string]any{"type": "object", "description": "Fields to replace", "properties": recipeFields, "additionalProperties": false, "minProperties": 1}}, func(ctx context.Context, a map[string]json.RawMessage) (json.RawMessage, error) {
		id, e := requiredString(a, "id")
		if e != nil {
			return nil, e
		}
		changes, e := object(a, "changes")
		if e != nil {
			return nil, e
		}
		if len(changes) == 0 {
			return nil, errors.New("changes must not be empty")
		}
		return s.client.RecipeUpdate(ctx, id, changes)
	})
	s.add("recipe_mark_cooked", "Set the recipe's last-prepared timestamp. RecipeSage stores only the latest timestamp, not a history.", false, false, []string{"id"}, map[string]any{"id": id, "cookedAt": txt("RFC3339 timestamp, defaults to now")}, func(ctx context.Context, a map[string]json.RawMessage) (json.RawMessage, error) {
		id, e := requiredString(a, "id")
		if e != nil {
			return nil, e
		}
		date := optionalString(a, "cookedAt", time.Now().UTC().Format(time.RFC3339))
		if _, e = time.Parse(time.RFC3339, date); e != nil {
			return nil, errors.New("cookedAt must be RFC3339")
		}
		raw, _ := json.Marshal(date)
		return s.client.RecipeUpdate(ctx, id, map[string]json.RawMessage{"lastMadeAt": raw})
	})
	s.add("recipe_delete", "Permanently delete a recipe. Explicit confirmation is required.", false, true, []string{"id", "confirm"}, map[string]any{"id": id, "confirm": field("boolean", "Must be true")}, func(ctx context.Context, a map[string]json.RawMessage) (json.RawMessage, error) {
		id, e := requiredString(a, "id")
		if e != nil {
			return nil, e
		}
		if e = confirmed(a); e != nil {
			return nil, e
		}
		return s.post(ctx, "/compat/v2/recipes/deleteRecipe", map[string]any{"id": id})
	})
	s.add("recipe_clip_preview", "Extract recipe details from a URL without saving.", true, false, []string{"url"}, map[string]any{"url": txt("Public recipe URL")}, func(ctx context.Context, a map[string]json.RawMessage) (json.RawMessage, error) {
		link, e := safeURL(a)
		if e != nil {
			return nil, e
		}
		return s.client.Request(ctx, http.MethodGet, "/clip?url="+url.QueryEscape(link), nil)
	})
	s.add("recipe_import_url", "Extract a recipe from a URL and save it in RecipeSage.", false, false, []string{"url"}, map[string]any{"url": txt("Public recipe URL"), "folder": folder}, func(ctx context.Context, a map[string]json.RawMessage) (json.RawMessage, error) {
		link, e := safeURL(a)
		if e != nil {
			return nil, e
		}
		clipped, e := s.client.Request(ctx, http.MethodGet, "/clip?url="+url.QueryEscape(link), nil)
		if e != nil {
			return nil, e
		}
		var values map[string]json.RawMessage
		if e = json.Unmarshal(clipped, &values); e != nil {
			return nil, fmt.Errorf("invalid clip response: %w", e)
		}
		// Keep only fields accepted by the createRecipe endpoint.
		allowed := map[string]json.RawMessage{}
		for key, value := range values {
			if recipesage.IsRecipeField(key) {
				allowed[key] = value
			}
		}
		if _, ok := allowed["url"]; !ok {
			allowed["url"], _ = json.Marshal(link)
		}
		if f, ok := a["folder"]; ok {
			allowed["folder"] = f
		}
		payload, e := recipesage.PrepareRecipe(allowed)
		if e != nil {
			return nil, e
		}
		return s.post(ctx, "/compat/v2/recipes/createRecipe", payload)
	})
	s.add("recipe_labels", "List recipe labels.", true, false, nil, map[string]any{}, func(ctx context.Context, a map[string]json.RawMessage) (json.RawMessage, error) {
		return s.client.Request(ctx, http.MethodGet, "/compat/v2/labels/getLabels", nil)
	})
	s.add("recipe_label_upsert", "Create a label or attach existing label title to recipes.", false, false, []string{"title"}, map[string]any{"title": txt("Label title"), "labelGroupId": id, "addToRecipeIds": strs("Recipe IDs to label")}, func(ctx context.Context, a map[string]json.RawMessage) (json.RawMessage, error) {
		title, e := requiredString(a, "title")
		if e != nil {
			return nil, e
		}
		body := map[string]any{"title": title, "addToRecipeIds": nil}
		if value, ok := a["addToRecipeIds"]; ok {
			body["addToRecipeIds"] = value
		}
		if value, ok := a["labelGroupId"]; ok {
			body["labelGroupId"] = value
		}
		return s.post(ctx, "/compat/v2/labels/upsertLabel", body)
	})
	s.add("recipe_label_update", "Rename an existing label or move it to another label group.", false, false, []string{"id"}, map[string]any{"id": id, "title": txt("New title"), "labelGroupId": id}, func(ctx context.Context, a map[string]json.RawMessage) (json.RawMessage, error) {
		item, e := requiredString(a, "id")
		if e != nil {
			return nil, e
		}
		body := map[string]any{"id": item}
		for _, key := range []string{"title", "labelGroupId"} {
			if value, ok := a[key]; ok {
				body[key] = value
			}
		}
		if len(body) == 1 {
			return nil, errors.New("provide title or labelGroupId")
		}
		return s.post(ctx, "/compat/v2/labels/updateLabel", body)
	})
	s.add("recipe_account_stats", "Retrieve the account's RecipeSage recipe statistics.", true, false, nil, map[string]any{}, func(ctx context.Context, a map[string]json.RawMessage) (json.RawMessage, error) {
		return s.client.Request(ctx, http.MethodGet, "/compat/v2/users/getMyStats", nil)
	})
	s.add("meal_plan_list", "List all meal plans.", true, false, nil, map[string]any{}, func(ctx context.Context, a map[string]json.RawMessage) (json.RawMessage, error) {
		return s.client.Request(ctx, http.MethodGet, "/compat/v2/mealPlans/getMealPlans", nil)
	})
	s.add("meal_plan_get", "Read one meal plan and its items.", true, false, []string{"id"}, map[string]any{"id": id}, func(ctx context.Context, a map[string]json.RawMessage) (json.RawMessage, error) {
		id, e := requiredString(a, "id")
		if e != nil {
			return nil, e
		}
		plan, e := s.client.Request(ctx, http.MethodGet, "/compat/v2/mealPlans/getMealPlan?id="+url.QueryEscape(id), nil)
		if e != nil {
			return nil, e
		}
		items, e := s.client.Request(ctx, http.MethodGet, "/compat/v2/mealPlans/getMealPlanItems?mealPlanId="+url.QueryEscape(id), nil)
		if e != nil {
			return nil, e
		}
		return nestedResponse("mealPlan", plan, "items", items)
	})
	s.add("meal_plan_create", "Create an empty meal plan.", false, false, []string{"title"}, map[string]any{"title": txt("Meal plan title")}, func(ctx context.Context, a map[string]json.RawMessage) (json.RawMessage, error) {
		title, e := requiredString(a, "title")
		if e != nil {
			return nil, e
		}
		return s.post(ctx, "/compat/v2/mealPlans/createMealPlan", map[string]any{"title": title, "collaboratorUserIds": []string{}})
	})
	mealItem := map[string]any{"type": "object", "required": []string{"title", "scheduledDate", "meal"}, "properties": map[string]any{"title": txt("Meal description"), "scheduledDate": txt("Date YYYY-MM-DD"), "meal": txt("Breakfast, lunch, dinner, etc."), "recipeId": id, "notes": txt("Optional notes")}, "additionalProperties": false}
	s.add("meal_plan_add_items", "Schedule meals in an existing meal plan.", false, false, []string{"mealPlanId", "items"}, map[string]any{"mealPlanId": id, "items": map[string]any{"type": "array", "minItems": 1, "maxItems": 100, "items": mealItem}}, func(ctx context.Context, a map[string]json.RawMessage) (json.RawMessage, error) {
		return s.postItems(ctx, "/compat/v2/mealPlans/createMealPlanItems", a, "mealPlanId")
	})
	s.add("shopping_list_list", "List shopping lists.", true, false, nil, map[string]any{}, func(ctx context.Context, a map[string]json.RawMessage) (json.RawMessage, error) {
		return s.client.Request(ctx, http.MethodGet, "/compat/v2/shoppingLists/getShoppingLists", nil)
	})
	s.add("shopping_list_get", "Read a shopping list and its items.", true, false, []string{"id"}, map[string]any{"id": id}, func(ctx context.Context, a map[string]json.RawMessage) (json.RawMessage, error) {
		id, e := requiredString(a, "id")
		if e != nil {
			return nil, e
		}
		list, e := s.client.Request(ctx, http.MethodGet, "/compat/v2/shoppingLists/getShoppingList?id="+url.QueryEscape(id), nil)
		if e != nil {
			return nil, e
		}
		items, e := s.client.Request(ctx, http.MethodGet, "/compat/v2/shoppingLists/getShoppingListItems?shoppingListId="+url.QueryEscape(id), nil)
		if e != nil {
			return nil, e
		}
		return nestedResponse("shoppingList", list, "items", items)
	})
	s.add("shopping_list_create", "Create an empty shopping list.", false, false, []string{"title"}, map[string]any{"title": txt("Shopping list title")}, func(ctx context.Context, a map[string]json.RawMessage) (json.RawMessage, error) {
		title, e := requiredString(a, "title")
		if e != nil {
			return nil, e
		}
		return s.post(ctx, "/compat/v2/shoppingLists/createShoppingList", map[string]any{"title": title, "collaboratorUserIds": []string{}})
	})
	shoppingItem := map[string]any{"type": "object", "required": []string{"title"}, "properties": map[string]any{"title": txt("Ingredient or shopping item"), "recipeId": id, "categoryTitle": txt("Category"), "completed": field("boolean", "Already purchased")}, "additionalProperties": false}
	s.add("shopping_list_add_items", "Add one or more items to a shopping list.", false, false, []string{"shoppingListId", "items"}, map[string]any{"shoppingListId": id, "items": map[string]any{"type": "array", "minItems": 1, "maxItems": 100, "items": shoppingItem}}, func(ctx context.Context, a map[string]json.RawMessage) (json.RawMessage, error) {
		return s.postItems(ctx, "/compat/v2/shoppingLists/createShoppingListItems", a, "shoppingListId")
	})
	s.add("shopping_list_set_completed", "Mark an item as completed or incomplete.", false, false, []string{"shoppingListId", "itemId", "completed"}, map[string]any{"shoppingListId": id, "itemId": id, "completed": field("boolean", "Whether the item is purchased")}, func(ctx context.Context, a map[string]json.RawMessage) (json.RawMessage, error) {
		list, e := requiredString(a, "shoppingListId")
		if e != nil {
			return nil, e
		}
		item, e := requiredString(a, "itemId")
		if e != nil {
			return nil, e
		}
		raw, ok := a["completed"]
		if !ok {
			return nil, errors.New("completed is required")
		}
		var status bool
		if e = json.Unmarshal(raw, &status); e != nil {
			return nil, errors.New("completed must be boolean")
		}
		return s.post(ctx, "/compat/v2/shoppingLists/updateShoppingListItems", map[string]any{"shoppingListId": list, "items": []any{map[string]any{"id": item, "completed": status}}})
	})
	s.add("shopping_list_delete_items", "Permanently delete shopping-list items. Confirmation required.", false, true, []string{"shoppingListId", "ids", "confirm"}, map[string]any{"shoppingListId": id, "ids": strs("Item identifiers"), "confirm": field("boolean", "Must be true")}, func(ctx context.Context, a map[string]json.RawMessage) (json.RawMessage, error) {
		list, e := requiredString(a, "shoppingListId")
		if e != nil {
			return nil, e
		}
		ids, e := stringArray(a, "ids", 1, 100)
		if e != nil {
			return nil, e
		}
		if e = confirmed(a); e != nil {
			return nil, e
		}
		return s.post(ctx, "/compat/v2/shoppingLists/deleteShoppingListItems", map[string]any{"shoppingListId": list, "ids": ids})
	})
}

func (s *Server) postItems(ctx context.Context, path string, args map[string]json.RawMessage, key string) (json.RawMessage, error) {
	if _, err := requiredString(args, key); err != nil {
		return nil, err
	}
	var entries []map[string]json.RawMessage
	if err := json.Unmarshal(args["items"], &entries); err != nil || len(entries) == 0 || len(entries) > 100 {
		return nil, errors.New("items must contain 1 to 100 objects")
	}
	for _, entry := range entries {
		if _, err := requiredString(entry, "title"); err != nil {
			return nil, err
		}
		// Both bulk-create endpoints require recipeId even when there is no linked recipe.
		if _, ok := entry["recipeId"]; !ok {
			entry["recipeId"] = json.RawMessage("null")
		}
		if key == "mealPlanId" {
			meal, err := requiredString(entry, "meal")
			if err != nil {
				return nil, err
			}
			_ = meal
			date, err := requiredString(entry, "scheduledDate")
			if err != nil {
				return nil, err
			}
			if _, err := time.Parse("2006-01-02", date); err != nil {
				return nil, errors.New("scheduledDate must be YYYY-MM-DD")
			}
		}
	}
	return s.post(ctx, path, args)
}
