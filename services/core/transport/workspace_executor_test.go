package transport

import (
    "context"
    "encoding/json"
    "net"
    "net/http"
    "path/filepath"
    "strings"
    "testing"

    "github.com/dbontr/homelab-new/system/approval"
    "github.com/dbontr/homelab-new/system/privileged"
    "github.com/dbontr/homelab-new/system/security"
)

func TestWorkspaceCommandRequiresExecutorAndGatewayOwnedIdentity(t *testing.T) {
    socket := filepath.Join(t.TempDir(), "executor.sock")
    listener, err := net.Listen("unix", socket)
    if err != nil { t.Fatal(err) }
    defer listener.Close()
    requests := make(chan map[string]any, 1)
    server := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        if r.URL.Path != "/v1/execute" || r.Header.Get("Cf-Access-Jwt-Assertion") != "signed-owner" {
            http.Error(w, "invalid owner channel", http.StatusForbidden)
            return
        }
        var payload map[string]any
        if err := json.NewDecoder(r.Body).Decode(&payload); err != nil { t.Error(err) }
        requests <- payload
        w.Header().Set("Content-Type", "application/json")
        _, _ = w.Write([]byte(`{"allowed":false,"reason":"independent owner approval required"}`))
    })}
    go func(){ _ = server.Serve(listener) }()
    defer server.Close()
    client, err := privileged.NewClient(socket)
    if err != nil { t.Fatal(err) }
    gateway := &Server{executorClient: client}
    requestID := strings.Repeat("b", 32)
    input := map[string]any{
        "host": "saturn", "project_id": "research", "workspace_id": "ws1",
        "command": "lean --version", "request_id": requestID,
    }
    body, _ := json.Marshal(input)
    ctx := security.WithAccessAssertion(context.Background(), "signed-owner")
    got := gateway.invoke(ctx, "infrastructure__workspace_command", body, json.RawMessage("1"))
    if got["isError"] == true { t.Fatalf("valid command blocked before owner approval: %v", got) }
    select {
    case request := <-requests:
        if request["operation"] != "infrastructure__workspace_command" ||
            request["request_id"] != requestID || request["target"] != "saturn" {
            t.Fatalf("executor request lost identity or target: %v", request)
        }
        args, ok := request["arguments"].(map[string]any)
        if !ok || args["workspace_request_id"] != requestID || args["request_id"] != nil {
            t.Fatalf("workspace receipt not bound to executor identity: %v", args)
        }
    default: t.Fatal("workspace command bypassed executor")
    }
    input["workspace_request_id"] = strings.Repeat("c", 32)
    forged, _ := json.Marshal(input)
    if gateway.invoke(ctx, "infrastructure__workspace_command", forged,
        json.RawMessage("2"))["isError"] != true {
        t.Fatal("caller forged workspace request identity")
    }
    if (&Server{}).invoke(ctx, "infrastructure__workspace_command", body,
        json.RawMessage("3"))["isError"] != true {
        t.Fatal("workspace command without independent executor was accepted")
    }
    find := func(server *Server, target string) bool {
        for _, entry := range server.localTools() {
            if entry["name"] == target { return true }
        }
        return false
    }
    if approval.ScopeFor("workspace__workspace_call") != "compute.execute" ||
       approval.ScopeFor("workspace__workspace_operation_by_request") != "" {
        t.Fatal("mutable controller actions were exposed or request lookup was denied")
    }
    for _, operation := range []string{
        "infrastructure__workspace_create", "infrastructure__workspace_command",
    } {
        if !find(gateway, operation) { t.Fatalf("missing approved workspace tool: %s", operation) }
        if find(&Server{}, operation) { t.Fatalf("workspace operation leaked without executor: %s", operation) }
    }
}
