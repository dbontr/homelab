package bridge

import (
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestBackendCannotOverrideInterfaceResponseSecurity(t *testing.T) {
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer "+internalSecret {
			t.Error("missing internal service identity")
		}
		w.Header().Set("Set-Cookie", "session=backend-controlled; HttpOnly")
		w.Header().Set("Cache-Control", "public, max-age=3600")
		w.Header().Set("Referrer-Policy", "unsafe-url")
		w.Header().Set("X-Content-Type-Options", "none")
		w.Header().Set("X-Frame-Options", "ALLOWALL")
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, "{\"ok\":true}")
	}))
	defer backend.Close()

	for _, tc := range []struct {
		surface      Surface
		method, path string
	}{
		{MCP, "POST", "/mcp"},
		{API, "GET", "/v1/tasks"},
	} {
		handler, err := New(Config{
			Surface: tc.surface, BackendURL: backend.URL,
			ClientToken: clientSecret, ServiceToken: internalSecret,
		})
		if err != nil {
			t.Fatal(err)
		}
		req := httptest.NewRequest(tc.method, tc.path, nil)
		req.Header.Set("Authorization", "Bearer "+clientSecret)
		result := httptest.NewRecorder()
		handler.ServeHTTP(result, req)
		if result.Code != 200 || !strings.Contains(result.Body.String(), "\"ok\":true") {
			t.Fatalf("%s: %d %s", tc.surface, result.Code, result.Body.String())
		}
		expected := map[string]string{
			"Cache-Control":          "no-store",
			"Referrer-Policy":        "no-referrer",
			"X-Content-Type-Options": "nosniff",
			"X-Frame-Options":        "DENY",
		}
		for key, value := range expected {
			if actual := result.Header().Get(key); actual != value {
				t.Errorf("%s: %s=%q, want %q", tc.surface, key, actual, value)
			}
		}
		if len(result.Result().Cookies()) != 0 || result.Header().Get("Set-Cookie") != "" {
			t.Errorf("%s forwarded an upstream session cookie", tc.surface)
		}
	}
}

func TestOversizedBackendResponseHeadersFailClosed(t *testing.T) {
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Large-Response", strings.Repeat("x", 80<<10))
		_, _ = io.WriteString(w, "data")
	}))
	defer backend.Close()

	handler, err := New(Config{
		Surface: API, BackendURL: backend.URL,
		ClientToken: clientSecret, ServiceToken: internalSecret,
	})
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodGet, "/v1/tasks", nil)
	req.Header.Set("Authorization", "Bearer "+clientSecret)
	result := httptest.NewRecorder()
	handler.ServeHTTP(result, req)
	if result.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected unavailable for oversized headers, got %d", result.Code)
	}
	if result.Header().Get("Cache-Control") != "no-store" || result.Header().Get("X-Large-Response") != "" {
		t.Fatal("backend rejection leaked untrusted response headers")
	}
}

func TestProtectedInterfaceCredentialsMustBeOwnerOnlyRegularFiles(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "credential")
	token := strings.Repeat("s", 40)
	if err := os.WriteFile(path, []byte(token+"\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if got, err := protectedToken(path); err != nil || got != token {
		t.Fatalf("protected credential unavailable: %v", err)
	}
	link := filepath.Join(dir, "link")
	if err := os.Symlink(path, link); err != nil {
		t.Fatal(err)
	}
	if _, err := protectedToken(link); err == nil {
		t.Fatal("symlink credential was accepted")
	}
	if err := os.Chmod(path, 0644); err != nil {
		t.Fatal(err)
	}
	if _, err := protectedToken(path); err == nil {
		t.Fatal("world-readable credential was accepted")
	}
	if err := os.Chmod(path, 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(strings.Repeat("x", 4098)), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := protectedToken(path); err == nil {
		t.Fatal("oversized credential was accepted")
	}
	if err := os.WriteFile(path, []byte("short"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := protectedToken(path); err == nil {
		t.Fatal("short credential was accepted")
	}
}
