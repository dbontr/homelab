package transport

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"path/filepath"
	"sync/atomic"
	"testing"

	"github.com/dbontr/homelab-new/system/privileged"
)

func TestPrivilegedToolResponsesPreserveExecutionState(t *testing.T) {
	socket := filepath.Join(t.TempDir(), "executor.sock")
	listener, err := net.Listen("unix", socket)
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	var outcome atomic.Value
	outcome.Store(`{"allowed":true,"state":"accepted","audit_id":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}`)
	srv := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/v1/execute" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(outcome.Load().(string)))
	})}
	defer srv.Close()
	go func() { _ = srv.Serve(listener) }()
	client, err := privileged.NewClient(socket)
	if err != nil {
		t.Fatal(err)
	}
	server := &Server{executorClient: client}
	call := func() map[string]any {
		t.Helper()
		return server.invoke(context.Background(), "infrastructure__service_restart",
			json.RawMessage(`{"host":"saturn","service":"homelab-platform.service"}`), json.RawMessage("1"))
	}
	decode := func(response map[string]any) map[string]any {
		t.Helper()
		parts, ok := response["content"].([]any)
		if !ok || len(parts) != 1 {
			t.Fatalf("unexpected tool response: %+v", response)
		}
		var content string
		switch part := parts[0].(type) {
		case map[string]string:
			content = part["text"]
		case map[string]any:
			content, _ = part["text"].(string)
		}
		if content == "" {
			t.Fatalf("missing tool text: %+v", response)
		}
		var decoded map[string]any
		if err := json.Unmarshal([]byte(content), &decoded); err != nil {
			t.Fatal(err)
		}
		return decoded
	}
	got := decode(call())
	if got["accepted"] != true || got["state"] != "accepted" || got["executed"] != nil {
		t.Fatalf("submitted action was misreported as completed: %+v", got)
	}
	outcome.Store(`{"allowed":true,"state":"completed","audit_id":"bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"}`)
	got = decode(call())
	if got["executed"] != true || got["completed"] != true || got["state"] != "completed" {
		t.Fatalf("verified execution was not reported: %+v", got)
	}
	outcome.Store(`{"allowed":true}`)
	response := call()
	if failed, _ := response["isError"].(bool); !failed {
		t.Fatalf("invalid executor response was trusted: %+v", response)
	}
}
