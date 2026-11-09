package agentflow

import (
 "bytes"
 "context"
 "encoding/json"
 "errors"
 "fmt"
 "io"
 "net"
 "net/http"
 "net/url"
 "strings"
 "time"
)

// ComputeClient is restricted to project-scoped engineering operations.
// It cannot invoke service lifecycle, hardware administration, deployment,
// or GitHub merge operations through the compute controller.
type ComputeClient struct {
 Endpoint string
 token string
 http *http.Client
}
var permittedComputeTools=map[string]bool{
 "workspace_create":true,"workspace_list":true,"workspace_call":true,
 "workspace_operation":true,"github_checkout_repository":true,
 "github_publish_artifacts":true,
}
func NewComputeClient(endpoint,token string)(*ComputeClient,error){
 u,err:=url.Parse(endpoint)
 if err!=nil||u.Host==""||u.User!=nil||u.RawQuery!=""||u.Fragment!=""||u.Path!="/mcp"||
 len(token)<32||strings.ContainsAny(token,"\r\n"){return nil,errors.New("invalid compute endpoint or service credential")}
 ip:=net.ParseIP(u.Hostname())
 local:=u.Hostname()=="localhost"||(ip!=nil&&ip.IsLoopback())
 if u.Scheme!="https"&&!(u.Scheme=="http"&&local){return nil,errors.New("compute endpoint requires HTTPS or loopback HTTP")}
 return &ComputeClient{
  Endpoint:endpoint,token:token,
  http:&http.Client{Timeout:120*time.Second,CheckRedirect:func(*http.Request,[]*http.Request)error{return errors.New("compute redirects are not permitted")}},
 },nil
}
func(c *ComputeClient)Call(ctx context.Context,tool string,arguments map[string]any)(map[string]any,error){
 if c==nil||c.http==nil||!permittedComputeTools[tool]{return nil,errors.New("compute operation is not authorized")}
 body,err:=json.Marshal(map[string]any{
  "jsonrpc":"2.0","id":1,"method":"tools/call",
  "params":map[string]any{"name":tool,"arguments":arguments},
 })
 if err!=nil||len(body)>128<<10{return nil,errors.New("compute request exceeds limit")}
 req,err:=http.NewRequestWithContext(ctx,http.MethodPost,c.Endpoint,bytes.NewReader(body));if err!=nil{return nil,err}
 req.Header.Set("Content-Type","application/json")
 req.Header.Set("Accept","application/json")
 req.Header.Set("MCP-Protocol-Version","2025-06-18")
 req.Header.Set("Authorization","Bearer "+c.token)
 response,err:=c.http.Do(req);if err!=nil{return nil,errors.New("compute service unavailable")}
 defer response.Body.Close()
 if response.StatusCode!=http.StatusOK{return nil,fmt.Errorf("compute service returned HTTP %d",response.StatusCode)}
 raw,err:=io.ReadAll(io.LimitReader(response.Body,8<<20+1));if err!=nil||len(raw)>8<<20{return nil,errors.New("invalid compute response size")}
 var result struct{
  ID json.RawMessage `json:"id"`
  JSONRPC string `json:"jsonrpc"`
  Error *struct{Code int `json:"code"`} `json:"error"`
  Result struct{
   IsError bool `json:"isError"`
   Structured map[string]any `json:"structuredContent"`
  } `json:"result"`
 }
 if err:=json.Unmarshal(raw,&result);err!=nil||result.JSONRPC!="2.0"||string(result.ID)!="1"||result.Error!=nil{
  return nil,errors.New("invalid compute RPC response")
 }
 if result.Result.IsError||result.Result.Structured==nil{return nil,errors.New("compute operation failed or was denied")}
 return result.Result.Structured,nil
}

// VerifyProjectCredential denies administrator credentials before autonomous
// execution. Controller-side project grants remain authoritative on every call.
func (c *ComputeClient) VerifyProjectCredential(ctx context.Context) error {
 if c==nil||c.http==nil{return errors.New("compute service is not configured")}
 target:=strings.TrimSuffix(c.Endpoint,"/mcp")+"/api/whoami"
 req,err:=http.NewRequestWithContext(ctx,http.MethodGet,target,nil)
 if err!=nil{return err}
 req.Header.Set("Authorization","Bearer "+c.token)
 response,err:=c.http.Do(req)
 if err!=nil{return errors.New("unable to verify compute service identity")}
 defer response.Body.Close()
 if response.StatusCode!=http.StatusOK{return fmt.Errorf("compute identity verification failed (HTTP %d)",response.StatusCode)}
 reader:=io.LimitReader(response.Body,4097)
 data,err:=io.ReadAll(reader)
 if err!=nil||len(data)>4096{return errors.New("invalid compute identity response")}
 var profile struct {
  Principal string `json:"principal"`
  Administrator bool `json:"administrator"`
 }
 if err:=json.Unmarshal(data,&profile);err!=nil||profile.Principal==""||profile.Administrator{
  return errors.New("engineering requires a non-administrator project credential")
 }
 return nil
}
