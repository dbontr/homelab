package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/dbontr/homelab-new/system/health/internal/recipesage"
)

func newServer(t *testing.T, handler http.HandlerFunc) *Server {
	t.Helper()
	upstream := httptest.NewServer(handler)
	t.Cleanup(upstream.Close)
	c, err := recipesage.New(recipesage.Config{URL: upstream.URL, Token: "upstream-token", AllowHTTP: true})
	if err != nil {
		t.Fatal(err)
	}
	return New(c)
}
func invoke(t *testing.T, s *Server, method, params string) *response {
	t.Helper()
	data := []byte(`{"jsonrpc":"2.0","id":1,"method":"` + method + `","params":` + params + `}`)
	result := s.Dispatch(context.Background(), data)
	if result == nil {
		t.Fatal("missing response")
	}
	return result
}
func TestInitializeAndListTools(t *testing.T) {
	s := newServer(t, func(w http.ResponseWriter, r *http.Request) { t.Fatal("unexpected upstream call") })
	result := invoke(t, s, "initialize", `{}`)
	data, _ := json.Marshal(result.Result)
	if !strings.Contains(string(data), protocolVersion) {
		t.Fatalf("wrong protocol version: %s", data)
	}
	tools := invoke(t, s, "tools/list", `{}`)
	var list struct {
		Tools []Tool `json:"tools"`
	}
	raw, _ := json.Marshal(tools.Result)
	if err := json.Unmarshal(raw, &list); err != nil {
		t.Fatal(err)
	}
	if len(list.Tools) < 23 {
		t.Fatalf("missing tools: %d", len(list.Tools))
	}
	names := map[string]bool{}
	for _, tool := range list.Tools {
		if names[tool.Name] {
			t.Fatalf("duplicate %s", tool.Name)
		}
		names[tool.Name] = true
		if tool.InputSchema == nil {
			t.Fatal("missing schema")
		}
	}
	for _, name := range []string{"recipe_get", "recipe_create", "recipe_edit", "recipe_delete", "meal_plan_add_items", "shopping_list_add_items"} {
		if !names[name] {
			t.Errorf("missing %s", name)
		}
	}
}
func TestStdioCall(t *testing.T) {
	s := newServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer upstream-token" {
			t.Errorf("missing RecipeSage auth")
		}
		if r.Method != "GET" || r.URL.Path != "/compat/v2/recipes/getRecipe" || r.URL.Query().Get("id") != "my-id" {
			t.Errorf("wrong request: %s %s", r.Method, r.URL.String())
		}
		_, _ = w.Write([]byte(`{"title":"Soup","ingredients":"water"}`))
	})
	input := strings.NewReader("{\"jsonrpc\":\"2.0\",\"id\":1,\"method\":\"initialize\",\"params\":{}}\n" +
		"{\"jsonrpc\":\"2.0\",\"method\":\"notifications/initialized\"}\n" +
		"{\"jsonrpc\":\"2.0\",\"id\":2,\"method\":\"tools/call\",\"params\":{\"name\":\"recipe_get\",\"arguments\":{\"id\":\"my-id\"}}}\n")
	var out bytes.Buffer
	if err := s.ServeStdio(context.Background(), input, &out); err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(out.String()), "\n")
	if len(lines) != 2 {
		t.Fatalf("expected two JSON-RPC replies: %s", out.String())
	}
	if !strings.Contains(lines[1], "Soup") {
		t.Fatalf("recipe missing: %s", lines[1])
	}
}
func TestDeletionRequiresConfirmation(t *testing.T) {
	s := newServer(t, func(w http.ResponseWriter, r *http.Request) { t.Fatal("delete called without confirmation") })
	result := invoke(t, s, "tools/call", `{"name":"recipe_delete","arguments":{"id":"my-id","confirm":false}}`)
	data, _ := json.Marshal(result.Result)
	if !strings.Contains(string(data), `"isError":true`) {
		t.Fatalf("delete not rejected: %s", data)
	}
}
func TestHTTPAuthorizationAndOrigin(t *testing.T) {
	s := newServer(t, func(w http.ResponseWriter, r *http.Request) { t.Fatal("unexpected upstream call") })
	handler, err := s.HTTPHandler("top-secret", []string{"https://trusted.test"})
	if err != nil {
		t.Fatal(err)
	}
	makeRequest := func(token, origin string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodPost, "/mcp", strings.NewReader(`{"jsonrpc":"2.0","id":4,"method":"ping"}`))
		req.Header.Set("Content-Type", "application/json")
		if token != "" {
			req.Header.Set("Authorization", token)
		}
		if origin != "" {
			req.Header.Set("Origin", origin)
		}
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)
		return w
	}
	if got := makeRequest("", ""); got.Code != 401 {
		t.Errorf("missing token status: %d", got.Code)
	}
	if got := makeRequest("Bearer top-secret", "https://evil.test"); got.Code != 403 {
		t.Errorf("origin status: %d", got.Code)
	}
	if got := makeRequest("Bearer top-secret", "https://trusted.test"); got.Code != 200 || !strings.Contains(got.Body.String(), `"result":{}`) {
		t.Errorf("valid request failed: %d %s", got.Code, got.Body.String())
	}
	reqPreflight := httptest.NewRequest(http.MethodOptions, "/mcp", nil)
	reqPreflight.Header.Set("Origin", "https://trusted.test")
	wPreflight := httptest.NewRecorder()
	handler.ServeHTTP(wPreflight, reqPreflight)
	if wPreflight.Code != http.StatusNoContent || wPreflight.Header().Get("Access-Control-Allow-Origin") != "https://trusted.test" {
		t.Errorf("CORS preflight failed: %d", wPreflight.Code)
	}
	if _, err := s.HTTPHandler("", nil); err == nil {
		t.Error("missing HTTP token accepted")
	}
	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	if w.Code != 200 || w.Body.String() != "ok\n" {
		t.Errorf("healthcheck failed: %d %q", w.Code, w.Body.String())
	}
}
func TestClipRejectsPrivateURL(t *testing.T) {
	s := newServer(t, func(w http.ResponseWriter, r *http.Request) { t.Fatal("SSRF target reached") })
	for _, v := range []string{"http://127.0.0.1/", "http://169.254.169.254/latest", "http://localhost/", "file:///etc/passwd", "http://192.168.1.2/"} {
		arg, _ := json.Marshal(map[string]any{"name": "recipe_clip_preview", "arguments": map[string]any{"url": v}})
		reply := invoke(t, s, "tools/call", string(arg))
		data, _ := json.Marshal(reply.Result)
		if !strings.Contains(string(data), `"isError":true`) {
			t.Errorf("URL permitted: %s", v)
		}
	}
}
func TestShoppingListMarkCompleted(t *testing.T) {
	s := newServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/compat/v2/shoppingLists/updateShoppingListItems" {
			t.Errorf("wrong endpoint: %s", r.URL.Path)
		}
		body, _ := io.ReadAll(r.Body)
		if !bytes.Contains(body, []byte(`"completed":true`)) || !bytes.Contains(body, []byte(`"shoppingListId":"list1"`)) {
			t.Errorf("wrong payload %s", body)
		}
		_, _ = w.Write([]byte(`{"reference":"rev1"}`))
	})
	result := invoke(t, s, "tools/call", `{"name":"shopping_list_set_completed","arguments":{"shoppingListId":"list1","itemId":"item1","completed":true}}`)
	data, _ := json.Marshal(result.Result)
	if !strings.Contains(string(data), `"isError":false`) {
		t.Fatalf("unexpected result: %s", data)
	}
}
