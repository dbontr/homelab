package transport

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dbontr/homelab-new/system/recipes"
	"github.com/dbontr/homelab-new/system/store"
)

func TestRecipeRoutesAndScopedBrowserWrites(t *testing.T) {
	dir := t.TempDir()
	tasks, err := store.Open(filepath.Join(dir, "state.json"), "UTC")
	if err != nil { t.Fatal(err) }
	s, err := New(tasks, strings.Repeat("t", 40), nil)
	if err != nil { t.Fatal(err) }
	recipeStore, err := recipes.Open(filepath.Join(dir, "recipes", "recipes.json"))
	if err != nil { t.Fatal(err) }
	s.SetRecipes(recipeStore)
	manager, accessAssertion := accessTransportFixture(t)
	s.SetSessions(manager)
	if err := s.SetAllowedOrigins([]string{"https://console.example"}); err != nil { t.Fatal(err) }
	handler := s.Handler()
	call := func(method, path, body string, identity *accessTestIdentity, csrf string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(method, path, strings.NewReader(body))
		r.Host = "console.example"
		r.Header.Set("Origin", "https://console.example")
		if body != "" { r.Header.Set("Content-Type", "application/json") }
		if csrf != "" { r.Header.Set("X-CSRF-Token", csrf) }
		if identity != nil { r.Header.Set("Cf-Access-Jwt-Assertion",identity.Value) }
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		return w
	}
	if r := call("GET","/v1/recipes","",nil,""); r.Code!=401 { t.Fatal("anonymous recipe read accepted") }
	accessToken := &accessTestIdentity{Value: accessAssertion}
	status := call("GET","/ui/session","",accessToken,"")
	var sessionInfo struct { CSRF string `json:"csrf_token"` }
	if err := json.Unmarshal(status.Body.Bytes(),&sessionInfo);err!=nil { t.Fatal(err) }
	if sessionInfo.CSRF=="" { t.Fatal("missing session CSRF token") }
	payload := `{"title":"Rice bowl","servings":2,"ingredients":[{"name":"Rice","amount":1,"unit":"cup"}],"steps":[{"text":"Cook the rice."}]}`
	if r:=call("POST","/v1/recipes",payload,accessToken,"");r.Code!=401 { t.Fatal("recipe write missing CSRF accepted") }
	created := call("POST","/v1/recipes",payload,accessToken,sessionInfo.CSRF)
	if created.Code!=201 { t.Fatalf("create: %d %s",created.Code,created.Body.String()) }
	var recipe recipes.Recipe
	if err:=json.Unmarshal(created.Body.Bytes(),&recipe);err!=nil { t.Fatal(err) }
	if recipe.ID=="" || recipe.Revision!=1 { t.Fatal("missing stored identity or revision") }
	recipe.Title = "Updated rice bowl"
	serialized, err := json.Marshal(recipe)
	if err != nil { t.Fatal(err) }
	if response := call("PUT", "/v1/recipes/"+recipe.ID, string(serialized), accessToken, sessionInfo.CSRF); response.Code != http.StatusOK {
		t.Fatalf("recipe update: %d %s", response.Code, response.Body.String())
	}
	if response := call("GET", "/v1/recipes/"+recipe.ID+"/history", "", nil, ""); response.Code != http.StatusUnauthorized {
		t.Fatalf("anonymous recipe history accepted: %d", response.Code)
	}
	history := call("GET", "/v1/recipes/"+recipe.ID+"/history", "", accessToken, "")
	var versions []recipes.Recipe
	if history.Code != http.StatusOK || json.Unmarshal(history.Body.Bytes(), &versions) != nil ||
		len(versions) != 2 || versions[0].Revision != 2 || versions[1].Revision != 1 {
		t.Fatalf("recipe history: %d %s", history.Code, history.Body.String())
	}
	restoreRequest := `{"source_revision":1,"expected_revision":2}`
	if response := call("POST", "/v1/recipes/"+recipe.ID+"/restore", restoreRequest, accessToken, ""); response.Code != 401 {
		t.Fatalf("restore without CSRF accepted: %d", response.Code)
	}
	restored := call("POST", "/v1/recipes/"+recipe.ID+"/restore", restoreRequest, accessToken, sessionInfo.CSRF)
	var restoredRecipe recipes.Recipe
	if restored.Code != 200 || json.Unmarshal(restored.Body.Bytes(), &restoredRecipe) != nil ||
		restoredRecipe.Title != "Rice bowl" || restoredRecipe.Revision != 3 {
		t.Fatalf("restore failed: %d %s", restored.Code, restored.Body.String())
	}
	if response := call("POST", "/v1/recipes/"+recipe.ID+"/restore", restoreRequest, accessToken, sessionInfo.CSRF); response.Code != 409 {
		t.Fatalf("stale restore accepted: %d", response.Code)
	}
	if r:=call("POST","/v1/tasks",`{"title":"Owner task"}`,accessToken,sessionInfo.CSRF);r.Code!=201 {
        t.Fatalf("owner task creation rejected: %d %s",r.Code,r.Body.String())
    }
	if r:=call("POST","/mcp",`{"jsonrpc":"2.0","id":1,"method":"tools/list"}`,accessToken,sessionInfo.CSRF);r.Code!=401 {
		t.Fatal("recipe-scoped session reached MCP")
	}
	meal := `{"recipe_id":"`+recipe.ID+`","date":"2026-11-12","servings":4}`
	if r:=call("POST","/v1/meals",meal,accessToken,sessionInfo.CSRF);r.Code!=201 {t.Fatalf("schedule meal: %s",r.Body.String())}
	if r:=call("GET","/v1/groceries","",accessToken,"");r.Code!=200 || !strings.Contains(r.Body.String(),`"amount":2`) {
		t.Fatalf("consolidated grocery list: %s",r.Body.String())
	}
	blockedURL := `{"url":"https://127.0.0.1/latest/meta-data"}`
	if r:=call("POST","/v1/recipes/import-url",blockedURL,nil,"");r.Code!=401 {
		t.Fatalf("unauthenticated URL import accepted: %d",r.Code)
	}
	if r:=call("POST","/v1/recipes/import-url",blockedURL,accessToken,"");r.Code!=401 {
		t.Fatalf("URL import without CSRF accepted: %d",r.Code)
	}
	if r:=call("POST","/v1/recipes/import-url",blockedURL,accessToken,sessionInfo.CSRF);r.Code!=422 {
		t.Fatalf("local-network URL import accepted: %d %s",r.Code,r.Body.String())
	}
	if r:=call("DELETE","/v1/recipes/"+recipe.ID,"",accessToken,sessionInfo.CSRF);r.Code!=428 {
		t.Fatal("delete without revision accepted")
	}
	if r:=call("GET","/v1/recipes/export","",accessToken,"");r.Code!=200 || !strings.Contains(r.Body.String(),`"version":1`) {
		t.Fatal("archive not available to authenticated owner")
	}
}
