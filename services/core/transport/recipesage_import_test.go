package transport

import (
	"bytes"
	"encoding/json"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dbontr/homelab-new/system/recipes"
	"github.com/dbontr/homelab-new/system/store"
)

func TestRecipeSagePreviewAndImportAuthorization(t *testing.T) {
	base := t.TempDir()
	tasks, err := store.Open(filepath.Join(base, "tasks.json"), "UTC")
	if err != nil {
		t.Fatal(err)
	}
	server, err := New(tasks, strings.Repeat("z", 40), nil)
	if err != nil {
		t.Fatal(err)
	}
	db, err := recipes.Open(filepath.Join(base, "recipes.json"))
	if err != nil {
		t.Fatal(err)
	}
	server.SetRecipes(db)
	manager, accessAssertion := accessTransportFixture(t)
	server.SetSessions(manager)
	if err := server.SetAllowedOrigins([]string{"https://console.example"}); err != nil {
		t.Fatal(err)
	}
	handler := server.Handler()
	call := func(method, path string, body []byte, identity *accessTestIdentity, csrf, mediaType string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, path, bytes.NewReader(body))
		req.Host = "console.example"
		req.Header.Set("Origin", "https://console.example")
		if mediaType != "" {
			req.Header.Set("Content-Type", mediaType)
		}
		if csrf != "" {
			req.Header.Set("X-CSRF-Token", csrf)
		}
		if identity != nil {
			req.Header.Set("Cf-Access-Jwt-Assertion",identity.Value)
		}
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)
		return w
	}
	accessToken := &accessTestIdentity{Value: accessAssertion}
	var identity struct {
		CSRF string `json:"csrf_token"`
	}
	s := call("GET", "/ui/session", nil, identity, "", "")
	if err := json.Unmarshal(s.Body.Bytes(), &identity); err != nil || identity.CSRF == "" {
		t.Fatalf("missing session token: %v", err)
	}
	source, err := json.Marshal(map[string]any{"recipes": []any{
		map[string]any{
			"@type": "Recipe", "identifier": "sage-1", "name": "Olive salad",
			"recipeYield": "2", "recipeIngredient": []any{"2 cups olives"},
			"recipeInstructions": []any{map[string]string{"@type": "HowToStep", "text": "Serve cold."}},
		},
	}})
	if err != nil {
		t.Fatal(err)
	}
	request, err := json.Marshal(map[string]any{"source": string(source)})
	if err != nil {
		t.Fatal(err)
	}
	if r := call("POST", "/v1/recipes/recipesage/preview", request, nil, "", "application/json"); r.Code != 401 {
		t.Fatalf("anonymous migration preview accepted: %d", r.Code)
	}
	if r := call("POST", "/v1/recipes/recipesage/preview", request, identity, "", "application/json"); r.Code != 401 {
		t.Fatalf("preview without CSRF accepted: %d", r.Code)
	}
	if r := call("POST", "/v1/recipes/recipesage/preview", request, identity, identity.CSRF, "text/plain"); r.Code != 401 {
		t.Fatalf("unexpected media type accepted: %d", r.Code)
	}
	previewResponse := call("POST", "/v1/recipes/recipesage/preview", request, identity, identity.CSRF, "application/json")
	var preview recipes.RecipeSagePreview
	if previewResponse.Code != 200 || json.Unmarshal(previewResponse.Body.Bytes(), &preview) != nil ||
		preview.Ready != 1 {
		t.Fatalf("preview failed: %d %s", previewResponse.Code, previewResponse.Body.String())
	}
	payload, err := json.Marshal(map[string]any{
		"source": string(source), "digest": preview.Digest,
		"keys": []string{preview.Candidates[0].Key},
	})
	if err != nil {
		t.Fatal(err)
	}
	if r := call("POST", "/v1/recipes/recipesage/import", payload, identity, "", "application/json"); r.Code != 401 {
		t.Fatalf("import without CSRF accepted: %d", r.Code)
	}
	imported := call("POST", "/v1/recipes/recipesage/import", payload, identity, identity.CSRF, "application/json")
	if imported.Code != 201 || !strings.Contains(imported.Body.String(), `"imported":1`) {
		t.Fatalf("import failed: %d %s", imported.Code, imported.Body.String())
	}
	if r := call("POST", "/v1/recipes/recipesage/import", payload, identity, identity.CSRF, "application/json"); r.Code != 409 {
		t.Fatalf("duplicate migration allowed: %d %s", r.Code, r.Body.String())
	}
	if r := call("POST", "/mcp", request, identity, identity.CSRF, "application/json"); r.Code != 401 {
		t.Fatalf("browser session obtained MCP credentials: %d", r.Code)
	}
	if len(db.List("")) != 1 {
		t.Fatal("repeated migration changed the library")
	}
	incompleteSource, err := json.Marshal(map[string]any{"recipes": []any{
		map[string]any{
			"@type": "Recipe", "identifier": "sage-2", "name": "Soup",
			"recipeYield": "2", "recipeIngredient": []any{"onion"},
			"recipeInstructions": []any{"Heat gently."},
		},
	}})
	if err != nil {
		t.Fatal(err)
	}
	reviewBody, err := json.Marshal(map[string]any{"source": string(incompleteSource)})
	if err != nil {
		t.Fatal(err)
	}
	reviewResponse := call("POST", "/v1/recipes/recipesage/preview", reviewBody, identity, identity.CSRF, "application/json")
	var review recipes.RecipeSagePreview
	if reviewResponse.Code != 200 || json.Unmarshal(reviewResponse.Body.Bytes(), &review) != nil ||
		review.Candidates[0].Status != "needs_review" {
		t.Fatalf("incomplete preview: %d %s", reviewResponse.Code, reviewResponse.Body.String())
	}
	corrected := review.Candidates[0].Recipe
	corrected.Ingredients[0].Amount = 1
	applyBody, err := json.Marshal(map[string]any{
		"source": string(incompleteSource), "digest": review.Digest,
		"keys": []string{review.Candidates[0].Key},
		"overrides": map[string]recipes.Recipe{review.Candidates[0].Key: corrected},
	})
	if err != nil {
		t.Fatal(err)
	}
	applied := call("POST", "/v1/recipes/recipesage/import", applyBody, identity, identity.CSRF, "application/json")
	if applied.Code != 201 || len(db.List("")) != 2 {
		t.Fatalf("reviewed import failed: %d %s", applied.Code, applied.Body.String())
	}
}
