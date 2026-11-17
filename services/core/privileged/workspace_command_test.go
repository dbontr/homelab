package privileged

import (
    "strings"
    "testing"
)

func TestWorkspaceCommandConfinedToAllowedProjectAndRequest(t *testing.T) {
    policy := &WorkspacePolicy{Projects: []string{"research"}}
    requestID := strings.Repeat("a", 32)
    valid := map[string]any{
        "host": "saturn", "project_id": "research", "workspace_id": "ws1",
        "command": "cargo test --locked && lake build",
        "workspace_request_id": requestID,
    }
    name, arguments, err := policy.operationArgs("infrastructure__workspace_command", valid)
    if err != nil || name != "workspace_call" {
        t.Fatalf("approved isolated command refused: %v, %v", name, err)
    }
    if arguments["host"] != nil || arguments["action"] != "command" {
        t.Fatalf("privileged host escaped into workspace action: %v", arguments)
    }
    payload, ok := arguments["payload"].(map[string]any)
    if !ok || payload["workspace_request_id"] != requestID ||
        payload["command"] != valid["command"] {
        t.Fatalf("request identity not preserved: %v", arguments)
    }
    for _, tc := range []struct {
        key string
        value any
    }{
        {"project_id", "foreign"},
        {"workspace_id", "../host"},
        {"command", ""},
        {"command", " \t\n"},
        {"command", "echo x\x00y"},
        {"command", strings.Repeat("x", 4097)},
        {"workspace_request_id", "bad"},
        {"workspace_request_id", strings.Repeat("A", 32)},
        {"unknown_field", "bypass"},
    } {
        candidate := make(map[string]any, len(valid)+1)
        for key, value := range valid { candidate[key] = value }
        candidate[tc.key] = tc.value
        if _, _, err := policy.operationArgs("infrastructure__workspace_command", candidate); err == nil {
            t.Errorf("unapproved request accepted: %s", tc.key)
        }
    }
    delete(valid, "workspace_request_id")
    if _, _, err := policy.operationArgs("infrastructure__workspace_command", valid); err == nil {
        t.Fatal("execution request without a gateway-owned identity was allowed")
    }
}
