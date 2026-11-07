package transport

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/dbontr/homelab-new/system/media"
	"github.com/dbontr/homelab-new/system/store"
)

func TestMediaOperationsShareNativeAndMCPInterfaces(t *testing.T) {
	var requested atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Api-Key") != "media-secret" {
			t.Error("protected API credential missing")
		}
		if r.URL.Path == "/api/v1/request" && r.Method == http.MethodPost {
			requested.Add(1)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"results":[{"title":"Example"}]}`))
	}))
	defer upstream.Close()
	db, err := store.Open(filepath.Join(t.TempDir(), "state.json"), "UTC")
	if err != nil {
		t.Fatal(err)
	}
	server, err := New(db, strings.Repeat("d", 40), nil)
	if err != nil {
		t.Fatal(err)
	}
	client, err := media.NewSeerr(upstream.URL, "media-secret")
	if err != nil {
		t.Fatal(err)
	}
	server.ConfigureMedia(client)
	token := strings.Repeat("d", 40)
	w := request(server.Handler(), "GET", "/v1/media/search?query=Example", "", token)
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), "Example") {
		t.Fatal("native search failed")
	}
	w = request(server.Handler(), "POST", "/mcp", `{"jsonrpc":"2.0","id":1,"method":"tools/list"}`, token)
	if !strings.Contains(w.Body.String(), "media_request") {
		t.Fatal("media tools not discoverable")
	}
	w = request(server.Handler(), "POST", "/mcp", `{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"media_request","arguments":{"mediaType":"movie","mediaId":5}}}`, token)
	if !strings.Contains(w.Body.String(), "Example") || requested.Load() != 1 {
		t.Fatalf("media request failed: %s", w.Body.String())
	}
}
