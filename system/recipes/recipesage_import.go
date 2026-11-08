package recipes

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"
)

const MaxRecipeSageExportBytes = 8 << 20
const maxRecipeSageEntries = 1000

type RecipeSageCandidate struct {
	Key      string   `json:"key"`
	Recipe   Recipe   `json:"recipe"`
	Status   string   `json:"status"`
	Warnings []string `json:"warnings,omitempty"`
}

type RecipeSagePreview struct {
	Digest     string                `json:"digest"`
	Total      int                   `json:"total"`
	Ready      int                   `json:"ready"`
	Candidates []RecipeSageCandidate `json:"candidates"`
}

type RecipeSageImportResult struct {
	Imported int      `json:"imported"`
	IDs      []string `json:"ids"`
}

func validRecipeSageRef(ref string) bool {
	const prefix = "recipesage:"
	if !strings.HasPrefix(ref, prefix) || len(ref) != len(prefix)+64 {
		return false
	}
	for _, digit := range ref[len(prefix):] {
		if (digit < '0' || digit > '9') && (digit < 'a' || digit > 'f') {
			return false
		}
	}
	return true
}

func recipeSageKey(record map[string]any, raw json.RawMessage) string {
	identifier := schemaString(record["identifier"])
	if identifier != "" {
		raw = []byte("identifier:" + identifier)
	} else {
		// Canonical JSON is stable across whitespace changes in the export.
		normalized, err := json.Marshal(record)
		if err == nil {
			raw = normalized
		}
	}
	digest := sha256.Sum256(raw)
	return "recipesage:" + hex.EncodeToString(digest[:])
}

func schemaCategory(value any) []string {
	var result []string
	switch entries := value.(type) {
	case string:
		for _, part := range strings.Split(entries, ",") {
			if label := strings.TrimSpace(part); label != "" {
				result = append(result, label)
			}
		}
	case []any:
		for _, item := range entries {
			if node, ok := item.(map[string]any); ok {
				item = node["name"]
			}
			if label := schemaString(item); label != "" {
				result = append(result, label)
			}
		}
	}
	return result
}

var sageNutritionAmount = regexp.MustCompile(`(?i)^\s*(\d+(?:\.\d+)?)\s*(kcal|g|mg|mcg)\s*$`)

func parseSageNutrition(value any) (*Nutrition, []string) {
	info, ok := value.(map[string]any)
	if !ok {
		values, isList := value.([]any)
		if !isList || len(values) == 0 {
			return nil, nil
		}
		info, ok = values[0].(map[string]any)
		if !ok {
			return nil, []string{"Unrecognized nutrition data requires manual review"}
		}
	}

	fields := []struct {
		Source, Name, Unit string
	}{
		{"calories", "calories", "kcal"},
		{"fatContent", "fat", "g"},
		{"saturatedFatContent", "saturated fat", "g"},
		{"transFatContent", "trans fat", "g"},
		{"unsaturatedFatContent", "unsaturated fat", "g"},
		{"cholesterolContent", "cholesterol", "mg"},
		{"sodiumContent", "sodium", "mg"},
		{"carbohydrateContent", "carbohydrate", "g"},
		{"fiberContent", "fiber", "g"},
		{"sugarContent", "sugars", "g"},
		{"proteinContent", "protein", "g"},
	}
	out := &Nutrition{Method: "reported", Source: "RecipeSage JSON-LD export"}
	var warnings []string
	for _, field := range fields {
		if info[field.Source] == nil {
			continue
		}
		raw := schemaString(info[field.Source])
		matched := sageNutritionAmount.FindStringSubmatch(raw)
		if len(matched) != 3 || !strings.EqualFold(matched[2], field.Unit) {
			warnings = append(warnings, "Unrecognized "+field.Name+" unit; verify nutrition manually")
			continue
		}
		amount, err := strconv.ParseFloat(matched[1], 64)
		if err != nil || math.IsInf(amount, 0) || math.IsNaN(amount) || amount < 0 {
			warnings = append(warnings, "Invalid "+field.Name+" amount; verify nutrition manually")
			continue
		}
		out.PerServing = append(out.PerServing, Nutrient{
			Name: field.Name, Amount: amount, Unit: field.Unit,
		})
	}
	if len(out.PerServing) == 0 {
		return nil, warnings
	}
	return out, warnings
}

func sageNotes(data map[string]any) string {
	var notes []string
	comments, _ := data["comment"].([]any)
	for _, entry := range comments {
		comment, ok := entry.(map[string]any)
		if ok && schemaString(comment["name"]) == "Author Notes" {
			if value := schemaString(comment["text"]); value != "" {
				notes = append(notes, value)
			}
		}
	}
	if credit := schemaString(data["creditText"]); credit != "" {
		notes = append(notes, "Credit: "+credit)
	}
	if rating, ok := data["aggregateRating"].(map[string]any); ok {
		if value := schemaString(rating["ratingValue"]); value != "" {
			notes = append(notes, "Original rating: "+value)
		}
	}
	if nutrition, ok := data["nutrition"].(map[string]any); ok {
		if description := schemaString(nutrition["description"]); description != "" {
			notes = append(notes, "Nutrition details: "+description)
		}
	}
	return strings.Join(notes, "\n\n")
}

func sageRecipe(data map[string]any) (Recipe, []string) {
	recipe := Recipe{
		Title:       schemaString(data["name"]),
		Summary:     schemaString(data["description"]),
		Servings:    1,
		PrepMinutes: schemaMinutes(data["prepTime"]),
		Tags:        schemaCategory(data["recipeCategory"]),
		Notes:       sageNotes(data),
		Steps:       schemaSteps(data["recipeInstructions"]),
	}
	var warnings []string
	if total := schemaMinutes(data["totalTime"]); total > 0 {
		if total >= recipe.PrepMinutes {
			recipe.CookMinutes = total - recipe.PrepMinutes
		} else {
			warnings = append(warnings, "Total time is shorter than preparation time")
		}
	} else {
		recipe.CookMinutes = schemaMinutes(data["cookTime"])
	}
	if raw := schemaString(data["recipeYield"]); raw != "" {
		parts := strings.Fields(raw)
		if len(parts) > 0 {
			if amount, ok := parseQuantity(parts[0]); ok {
				recipe.Servings = amount
			} else {
				warnings = append(warnings, "Serving yield needs confirmation")
			}
		}
	}
	for _, ingredient := range schemaStrings(data["recipeIngredient"]) {
		recipe.Ingredients = append(recipe.Ingredients, parseIngredient(ingredient))
	}
	if len(recipe.Ingredients) == 0 || len(recipe.Steps) == 0 {
		warnings = append(warnings, "Ingredients or instructions are missing")
	}
	for _, ingredient := range recipe.Ingredients {
		if ingredient.Amount == 0 {
			warnings = append(warnings, "At least one ingredient quantity needs review")
			break
		}
	}

	rawURL := schemaString(data["isBasedOn"])
	if rawURL == "" {
		rawURL = schemaString(data["url"])
	}
	if rawURL != "" {
		parsed, err := url.Parse(rawURL)
		if err == nil && parsed.Scheme == "https" && parsed.Hostname() != "" && parsed.User == nil {
			recipe.SourceURL = rawURL
		} else {
			recipe.Notes = strings.TrimSpace(recipe.Notes + "\n\nOriginal source: " + rawURL)
			warnings = append(warnings, "Original source URL was not HTTPS")
		}
	}
	if data["image"] != nil || data["images"] != nil {
		warnings = append(warnings, "External recipe images are not copied; upload the image separately")
	}
	nutrition, nutritionWarnings := parseSageNutrition(data["nutrition"])
	recipe.Nutrition = nutrition
	warnings = append(warnings, nutritionWarnings...)
	if _, ok := data["linkedRecipeIds"]; ok {
		warnings = append(warnings, "Linked RecipeSage recipes are not carried over")
	}
	return recipe, warnings
}

// ParseRecipeSageExport accepts RecipeSage's documented {"recipes":[...]} JSON-LD export.
// It never accesses the original RecipeSage service or writes to first-party storage.
func ParseRecipeSageExport(body []byte) (RecipeSagePreview, error) {
	if len(body) == 0 || len(body) > MaxRecipeSageExportBytes {
		return RecipeSagePreview{}, errors.New("RecipeSage export must be 8 MiB or smaller")
	}
	var document struct {
		Recipes []json.RawMessage `json:"recipes"`
	}
	if err := json.Unmarshal(body, &document); err != nil || document.Recipes == nil {
		return RecipeSagePreview{}, errors.New("expected a RecipeSage JSON-LD export with a recipes collection")
	}
	if len(document.Recipes) == 0 || len(document.Recipes) > maxRecipeSageEntries {
		return RecipeSagePreview{}, errors.New("RecipeSage export must contain 1 to 1000 recipes")
	}
	digest := sha256.Sum256(body)
	preview := RecipeSagePreview{
		Digest: hex.EncodeToString(digest[:]),
		Total: len(document.Recipes),
		Candidates: make([]RecipeSageCandidate, 0, len(document.Recipes)),
	}
	seen := make(map[string]bool, len(document.Recipes))
	for _, raw := range document.Recipes {
		var data map[string]any
		_ = json.Unmarshal(raw, &data)
		key := recipeSageKey(data, raw)
		candidate := RecipeSageCandidate{Key: key}
		if !schemaType(data["@type"]) {
			candidate.Status = "needs_review"
			candidate.Recipe.Title = schemaString(data["name"])
			candidate.Warnings = append(candidate.Warnings, "Entry is not a schema.org Recipe")
		} else {
			candidate.Recipe, candidate.Warnings = sageRecipe(data)
			candidate.Status = "ready"
			if err := validateRecipe(candidate.Recipe); err != nil {
				candidate.Status = "needs_review"
				candidate.Warnings = append(candidate.Warnings, "Recipe requires review: "+err.Error())
			}
		}
		if seen[key] {
			candidate.Status = "duplicate"
			candidate.Warnings = append(candidate.Warnings, "Repeated recipe identity in export")
		}
		seen[key] = true
		if candidate.Status == "ready" {
			preview.Ready++
		}
		preview.Candidates = append(preview.Candidates, candidate)
	}
	return preview, nil
}

// PreviewRecipeSage marks entries that were imported before without modifying data.
func (s *Store) PreviewRecipeSage(body []byte) (RecipeSagePreview, error) {
	preview, err := ParseRecipeSageExport(body)
	if err != nil {
		return RecipeSagePreview{}, err
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	existing := make(map[string]bool, len(s.data.Recipes))
	for _, recipe := range s.data.Recipes {
		if recipe.ImportRef != "" {
			existing[recipe.ImportRef] = true
		}
	}
	preview.Ready = 0
	for i := range preview.Candidates {
		candidate := &preview.Candidates[i]
		if existing[candidate.Key] {
			candidate.Status = "imported"
			candidate.Warnings = append(candidate.Warnings, "This RecipeSage record already exists in the library")
		}
		if candidate.Status == "ready" {
			preview.Ready++
		}
	}
	return preview, nil
}

// ImportRecipeSage re-parses the original document and creates only selected,
// valid records in one atomic transaction. A preview digest fences the file
// that the owner inspected, and source identities remain immutable after import.
func (s *Store) ImportRecipeSage(body []byte, digest string, keys []string) (RecipeSageImportResult, error) {
	preview, err := ParseRecipeSageExport(body)
	if err != nil {
		return RecipeSageImportResult{}, err
	}
	if digest == "" || preview.Digest != digest || len(keys) == 0 || len(keys) > preview.Ready {
		return RecipeSageImportResult{}, errors.New("recipe import requires an unchanged preview and selected valid entries")
	}
	selected := make(map[string]bool, len(keys))
	for _, key := range keys {
		if !validRecipeSageRef(key) || selected[key] {
			return RecipeSageImportResult{}, errors.New("invalid or repeated recipe selection")
		}
		selected[key] = true
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	existing := make(map[string]bool, len(s.data.Recipes))
	for _, recipe := range s.data.Recipes {
		if recipe.ImportRef != "" {
			existing[recipe.ImportRef] = true
		}
	}
	next := duplicate(s.data)
	result := RecipeSageImportResult{IDs: make([]string, 0, len(selected))}
	now := time.Now().UTC()
	for _, candidate := range preview.Candidates {
		if !selected[candidate.Key] {
			continue
		}
		if candidate.Status != "ready" {
			return RecipeSageImportResult{}, errors.New("selected recipe requires review")
		}
		if existing[candidate.Key] {
			return RecipeSageImportResult{}, ErrConflict
		}
		id, err := randomID()
		if err != nil {
			return RecipeSageImportResult{}, err
		}
		recipe := copyRecipe(candidate.Recipe)
		recipe.ID = id
		recipe.ImportRef = candidate.Key
		recipe.Revision = 1
		recipe.CreatedAt, recipe.UpdatedAt = now, now
		if err := validateRecipe(recipe); err != nil {
			return RecipeSageImportResult{}, fmt.Errorf("invalid imported recipe: %w", err)
		}
		next.Recipes[id] = recipe
		result.IDs = append(result.IDs, id)
	}
	if len(result.IDs) != len(selected) {
		return RecipeSageImportResult{}, errors.New("selected recipes were not present in the export")
	}
	if err := s.save(next); err != nil {
		return RecipeSageImportResult{}, err
	}
	result.Imported = len(result.IDs)
	return result, nil
}
