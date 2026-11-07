package media

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestSearchAndRequest(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Api-Key") != "test-secret" {
			t.Error("API key missing")
		}
		switch r.URL.Path {
		case "/api/v1/search":
			if r.URL.Query().Get("query") != "Dune: Part Two" {
				t.Errorf("incorrect search query: %s", r.URL.RawQuery)
			}
		case "/api/v1/request":
			if r.Method != http.MethodPost {
				t.Errorf("wrong method: %s", r.Method)
			}
			var req MediaRequest
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				t.Error(err)
			}
			if req.MediaType != "movie" || req.MediaID != 123 {
				t.Errorf("unexpected request: %+v", req)
			}
		default:
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"results":[]}`))
	}))
	defer server.Close()
	client, err := NewSeerr(server.URL, "test-secret")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := client.Search(context.Background(), "Dune: Part Two"); err != nil {
		t.Fatal(err)
	}
	if _, err := client.AddRequest(context.Background(), MediaRequest{MediaType: "movie", MediaID: 123}); err != nil {
		t.Fatal(err)
	}
}

func TestRejectsUnsupportedOperations(t *testing.T) {
	for _, raw := range []string{"http://169.254.169.254:80", "http://example.com:5055", "https://me:pass@example.com:5055", "file:///etc/passwd"} {
		if _, err := NewSeerr(raw, "key"); err == nil {
			t.Fatalf("accepted unsafe URL: %s", raw)
		}
	}
	client, err := NewSeerr("http://127.0.0.1:5055", "secret")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := client.AddRequest(context.Background(), MediaRequest{MediaType: "book", MediaID: 12}); err == nil {
		t.Fatal("accepted unsupported book type")
	}
	if _, err := client.AddRequest(context.Background(), MediaRequest{MediaType: "tv", MediaID: 3, Seasons: []int{-1}}); err == nil {
		t.Fatal("accepted invalid season")
	}
	if _, err := client.Search(context.Background(), strings.Repeat("x", 201)); err == nil {
		t.Fatal("accepted invalid query")
	}
}
