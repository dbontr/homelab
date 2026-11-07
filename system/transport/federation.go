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

// forward sends a JSON-RPC request to a configured trusted service. It never follows redirects.
func (s *Server) forward(ctx context.Context, backend Backend, rpc rpcRequest, output *any) error {
    raw, err := json.Marshal(rpc)
    if err != nil { return err }
    req, err := http.NewRequestWithContext(ctx, http.MethodPost, backend.URL, bytes.NewReader(raw))
    if err != nil { return err }
    req.Header.Set("Content-Type", "application/json")
    req.Header.Set("Accept", "application/json, text/event-stream")
    req.Header.Set("MCP-Protocol-Version", "2025-06-18")
    if backend.Token != "" {req.Header.Set("Authorization", "Bearer "+backend.Token)}
    resp, err := s.client.Do(req)
    if err != nil { return fmt.Errorf("service %s unavailable: %w",backend.Name,err) }
    defer resp.Body.Close()
    if resp.StatusCode < 200 || resp.StatusCode >= 300 {return fmt.Errorf("service %s returned HTTP %d",backend.Name,resp.StatusCode)}
    body, err := io.ReadAll(io.LimitReader(resp.Body, 64<<20))
    if err != nil {return err}
    if strings.HasPrefix(resp.Header.Get("Content-Type"),"text/event-stream") {
        for _, line := range strings.Split(string(body), "\n") {if strings.HasPrefix(line,"data: "){body=[]byte(strings.TrimSpace(strings.TrimPrefix(line,"data: ")))}}
    }
    var res rpcResponse
    if err = json.Unmarshal(body,&res);err!=nil{return errors.New("invalid upstream JSON-RPC response")}
    if res.Error != nil {return fmt.Errorf("service %s: %s",backend.Name,res.Error.Message)}
    if len(res.Result)==0 {return errors.New("empty upstream result")}
    return json.Unmarshal(res.Result,output)
}
