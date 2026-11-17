package controller

import (
    "strings"
    "testing"
)

func TestWorkspaceOperationLookupRequiresExactScopedExecutionIdentity(t *testing.T) {
    db, _, srv := fixture(t)
    project(t, srv)
    token := pairWorker(t, srv)
    code, created := call(t, srv, "POST", "/api/workspaces", testOwner,
        map[string]any{"project_id": "demo", "cpu": 1, "memory_mb": 512})
    wantOK(t, code, created)
    workspaceID := asString(created["id"])
    start := claimOp(t, srv, token)
    if start["action"] != "start" { t.Fatalf("unexpected start: %v", start) }
    finishOp(t, srv, token, start, map[string]any{"status": "ready"})
    requestID := strings.Repeat("a", 32)
    queued, err := db.CallWorkspace("demo", workspaceID, "command",
        map[string]any{"command": "printf verified", "workspace_request_id": requestID})
    if err != nil { t.Fatal(err) }
    found, err := db.OperationByRequest("demo", workspaceID, requestID)
    if err != nil || found["id"] != queued["operation_id"] || found["status"] != "queued" {
        t.Fatalf("request lookup lost the queued operation: %v %v", found, err)
    }
    for _, testcase := range []struct{ project, workspace, id string }{
        {"other", workspaceID, requestID},
        {"demo", "other", requestID},
        {"demo", workspaceID, strings.Repeat("b", 32)},
        {"demo", workspaceID, strings.Repeat("A", 32)},
        {"demo", workspaceID, "not-hex"},
    } {
        if _, err := db.OperationByRequest(testcase.project, testcase.workspace, testcase.id); err == nil {
            t.Errorf("unauthorized or mismatched operation found: %+v", testcase)
        }
    }
    operation := claimOp(t, srv, token)
    if operation["action"] != "command" { t.Fatalf("unexpected operation: %v", operation) }
    finishOp(t, srv, token, operation, map[string]any{"stdout": "verified", "exit_code": 0})
    finished, err := db.OperationByRequest("demo", workspaceID, requestID)
    if err != nil || finished["status"] != "succeeded" ||
        gitObj(finished["result"])["stdout"] != "verified" {
        t.Fatalf("completed command not recoverable: %v %v", finished, err)
    }
    if _, err := db.CallWorkspace("demo", workspaceID, "command",
        map[string]any{"command": "printf repeated", "workspace_request_id": requestID}); err != nil {
        t.Fatal(err)
    }
    if _, err := db.OperationByRequest("demo", workspaceID, requestID); err == nil {
        t.Fatal("ambiguous duplicate executor request was not rejected")
    }
}
