package privileged

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
)

func TestCredentialFileMustRemainProtectedAtUseTime(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"jsonrpc":"2.0","id":1,"result":{"content":[]}}`))
	}))
	defer server.Close()
	dir := t.TempDir()
	tokenPath := filepath.Join(dir, "root-token")
	otherPath := filepath.Join(dir, "other")
	secret := strings.Repeat("s", 48) + "\n"
	if err := os.WriteFile(tokenPath, []byte(secret), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(otherPath, []byte(secret), 0600); err != nil {
		t.Fatal(err)
	}
	p := &WorkspacePolicy{Endpoint: server.URL + "/mcp", TokenFile: tokenPath, Projects: []string{"research"}}
	if err := p.validate(); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(tokenPath); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(otherPath, tokenPath); err != nil {
		t.Fatal(err)
	}
	args := map[string]any{"host": "saturn", "project_id": "research", "workspace_id": "ws1"}
	if err := p.Call(context.Background(), "infrastructure__workspace_stop", args); err == nil {
		t.Fatal("symlink replacement reached provider")
	}
	if calls.Load() != 0 {
		t.Fatal("credential replacement contacted provider")
	}
	if err := os.Remove(tokenPath); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(tokenPath, []byte(secret), 0644); err != nil {
		t.Fatal(err)
	}
	// Exercise the unsafe mode explicitly when the test runner uses umask 077.
	if err := os.Chmod(tokenPath, 0644); err != nil {
		t.Fatal(err)
	}
	if err := p.Call(context.Background(), "infrastructure__workspace_stop", args); err == nil {
		t.Fatal("world-readable replacement reached provider")
	}
	if calls.Load() != 0 {
		t.Fatal("unsafe file permissions contacted provider")
	}
	if err := os.Chmod(tokenPath, 0600); err != nil {
		t.Fatal(err)
	}
	if err := p.Call(context.Background(), "infrastructure__workspace_stop", args); err != nil {
		t.Fatalf("restored credential was rejected: %v", err)
	}
	if calls.Load() != 1 {
		t.Fatal("valid credential was not used")
	}
}

func TestVaultCredentialSizeIsBoundedBeforeSending(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { calls.Add(1); w.WriteHeader(http.StatusOK) }))
	defer server.Close()
	p := &WorkspacePolicy{Endpoint: server.URL + "/mcp", Projects: []string{"research"},
		TokenSource: func() (string, error) { return strings.Repeat("x", 8193), nil }}
	err := p.Call(context.Background(), "infrastructure__workspace_stop", map[string]any{"host": "saturn", "project_id": "research", "workspace_id": "ws1"})
	if err == nil || calls.Load() != 0 {
		t.Fatal("oversized vault credential reached backend")
	}
}
