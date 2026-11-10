package privileged

import "testing"

func TestWorkspaceMCPToolAcceptance(t *testing.T) {
    cases := []struct {
        name, body string
        ok bool
    }{
        {"implicit success", `{"jsonrpc":"2.0","id":1,"result":{"content":[]}}`, true},
        {"explicit success", `{"jsonrpc":"2.0","id":1,"result":{"content":[],"isError":false}}`, true},
        {"structured success", `{"jsonrpc":"2.0","id":1,"result":{"content":[{"type":"text","text":"accepted"}],"structuredContent":{"status":"queued"}}}`, true},
        {"tool error", `{"jsonrpc":"2.0","id":1,"result":{"content":[],"isError":true}}`, false},
        {"ambiguous error flag", `{"jsonrpc":"2.0","id":1,"result":{"content":[],"isError":null}}`, false},
        {"wrong error flag type", `{"jsonrpc":"2.0","id":1,"result":{"content":[],"isError":"false"}}`, false},
        {"missing required content", `{"jsonrpc":"2.0","id":1,"result":{"isError":false}}`, false},
        {"null content", `{"jsonrpc":"2.0","id":1,"result":{"content":null}}`, false},
        {"wrong content type", `{"jsonrpc":"2.0","id":1,"result":{"content":{}}}`, false},
        {"null block", `{"jsonrpc":"2.0","id":1,"result":{"content":[null]}}`, false},
        {"nonobject block", `{"jsonrpc":"2.0","id":1,"result":{"content":["text"]}}`, false},
        {"wrong ID", `{"jsonrpc":"2.0","id":2,"result":{"content":[]}}`, false},
        {"missing ID", `{"jsonrpc":"2.0","result":{"content":[]}}`, false},
        {"wrong protocol version", `{"jsonrpc":"1.0","id":1,"result":{"content":[]}}`, false},
        {"JSON-RPC error", `{"jsonrpc":"2.0","id":1,"error":{"code":-32000,"message":"rejected"}}`, false},
        {"mixed error and result", `{"jsonrpc":"2.0","id":1,"error":{"code":-32000},"result":{"content":[]}}`, false},
        {"result missing", `{"jsonrpc":"2.0","id":1}`, false},
        {"null result", `{"jsonrpc":"2.0","id":1,"result":null}`, false},
        {"invalid JSON", `{"jsonrpc":`, false},
        {"trailing response", `{"jsonrpc":"2.0","id":1,"result":{"content":[]}}{}`, false},
    }
    for _, tc := range cases {
        t.Run(tc.name, func(t *testing.T) {
            err := verifyWorkspaceToolResult([]byte(tc.body))
            if (err == nil) != tc.ok {
                t.Fatalf("success=%t, want %t: %v", err == nil, tc.ok, err)
            }
        })
    }
}
