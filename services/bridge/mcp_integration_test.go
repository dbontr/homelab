//go:build integration

package bridge_test

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/dbontr/homelab-new/services/bridge"
	"github.com/dbontr/homelab-new/system/store"
	"github.com/dbontr/homelab-new/system/transport"
)

const (
	publicCredential  = "integration-public-mcp-token-0123456789abcdef"
	serviceCredential = "integration-private-domain-token-0123456789abcdef"
	backendCredential = "integration-private-learning-token-0123456789abcdef"
)

type rpcResult struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Result  json.RawMessage `json:"result"`
	Error   json.RawMessage `json:"error"`
}

func rpcCall(t *testing.T, client *http.Client, endpoint, token, origin, payload string) (int, rpcResult) {
	t.Helper()
	req, err := http.NewRequest(http.MethodPost, endpoint+"/mcp", strings.NewReader(payload))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	if origin != "" {
		req.Header.Set("Origin", origin)
	}
	response, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	data, err := io.ReadAll(io.LimitReader(response.Body, 1<<20))
	if err != nil {
		t.Fatal(err)
	}
	var decoded rpcResult
	if response.StatusCode == http.StatusOK {
		if err := json.Unmarshal(data, &decoded); err != nil {
			t.Fatalf("invalid MCP response: %v", err)
		}
		if decoded.JSONRPC != "2.0" {
			t.Fatalf("invalid RPC protocol: %s", data)
		}
	}
	if response.Header.Get("Set-Cookie") != "" {
		t.Fatal("upstream cookie escaped the MCP interface")
	}
	return response.StatusCode, decoded
}

func TestMCPInterfaceEndToEnd(t *testing.T) {
	var remoteCalls atomic.Int32
	remote := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer "+backendCredential {
			t.Errorf("federated backend saw incorrect credential")
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		if r.Header.Get("Cookie") != "" || r.Header.Get("Cf-Access-Jwt-Assertion") != "" ||
			r.Header.Get("X-Forwarded-Host") != "" {
			t.Error("untrusted client headers reached federated service")
		}
		var input struct {
			ID     json.RawMessage `json:"id"`
			Method string          `json:"method"`
		}
		if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
			t.Error(err)
			http.Error(w, "invalid request", http.StatusBadRequest)
			return
		}
		w.Header().Set("Set-Cookie", "backend=invalid")
		remoteCalls.Add(1)
		switch input.Method {
		case "tools/list":
			w.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(w, `{"jsonrpc":"2.0","id":1,"result":{"tools":[{"name":"inspection","description":"Read-only inspection","inputSchema":{"type":"object"}}]}}`)
		case "tools/call":
			w.Header().Set("Content-Type", "text/event-stream")
			data := map[string]any{
				"jsonrpc": "2.0", "id": input.ID,
				"result": map[string]any{"content": []map[string]string{{"type": "text", "text": "delegation-ok"}}},
			}
			encoded, err := json.Marshal(data)
			if err != nil {
				t.Error(err)
				return
			}
			_, _ = io.WriteString(w, "event: message\ndata: "+string(encoded)+"\n\n")
		default:
			http.Error(w, "unknown method", http.StatusBadRequest)
		}
	}))
	defer remote.Close()

	db, err := store.Open(filepath.Join(t.TempDir(), "state.json"), "UTC")
	if err != nil {
		t.Fatal(err)
	}
	core, err := transport.New(db, serviceCredential, []transport.Backend{
		{Name: "learning", URL: remote.URL, Token: backendCredential},
	})
	if err != nil {
		t.Fatal(err)
	}
	coreServer := httptest.NewServer(core.Handler())
	defer coreServer.Close()

	adapter, err := bridge.New(bridge.Config{
		Surface: bridge.MCP, BackendURL: coreServer.URL,
		ClientToken: publicCredential, ServiceToken: serviceCredential,
		Origins: []string{"https://approved.example"},
	})
	if err != nil {
		t.Fatal(err)
	}
	public := httptest.NewServer(adapter)
	defer public.Close()
	client := public.Client()
	init := `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18","capabilities":{},"clientInfo":{"name":"integration","version":"1.0"}}}`

	for _, tc := range []struct {
		name, token, origin string
		want                int
	}{
		{"anonymous", "", "", http.StatusUnauthorized},
		{"incorrect client credential", backendCredential, "", http.StatusUnauthorized},
		{"unapproved origin", publicCredential, "https://other.example", http.StatusForbidden},
		{"approved", publicCredential, "https://approved.example", http.StatusOK},
	} {
		t.Run(tc.name, func(t *testing.T) {
			status, value := rpcCall(t, client, public.URL, tc.token, tc.origin, init)
			if status != tc.want {
				t.Fatalf("HTTP %d, want %d", status, tc.want)
			}
			if status == http.StatusOK && !bytes.Contains(value.Result, []byte("2025-06-18")) {
				t.Fatal("service protocol initialization failed")
			}
		})
	}
	if remoteCalls.Load() != 0 {
		t.Fatal("remote backend contacted before tool discovery")
	}

	status, listed := rpcCall(t, client, public.URL, publicCredential, "", `{"jsonrpc":"2.0","id":2,"method":"tools/list","params":{}}`)
	if status != http.StatusOK || !bytes.Contains(listed.Result, []byte("task_add")) ||
		!bytes.Contains(listed.Result, []byte("learning__inspection")) {
		t.Fatalf("tool catalog is incomplete: HTTP %d %s", status, listed.Result)
	}

	status, created := rpcCall(t, client, public.URL, publicCredential, "", `{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"task_add","arguments":{"title":"MCP integration acceptance"}}}`)
	if status != http.StatusOK || !bytes.Contains(created.Result, []byte("MCP integration acceptance")) {
		t.Fatalf("authorized tool did not execute: HTTP %d %s", status, created.Result)
	}
	status, tasks := rpcCall(t, client, public.URL, publicCredential, "", `{"jsonrpc":"2.0","id":4,"method":"tools/call","params":{"name":"task_list","arguments":{}}}`)
	if status != http.StatusOK || !bytes.Contains(tasks.Result, []byte("MCP integration acceptance")) {
		t.Fatalf("tool result was not persisted: HTTP %d %s", status, tasks.Result)
	}

	status, forwarded := rpcCall(t, client, public.URL, publicCredential, "", `{"jsonrpc":"2.0","id":5,"method":"tools/call","params":{"name":"learning__inspection","arguments":{}}}`)
	if status != http.StatusOK || !bytes.Contains(forwarded.Result, []byte("delegation-ok")) {
		t.Fatalf("delegated tool did not complete: HTTP %d %s", status, forwarded.Result)
	}

	before := remoteCalls.Load()
	status, denied := rpcCall(t, client, public.URL, publicCredential, "", `{"jsonrpc":"2.0","id":6,"method":"tools/call","params":{"name":"learning__unadvertised","arguments":{}}}`)
	if status != http.StatusOK || !bytes.Contains(denied.Result, []byte(`"isError":true`)) {
		t.Fatalf("unadvertised backend tool was not rejected: HTTP %d %s", status, denied.Result)
	}
	if remoteCalls.Load() != before+1 {
		t.Fatal("unadvertised tool was dispatched instead of only checked through discovery")
	}

	req, err := http.NewRequest(http.MethodPost, coreServer.URL+"/mcp", strings.NewReader(init))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Authorization", "Bearer "+publicCredential)
	resp, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("public MCP token accessed the private service: %d", resp.StatusCode)
	}
}
