package agentflow

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// The gateway, decision model, compute RPC and workflow state communicate
// through their production transport contracts. Only the remote providers are
// replaced with deterministic HTTP fixtures.
func TestAutonomousEngineeringHTTPAndUnixEndToEnd(t *testing.T) {
	worker := &fakeCompute{files: map[string]string{"repos/example__repo/README.md": "package sample"}}
	serviceToken := strings.Repeat("s", 40)
	identityChecks, grantChecks, rpcCalls := 0, 0, 0
	compute := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer "+serviceToken {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		if r.URL.Path == "/api/whoami" {
			identityChecks++
			_ = json.NewEncoder(w).Encode(map[string]any{"principal": "oidc:acceptance#project-editor", "administrator": false})
			return
		}
		if r.URL.Path == "/api/projects/research/role" {
			grantChecks++
			_ = json.NewEncoder(w).Encode(map[string]any{"project_id": "research", "role": "editor"})
			return
		}
		if r.URL.Path != "/mcp" || r.Method != http.MethodPost {
			http.NotFound(w, r)
			return
		}
		var req struct {
			ID     int
			Method string
			Params struct {
				Name      string
				Arguments map[string]any
			}
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.ID != 1 || req.Method != "tools/call" {
			http.Error(w, "invalid RPC request", http.StatusBadRequest)
			return
		}
		rpcCalls++
		result, err := worker.Call(r.Context(), req.Params.Name, req.Params.Arguments)
		if err != nil {
			_ = json.NewEncoder(w).Encode(map[string]any{"jsonrpc": "2.0", "id": 1, "error": map[string]any{"code": -32000, "message": "rejected"}})
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"jsonrpc": "2.0", "id": 1, "result": map[string]any{"isError": false, "structuredContent": result}})
	}))
	defer compute.Close()

	decisions := agentDecisions()
	modelCalls := 0
	model := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/chat/completions" || r.Method != http.MethodPost || modelCalls >= len(decisions) {
			http.Error(w, "invalid model request", http.StatusBadRequest)
			return
		}
		decision, err := json.Marshal(decisions[modelCalls])
		if err != nil {
			t.Error(err)
			return
		}
		modelCalls++
		_ = json.NewEncoder(w).Encode(map[string]any{"choices": []map[string]any{{"message": map[string]string{"content": string(decision)}, "finish_reason": "stop"}}})
	}))
	defer model.Close()

	tokenFile := filepath.Join(t.TempDir(), "compute-editor-token")
	if err := os.WriteFile(tokenFile, []byte(serviceToken+"\n"), 0600); err != nil {
		t.Fatal(err)
	}
	computeClient, err := NewComputeClientFromFile(compute.URL+"/mcp", tokenFile)
	if err != nil {
		t.Fatal(err)
	}
	if err := computeClient.VerifyProjectCredential(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := computeClient.VerifyProjectEditor(context.Background(), "research"); err != nil {
		t.Fatal(err)
	}
	author, err := NewModelAuthor(model.URL+"/v1/chat/completions", "acceptance-model", "")
	if err != nil {
		t.Fatal(err)
	}
	state := testStore(t)
	engine := &Engine{Store: state, Compute: computeClient, Author: author, AllowedProjects: map[string]bool{"research": true}}

	socket := filepath.Join(t.TempDir(), "engineering.sock")
	if err := os.Chmod(filepath.Dir(socket), 0700); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	finished := make(chan error, 1)
	go func() { finished <- ServeUnix(ctx, socket, engine) }()
	gateway, err := NewClient(socket)
	if err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(3 * time.Second)
	for {
		if _, err := gateway.List(); err == nil {
			break
		}
		if time.Now().After(deadline) {
			select {
			case err := <-finished:
				t.Fatalf("engineering socket failed: %v", err)
			default:
				t.Fatal("engineering socket unavailable")
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	initial, err := gateway.Submit(autonomousProgram())
	if err != nil {
		t.Fatal(err)
	}
	runner := Runner{Store: state, Worker: engine, Lease: time.Minute}
	for i := 0; i < 15; i++ {
		current, err := gateway.Get(initial.ID)
		if err != nil {
			t.Fatal(err)
		}
		if current.Status == Succeeded {
			break
		}
		advanced, err := runner.RunOne(context.Background(), initial.ID)
		if err != nil || !advanced {
			t.Fatalf("step %d: advanced=%v err=%v status=%s", i, advanced, err, current.Status)
		}
	}
	final, err := gateway.Get(initial.ID)
	if err != nil {
		t.Fatal(err)
	}
	if final.Status != Succeeded || worker.drafts != 1 || modelCalls != len(decisions) {
		t.Fatalf("autonomous RPC integration failed: status=%s drafts=%d model_calls=%d", final.Status, worker.drafts, modelCalls)
	}
	if rpcCalls < 5 || identityChecks < rpcCalls || grantChecks < rpcCalls {
		t.Fatalf("project-scoped credential gate was bypassed: rpc=%d identity=%d grant=%d", rpcCalls, identityChecks, grantChecks)
	}
	if worker.exported["artifact1"] != "chapter sample" {
		t.Fatalf("verified source was not exported: %q", worker.exported["artifact1"])
	}
	if final.Agent == nil || !final.Agent.Completed || len(final.Agent.ChangedPaths) != 1 {
		t.Fatalf("durable autonomous progress missing: %+v", final.Agent)
	}
	if strings.Contains(mustJSON(final), "chapter sample") || strings.Contains(mustJSON(final), "package sample") {
		t.Fatal("private source leaked through gateway")
	}
	if err := gateway.Cancel(initial.ID, final.Revision); err == nil {
		t.Fatal("completed run was reclassified as cancelled")
	}
	cancel()
	select {
	case err := <-finished:
		if err != nil && !errors.Is(err, context.Canceled) {
			t.Fatalf("Unix service shutdown failed: %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("Unix service did not stop")
	}
}
