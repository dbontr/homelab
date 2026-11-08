package recipes

import (
	"encoding/json"
	"errors"
	"path/filepath"
	"strings"
	"testing"
)

func recipeSageFixture(t *testing.T, records ...map[string]any) []byte {
	t.Helper()
	body, err := json.Marshal(map[string]any{"recipes": records})
	if err != nil {
		t.Fatal(err)
	}
	return body
}

func sageRecipeRecord(id, title string, ingredients ...string) map[string]any {
	return map[string]any{
		"@context": "http://schema.org",
		"@type": "Recipe",
		"identifier": id,
		"name": title,
		"description": "A test recipe",
		"prepTime": "PT15M",
		"totalTime": "PT40M",
		"recipeYield": "2 servings",
		"recipeIngredient": ingredients,
		"recipeInstructions": []any{
			map[string]any{"@type": "HowToSection", "name": "Preparation"},
			map[string]any{"@type": "HowToStep", "text": "Combine and cook."},
		},
		"recipeCategory": []string{"Dinner", "Rice"},
		"creditText": "Original cook",
		"isBasedOn": "https://example.org/recipes/rice",
		"comment": []any{map[string]any{
			"@type": "Comment",
			"name": "Author Notes",
			"text": "Keep the heat low.",
		}},
		"nutrition": map[string]any{
			"@type": "NutritionInformation",
			"calories": "450 kcal",
			"proteinContent": "12 g",
			"sodiumContent": "300 mg",
			"description": "Vitamin D: 2 mcg",
		},
		"image": []any{"https://images.example.org/dish.png"},
	}
}

func TestRecipeSagePreviewAndSelectiveImport(t *testing.T) {
	db, err := Open(filepath.Join(t.TempDir(), "recipes.json"))
	if err != nil {
		t.Fatal(err)
	}
	valid := sageRecipeRecord("source-1", "Rice bowl", "2 cups rice", "1 tbsp oil")
	incomplete := sageRecipeRecord("source-2", "Rice bowl", "onion", "1 tbsp oil")
	body := recipeSageFixture(t, valid, incomplete)
	preview, err := db.PreviewRecipeSage(body)
	if err != nil {
		t.Fatal(err)
	}
	if preview.Total != 2 || preview.Ready != 1 ||
		preview.Candidates[0].Status != "ready" ||
		preview.Candidates[1].Status != "needs_review" {
		t.Fatalf("invalid preview classifications: %+v", preview)
	}
	first := preview.Candidates[0]
	if first.Recipe.PrepMinutes != 15 || first.Recipe.CookMinutes != 25 ||
		first.Recipe.Servings != 2 || first.Recipe.SourceURL != "https://example.org/recipes/rice" ||
		len(first.Recipe.Tags) != 2 || len(first.Recipe.Nutrition.PerServing) != 4 ||
		!strings.Contains(first.Recipe.Notes, "Keep the heat low") ||
		!strings.Contains(first.Recipe.Notes, "Original cook") ||
		!strings.Contains(first.Recipe.Notes, "Vitamin D") {
		t.Fatalf("source metadata missing: %+v", first.Recipe)
	}
	if first.Recipe.ImageRef != "" || len(first.Warnings) == 0 {
		t.Fatal("remote image was assigned or skipped without warning")
	}
	if got := db.List(""); len(got) != 0 {
		t.Fatal("preview changed the database")
	}
	if _, err := db.ImportRecipeSage(body, "invalid-digest", []string{first.Key}, nil); err == nil {
		t.Fatal("import accepted a stale preview digest")
	}
	if _, err := db.ImportRecipeSage(body, preview.Digest, []string{first.Key, preview.Candidates[1].Key}, nil); err == nil ||
		len(db.List("")) != 0 {
		t.Fatalf("unreviewed import was not atomic: %v", err)
	}
	result, err := db.ImportRecipeSage(body, preview.Digest, []string{first.Key}, nil)
	if err != nil || result.Imported != 1 || len(result.IDs) != 1 {
		t.Fatalf("selected import failed: %+v %v", result, err)
	}
	saved, err := db.Get(result.IDs[0])
	if err != nil || saved.ImportRef != first.Key || saved.Title != "Rice bowl" {
		t.Fatalf("imported identity mismatch: %+v %v", saved, err)
	}
	after, err := db.PreviewRecipeSage(body)
	if err != nil || after.Ready != 0 || after.Candidates[0].Status != "imported" {
		t.Fatalf("already-imported source not recognized: %+v %v", after, err)
	}
	if _, err := db.ImportRecipeSage(body, preview.Digest, []string{first.Key}, nil); !errors.Is(err, ErrConflict) {
		t.Fatalf("repeated source imported twice: %v", err)
	}
	saved.Notes = "Modified independently"
	updated, err := db.Save(saved)
	if err != nil || updated.Revision != 2 || updated.ImportRef != first.Key {
		t.Fatalf("import reference lost on edit: %+v %v", updated, err)
	}
	updated.ImportRef = ""
	if _, err := db.Save(updated); !errors.Is(err, ErrConflict) {
		t.Fatalf("import provenance could be erased by editing: %v", err)
	}
	reopened, err := Open(db.path)
	if err != nil {
		t.Fatal(err)
	}
	if later, err := reopened.PreviewRecipeSage(body); err != nil || later.Candidates[0].Status != "imported" {
		t.Fatalf("deduplication lost on restart: %+v %v", later, err)
	}
	archive := reopened.Export()
	imported, err := Open(filepath.Join(t.TempDir(), "another.json"))
	if err != nil {
		t.Fatal(err)
	}
	if count, err := imported.Import(archive); err != nil || count != 1 {
		t.Fatalf("archive migration: %d %v", count, err)
	}
	if dup, err := imported.PreviewRecipeSage(body); err != nil || dup.Candidates[0].Status != "imported" {
		t.Fatalf("source provenance lost from export: %+v %v", dup, err)
	}
}

func TestRecipeSageTitlesDoNotDetermineSourceIdentity(t *testing.T) {
	db, err := Open(filepath.Join(t.TempDir(), "recipes.json"))
	if err != nil {
		t.Fatal(err)
	}
	body := recipeSageFixture(t,
		sageRecipeRecord("one", "Rice bowl", "2 cups rice"),
		sageRecipeRecord("two", "Rice bowl", "2 cups rice"),
	)
	preview, err := db.PreviewRecipeSage(body)
	if err != nil || preview.Ready != 2 ||
		preview.Candidates[0].Key == preview.Candidates[1].Key {
		t.Fatalf("identical titles were confused: %+v %v", preview, err)
	}
	if result, err := db.ImportRecipeSage(body, preview.Digest, []string{
		preview.Candidates[0].Key, preview.Candidates[1].Key,
	}, nil); err != nil || result.Imported != 2 {
		t.Fatalf("independent sources failed: %+v %v", result, err)
	}
}

func TestRecipeSageRejectsDuplicateAndMalformedInputs(t *testing.T) {
	db, err := Open(filepath.Join(t.TempDir(), "recipes.json"))
	if err != nil {
		t.Fatal(err)
	}
	same := sageRecipeRecord("source-1", "Rice", "2 cups rice")
	duplicated := recipeSageFixture(t, same, same)
	preview, err := db.PreviewRecipeSage(duplicated)
	if err != nil || preview.Ready != 0 ||
		preview.Candidates[0].Status != "duplicate" || preview.Candidates[1].Status != "duplicate" {
		t.Fatalf("source duplicate was not marked: %+v %v", preview, err)
	}
	if _, err := db.ImportRecipeSage(duplicated, preview.Digest, []string{
		preview.Candidates[0].Key, preview.Candidates[0].Key,
	}, nil); err == nil {
		t.Fatal("repeated selection accepted")
	}
	for _, invalid := range [][]byte{
		[]byte("{}"),
		[]byte(`{"recipes":null}`),
		[]byte(`{"recipes":[]}`),
		[]byte(`{"recipes":{}}`),
		[]byte(`{"recipes":[`),
		[]byte(strings.Repeat("x", MaxRecipeSageExportBytes+1)),
	} {
		if _, err := db.PreviewRecipeSage(invalid); err == nil {
			t.Fatalf("invalid document accepted: %q", invalid[:min(len(invalid), 40)])
		}
	}
	noID := sageRecipeRecord("", "Rice", "2 cups rice")
	delete(noID, "identifier")
	first := recipeSageFixture(t, noID)
	slightlyDifferent := []byte(strings.ReplaceAll(string(first), "\":", "\": "))
	a, errA := db.PreviewRecipeSage(first)
	b, errB := db.PreviewRecipeSage(slightlyDifferent)
	if errA != nil || errB != nil || a.Candidates[0].Key != b.Candidates[0].Key {
		t.Fatalf("whitespace changed a source identity: %v %v", errA, errB)
	}
}


func TestRecipeSageOwnerCorrectionsKeepImportIdentity(t *testing.T) {
	db, err := Open(filepath.Join(t.TempDir(), "recipes.json"))
	if err != nil {
		t.Fatal(err)
	}
	source := recipeSageFixture(t, sageRecipeRecord("source-incomplete", "Vegetable soup", "onion", "1 tbsp oil"))
	preview, err := db.PreviewRecipeSage(source)
	if err != nil || preview.Ready != 0 || preview.Candidates[0].Status != "needs_review" {
		t.Fatalf("invalid recipe was eligible: %+v %v", preview, err)
	}
	key := preview.Candidates[0].Key
	corrected := preview.Candidates[0].Recipe
	corrected.Ingredients[0].Amount = 2
	corrected.Ingredients[0].Unit = "pieces"
	corrected.Ingredients[0].Notes = "Owner-confirmed quantity"
	if _, err := db.ImportRecipeSage(source, preview.Digest, []string{key}, nil); err == nil {
		t.Fatal("incomplete recipe imported without owner correction")
	}
	if _, err := db.ImportRecipeSage(source, preview.Digest, []string{key}, map[string]Recipe{
		"recipesage:" + strings.Repeat("a", 64): corrected,
	}); err == nil {
		t.Fatal("unselected source override was accepted")
	}
	broken := corrected
	broken.Servings = 0
	if _, err := db.ImportRecipeSage(source, preview.Digest, []string{key}, map[string]Recipe{key: broken}); err == nil {
		t.Fatal("invalid owner correction was accepted")
	}
	corrected.ID = strings.Repeat("f", 32)
	if _, err := db.ImportRecipeSage(source, preview.Digest, []string{key}, map[string]Recipe{key: corrected}); err == nil {
		t.Fatal("owner correction supplied a fabricated stored ID")
	}
	corrected.ID = ""
	result, err := db.ImportRecipeSage(source, preview.Digest, []string{key}, map[string]Recipe{key: corrected})
	if err != nil || result.Imported != 1 {
		t.Fatalf("reviewed recipe import failed: %+v %v", result, err)
	}
	saved, err := db.Get(result.IDs[0])
	if err != nil || saved.ImportRef != key || saved.Ingredients[0].Amount != 2 {
		t.Fatalf("reviewed data or source identity lost: %+v %v", saved, err)
	}
	repeated, err := db.PreviewRecipeSage(source)
	if err != nil || repeated.Candidates[0].Status != "imported" {
		t.Fatalf("reviewed record cannot be deduplicated: %+v %v", repeated, err)
	}
}
