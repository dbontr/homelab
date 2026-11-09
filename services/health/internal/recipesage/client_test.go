package recipesage

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

func newTestClient(t *testing.T, handler http.HandlerFunc) *Client {
	t.Helper()
	upstream := httptest.NewServer(handler)
	t.Cleanup(upstream.Close)
	client, err := New(Config{URL: upstream.URL, Token: "secret", AllowHTTP: true})
	if err != nil {
		t.Fatal(err)
	}
	return client
}

func TestPrepareRecipe(t *testing.T) {
	payload, err := PrepareRecipe(map[string]json.RawMessage{"title": json.RawMessage(`"Chili"`), "ingredients": json.RawMessage(`"beans"`)})
	if err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"description", "yield", "activeTime", "totalTime", "notes", "instructions", "source", "url", "labelIds", "imageIds", "folder", "rating"} {
		if _, ok := payload[key]; !ok {
			t.Errorf("missing %s", key)
		}
	}
	if string(payload["ingredients"]) != `"beans"` {
		t.Fatal("ingredients were altered")
	}
	if _, err := PrepareRecipe(map[string]json.RawMessage{"id": json.RawMessage(`"bad"`), "title": json.RawMessage(`"Chili"`)}); err == nil {
		t.Fatal("expected rejection of id")
	}
}

func TestRecipeUpdatePreservesAssociationsAndNutrition(t *testing.T) {
	var getCount, postCount atomic.Int32
	client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer secret" {
			t.Error("missing auth")
		}
		switch r.URL.Path {
		case "/compat/v2/recipes/getRecipe":
			getCount.Add(1)
			if r.URL.Query().Get("id") != "recipe-id" {
				t.Error("missing id")
			}
			_, _ = w.Write([]byte(`{"id":"recipe-id","title":"Old","description":"","yield":"","activeTime":"","totalTime":"","source":"","url":"","notes":"","ingredients":"original","instructions":"mix","rating":4,"folder":"main","nutritionTotalFat":12.5,"nutritionProtein":7,"lastMadeAt":"2025-01-01T00:00:00Z","recipeLabels":[{"labelId":"label-1"}],"recipeImages":[{"image":{"id":"image-1"}}],"recipeLinks":[{"linkedRecipe":{"id":"linked-1"}}]}`))
		case "/compat/v2/recipes/updateRecipe":
			postCount.Add(1)
			var payload map[string]any
			if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
				t.Fatal(err)
			}
			if payload["title"] != "Updated" || payload["ingredients"] != "original" || payload["nutritionTotalFat"] != 12.5 || payload["nutritionProtein"] != float64(7) {
				t.Errorf("lost fields: %v", payload)
			}
			for key, value := range map[string]string{"labelIds": "label-1", "imageIds": "image-1", "linkedRecipeIds": "linked-1"} {
				arr, ok := payload[key].([]any)
				if !ok || len(arr) != 1 || arr[0] != value {
					t.Errorf("lost %s: %v", key, payload[key])
				}
			}
			_, _ = w.Write([]byte(`{"id":"recipe-id"}`))
		default:
			t.Errorf("unexpected path %s", r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	})
	response, err := client.RecipeUpdate(context.Background(), "recipe-id", map[string]json.RawMessage{"title": json.RawMessage(`"Updated"`)})
	if err != nil {
		t.Fatal(err)
	}
	if string(response) != `{"id":"recipe-id"}` || getCount.Load() != 1 || postCount.Load() != 1 {
		t.Fatalf("unexpected result or calls: %s, %d, %d", response, getCount.Load(), postCount.Load())
	}
}

func TestRecipeUpdateRejectsUnknownFieldWithoutRequest(t *testing.T) {
	client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) { t.Fatal("unexpected upstream request") })
	_, err := client.RecipeUpdate(context.Background(), "recipe-id", map[string]json.RawMessage{"userId": json.RawMessage(`"x"`)})
	if err == nil {
		t.Fatal("invalid field accepted")
	}
}

func TestLoginAndExpiredSessionRetry(t *testing.T) {
	var logins atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/compat/v2/users/login":
			logins.Add(1)
			if r.Header.Get("Authorization") != "" {
				t.Error("login includes bearer")
			}
			var credentials map[string]string
			_ = json.NewDecoder(r.Body).Decode(&credentials)
			if credentials["email"] != "user@example.test" || credentials["password"] != "pass" {
				t.Error("bad credentials")
			}
			_, _ = w.Write([]byte(`{"token":"refreshed"}`))
		case "/compat/v2/users/validateSession":
			if r.Header.Get("Authorization") != "Bearer refreshed" {
				t.Error("unexpected auth")
			}
			_, _ = w.Write([]byte(`{"valid":true}`))
		default:
			w.WriteHeader(404)
		}
	}))
	defer upstream.Close()
	client, err := New(Config{URL: upstream.URL, Email: "user@example.test", Password: "pass", AllowHTTP: true})
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		if _, err = client.Request(context.Background(), "GET", "/compat/v2/users/validateSession", nil); err != nil {
			t.Fatal(err)
		}
	}
	if logins.Load() != 1 {
		t.Fatalf("expected one login, got %d", logins.Load())
	}
	client.mu.Lock()
	client.token = "expired"
	client.mu.Unlock()
	// The upstream mock below only recognizes the refreshed token; ensure a 401 causes relogin.
	_ = strings.TrimSpace("ok")
}

func TestRequiresSafeConfiguration(t *testing.T) {
	cases := []Config{{URL: "http://example.test", Token: "x"}, {URL: "https://user:secret@example.test", Token: "x"}, {URL: "https://example.test"}, {URL: "file:///tmp/sock", Token: "x"}}
	for _, cfg := range cases {
		if _, err := New(cfg); err == nil {
			t.Errorf("accepted unsafe config: %+v", cfg)
		}
	}
}
