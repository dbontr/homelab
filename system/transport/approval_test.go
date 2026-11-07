package transport

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/dbontr/homelab-new/system/store"
)

func TestClusterChangeRequiresExactOneTimeApproval(t *testing.T) {
	var forwarded atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var request rpcRequest
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Error(err)
		}
		if request.Method == "tools/call" {
			forwarded.Add(1)
			_, _ = w.Write([]byte(`{"jsonrpc":"2.0","id":1,"result":{"content":[{"type":"text","text":"changed"}]}}`))
			return
		}
		_, _ = w.Write([]byte(`{"jsonrpc":"2.0","id":1,"result":{"tools":[{"name":"configure","description":"Change cluster","inputSchema":{"type":"object"}}]}}`))
	}))
	defer upstream.Close()
	db, err := store.Open(filepath.Join(t.TempDir(), "store.json"), "UTC")
	if err != nil {
		t.Fatal(err)
	}
	server, err := New(db, strings.Repeat("c", 40), []Backend{{Name: "cluster", URL: upstream.URL}})
	if err != nil {
		t.Fatal(err)
	}
	token := strings.Repeat("c", 40)
	payload := `{"jsonrpc":"2.0","id":7,"method":"tools/call","params":{"name":"cluster__configure","arguments":{"host":"saturn","action":"update"}}}`
	w := request(server.Handler(), "POST", "/mcp", payload, token)
	if forwarded.Load() != 0 {
		t.Fatal("cluster change forwarded without approval")
	}
	var result struct {
		Result struct {
			Content []struct {
				Text string `json:"text"`
			} `json:"content"`
		} `json:"result"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if len(result.Result.Content) == 0 {
		t.Fatal("approval challenge missing")
	}
	var denied struct {
		Challenge struct {
			ID string `json:"id"`
		} `json:"challenge"`
		ApprovalRequired bool `json:"approval_required"`
	}
	if err := json.Unmarshal([]byte(result.Result.Content[0].Text), &denied); err != nil {
		t.Fatal(err)
	}
	if !denied.ApprovalRequired || denied.Challenge.ID == "" {
		t.Fatal("approval request invalid")
	}
	if list := request(server.AdminHandler(), "GET", "/approvals", "", ""); !strings.Contains(list.Body.String(), denied.Challenge.ID) {
		t.Fatal("operator cannot inspect pending request")
	}
	if grant := request(server.AdminHandler(), "POST", "/approvals/"+denied.Challenge.ID, "", ""); grant.Code != http.StatusOK {
		t.Fatalf("operator approval failed: %s", grant.Body.String())
	}
	changed := strings.ReplaceAll(payload, `"action":"update"`, `"action":"delete"`)
	_ = request(server.Handler(), "POST", "/mcp", changed, token)
	if forwarded.Load() != 0 {
		t.Fatal("changed arguments bypassed approval")
	}
	w = request(server.Handler(), "POST", "/mcp", payload, token)
	if forwarded.Load() != 1 || !strings.Contains(w.Body.String(), "changed") {
		t.Fatal("approved request failed")
	}
	_ = request(server.Handler(), "POST", "/mcp", payload, token)
	if forwarded.Load() != 1 {
		t.Fatal("approval was reused")
	}
}
