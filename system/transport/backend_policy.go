package transport

import (
    "context"
    "encoding/json"
    "strings"
    "time"
)

func validIntegrationName(name string) bool {
    if name == "" || len(name) > 48 { return false }
    for i, r := range name {
        if r >= 'a' && r <= 'z' { continue }
        if i > 0 && r >= '0' && r <= '9' { continue }
        if i > 0 && r == '-' { continue }
        return false
    }
    return true
}

func validBackendToolName(name string) bool {
    if name == "" || len(name) > 128 || strings.Contains(name,"__") { return false }
    for i, r := range name {
        if r >= 'a' && r <= 'z' { continue }
        if i > 0 && ((r >= '0' && r <= '9') || r == '_') { continue }
        return false
    }
    return true
}

// backendHasTool prevents a caller from reaching unadvertised backend operations.
// Absence of discovery is a denial, not a reason to forward an arbitrary tool name.
func (s *Server) backendHasTool(ctx context.Context, backend Backend, name string) bool {
    if !validBackendToolName(name) { return false }
    ctx, cancel := context.WithTimeout(ctx, 4*time.Second)
    defer cancel()
    var remote any
    if err := s.forward(ctx, backend, rpcRequest{JSONRPC:"2.0",ID:json.RawMessage("1"),Method:"tools/list"}, &remote); err != nil {
        return false
    }
    result, ok := remote.(map[string]any)
    if !ok { return false }
    list, ok := result["tools"].([]any)
    if !ok || len(list) > 512 { return false }
    for _, item := range list {
        tool, ok := item.(map[string]any)
        if !ok { continue }
        advertised, ok := tool["name"].(string)
        if ok && advertised == name { return true }
    }
    return false
}
