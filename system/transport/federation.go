package transport

import (
    "bytes"
    "context"
    "encoding/json"
    "errors"
    "fmt"
    "io"
    "net/http"
    "strings"
)

const maxFederatedResponse = 8 << 20

// forward accepts only a bounded JSON-RPC reply for the requested operation.
func (s *Server) forward(ctx context.Context, backend Backend, rpc rpcRequest, output *any) error {
    raw, err := json.Marshal(rpc)
    if err != nil { return err }
    req, err := http.NewRequestWithContext(ctx, http.MethodPost, backend.URL, bytes.NewReader(raw))
    if err != nil { return errors.New("invalid backend request") }
    req.Header.Set("Content-Type", "application/json")
    req.Header.Set("Accept", "application/json, text/event-stream")
    req.Header.Set("MCP-Protocol-Version", "2025-06-18")
    if backend.Token != "" { req.Header.Set("Authorization", "Bearer "+backend.Token) }
    resp, err := s.client.Do(req)
    if err != nil { return fmt.Errorf("service %s unavailable", backend.Name) }
    defer resp.Body.Close()
    if resp.StatusCode < 200 || resp.StatusCode >= 300 {
        return fmt.Errorf("service %s returned HTTP %d", backend.Name, resp.StatusCode)
    }
    body, err := io.ReadAll(io.LimitReader(resp.Body, maxFederatedResponse+1))
    if err != nil || len(body) > maxFederatedResponse {
        return fmt.Errorf("service %s returned an excessive or unreadable response", backend.Name)
    }
    if strings.HasPrefix(resp.Header.Get("Content-Type"), "text/event-stream") {
        body, err = streamReply(body, rpc.ID)
        if err != nil { return fmt.Errorf("service %s returned invalid event data", backend.Name) }
    }
    var response rpcResponse
    if err := json.Unmarshal(body, &response); err != nil || response.JSONRPC != "2.0" {
        return errors.New("invalid upstream JSON-RPC response")
    }
    if !bytes.Equal(bytes.TrimSpace(response.ID), bytes.TrimSpace(rpc.ID)) {
        return errors.New("upstream JSON-RPC response ID mismatch")
    }
    if response.Error != nil {
        return fmt.Errorf("service %s reported an RPC error (%d)", backend.Name, response.Error.Code)
    }
    if len(response.Result) == 0 {
        return errors.New("empty upstream result")
    }
    if err := json.Unmarshal(response.Result, output); err != nil {
        return errors.New("invalid upstream tool result")
    }
    return nil
}

// streamReply returns only a complete JSON-RPC reply matching the outbound request ID.
// Notifications and unrelated event data are never treated as a tool response.
func streamReply(stream []byte, id json.RawMessage) ([]byte, error) {
    lines := strings.Split(strings.ReplaceAll(string(stream), "\r\n", "\n"), "\n")
    data := make([]string, 0, 4)
    var reply []byte
    finish := func() {
        if len(data) == 0 { return }
        candidate := []byte(strings.Join(data, "\n"))
        data = data[:0]
        var rpc rpcResponse
        if json.Unmarshal(candidate, &rpc) == nil &&
           rpc.JSONRPC == "2.0" && bytes.Equal(bytes.TrimSpace(rpc.ID), bytes.TrimSpace(id)) {
            reply = append([]byte(nil), candidate...)
        }
    }
    for _,line := range lines {
        if line == "" { finish(); continue }
        if strings.HasPrefix(line, "data:") {
            value := strings.TrimPrefix(line, "data:")
            data = append(data, strings.TrimPrefix(value, " "))
        }
    }
    finish()
    if reply == nil { return nil, errors.New("no matching JSON-RPC event") }
    return reply, nil
}
