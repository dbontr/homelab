package privileged

import (
 "context"
 "encoding/json"
 "net/http"
 "net/http/httptest"
 "os"
 "path/filepath"
 "strings"
 "testing"

 "github.com/dbontr/homelab-new/system/security"
)

func testWorkspacePolicy(t *testing.T,handler http.Handler)(*WorkspacePolicy,func()){
 t.Helper()
 server:=httptest.NewServer(handler)
 path:=filepath.Join(t.TempDir(),"root-token")
 if err:=os.WriteFile(path,[]byte(strings.Repeat("k",48)+"\n"),0600);err!=nil{t.Fatal(err)}
 return &WorkspacePolicy{
  Endpoint:server.URL+"/mcp",TokenFile:path,Client:server.Client(),
  Projects:[]string{"research"},Workers:[]string{"jupiter"},
  Images:[]string{"approved-image"},Software:[]string{"approved-software"},Packages:[]string{"python3","numpy"},
 },server.Close
}

func TestWorkspaceBridgeForwardsOnlyScopedOperationsWithProtectedToken(t *testing.T){
 seen:=[]string{}
 policy,done:=testWorkspacePolicy(t,http.HandlerFunc(func(w http.ResponseWriter,r *http.Request){
  if r.Method!="POST"||r.URL.Path!="/mcp"||r.Header.Get("Authorization")!="Bearer "+strings.Repeat("k",48){
   t.Fatal("Workspace request not authenticated or sent to wrong endpoint")
  }
  var request struct{
   Method string `json:"method"`
   Params struct{Name string `json:"name"`;Arguments map[string]any `json:"arguments"`} `json:"params"`
  }
  if err:=json.NewDecoder(r.Body).Decode(&request);err!=nil||request.Method!="tools/call"||
    request.Params.Arguments["host"]!=nil{
   t.Fatal("invalid or overprivileged Workspace request",request,err)
  }
  seen=append(seen,request.Params.Name)
  w.Header().Set("Content-Type","application/json")
  _,_=w.Write([]byte(`{"jsonrpc":"2.0","id":1,"result":{"isError":false,"content":[]}}`))
 }))
 defer done()
 if err:=policy.validate();err!=nil{t.Fatal(err)}
 actions:=[]struct{operation string;fields map[string]any;downstream string}{
  {"infrastructure__workspace_stop",map[string]any{"host":"saturn","project_id":"research","workspace_id":"ws1"},"workspace_destroy"},
  {"infrastructure__workspace_build_environment",map[string]any{"host":"saturn","project_id":"research","workspace_id":"ws1","name":"python-dev"},"environment_build"},
  {"infrastructure__workspace_revoke_worker",map[string]any{"host":"saturn","node_id":"jupiter"},"worker_revoke"},
  {"infrastructure__workspace_register_software",map[string]any{
    "host":"saturn","software_id":"approved-software","image_id":"approved-image",
    "packages":[]any{"python3","numpy"}},"pool_register_software"},
 }
 for _,tc:=range actions{
  if err:=policy.Call(context.Background(),tc.operation,tc.fields);err!=nil{
   t.Fatal("authorized Workspace mutation was rejected",tc.operation,err)
  }
  if seen[len(seen)-1]!=tc.downstream{t.Fatal("wrong downstream tool used",seen)}
 }
}

func TestWorkspaceBridgeDeniesUnregisteredResourcesBeforeSendingCredentials(t *testing.T){
 sent:=0
 policy,done:=testWorkspacePolicy(t,http.HandlerFunc(func(w http.ResponseWriter,r *http.Request){sent++;w.WriteHeader(200)}))
 defer done()
 if err:=policy.validate();err!=nil{t.Fatal(err)}
 invalid:=[]struct{operation string;fields map[string]any}{
  {"infrastructure__workspace_stop",map[string]any{"host":"saturn","project_id":"other","workspace_id":"ws1"}},
  {"infrastructure__workspace_stop",map[string]any{"host":"saturn","project_id":"research","workspace_id":"ws1","command":"sh"}},
  {"infrastructure__workspace_register_software",map[string]any{"host":"saturn","image_id":"approved-image","software_id":"approved-software","packages":[]any{"python3; rm -rf /"}}},
  {"infrastructure__workspace_register_software",map[string]any{"host":"saturn","image_id":"unknown-image","software_id":"approved-software","packages":[]any{"python3"}}},
  {"infrastructure__workspace_revoke_worker",map[string]any{"host":"saturn","node_id":"unknown-worker"}},
  {"infrastructure__workspace_shell",map[string]any{"host":"saturn","command":"id"}},
 }
 for _,tc:=range invalid{
  if err:=policy.Call(context.Background(),tc.operation,tc.fields);err==nil{
   t.Fatal("unregistered Workspace mutation was permitted",tc)
  }
 }
 if sent!=0{t.Fatal("invalid operations reached credential-owning HTTP client")}
}

func TestWorkspaceBridgeFailsClosedOnUntrustedEndpointsAndMalformedReplies(t *testing.T){
 policy,done:=testWorkspacePolicy(t,http.HandlerFunc(func(w http.ResponseWriter,r *http.Request){
  w.Header().Set("Content-Type","application/json")
  _,_=w.Write([]byte(`{"jsonrpc":"2.0","id":1,"result":{}}`))
 }))
 defer done()
 if err:=policy.Call(context.Background(),"infrastructure__workspace_stop",
  map[string]any{"host":"saturn","project_id":"research","workspace_id":"ws1"});err==nil{
  t.Fatal("invalid MCP tool result was accepted")
 }
 for _,url:=range []string{"http://example.com/mcp","https://user:pass@example.com/mcp",
  "https://example.com/not-mcp","https://example.com/mcp?target=other"}{
  clone:=*policy;clone.Endpoint=url
  if err:=clone.validate();err==nil{t.Fatal("unsafe Workspace endpoint was permitted",url)}
 }
}

func TestWorkspaceExecutorReauthorizesEveryMutation(t *testing.T){
 calls:=0
 policy,done:=testWorkspacePolicy(t,http.HandlerFunc(func(w http.ResponseWriter,r *http.Request){
  calls++;w.Header().Set("Content-Type","application/json")
  _,_=w.Write([]byte(`{"jsonrpc":"2.0","id":1,"result":{"isError":false,"content":[]}}`))
 }))
 defer done()
 auth:=&fakeAuth{decision:security.Authorization{Reason:"owner confirmation required"}}
 executor,err:=New(Config{Audit:testExecutorAudit(t),
  Host:"saturn",Systemctl:"/usr/bin/systemctl",Auth:auth,Releases:&fakeRelease{},
  Run:func(context.Context,string,...string)error{t.Fatal("Workspace op fell through to shell");return nil},
  Workspace:policy,
 })
 if err!=nil{t.Fatal(err)}
 args:=json.RawMessage(`{"host":"saturn","project_id":"research","workspace_id":"ws1"}`)
 result,err:=executor.Execute(context.Background(),"infrastructure__workspace_stop","saturn",args)
 if err!=nil||result.Allowed||calls!=0||auth.calls!=1{
  t.Fatal("unconfirmed operation reached Workspace",result,err)
 }
 bad:=json.RawMessage(`{"host":"saturn","project_id":"other","workspace_id":"ws1"}`)
 if result,err:=executor.Execute(context.Background(),"infrastructure__workspace_stop","saturn",bad);err!=nil||result.Allowed||auth.calls!=1{
  t.Fatal("resource substitution reached authorizer",result,err)
 }
 auth.decision=security.Authorization{Allowed:true}
 result,err=executor.Execute(context.Background(),"infrastructure__workspace_stop","saturn",args)
 if err!=nil||!result.Allowed||calls!=1||auth.calls!=2{t.Fatal("approved Workspace operation not delegated",result,err)}
}


func TestWorkspaceBridgeAcceptsToolResultWithoutExplicitIsError(t *testing.T) {
    policy, done := testWorkspacePolicy(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        w.Header().Set("Content-Type", "application/json")
        _, _ = w.Write([]byte(`{"jsonrpc":"2.0","id":1,"result":{"content":[]}}`))
    }))
    defer done()
    args := map[string]any{"host":"saturn","project_id":"research","workspace_id":"ws1"}
    if err := policy.Call(context.Background(), "infrastructure__workspace_stop", args); err != nil {
        t.Fatalf("valid MCP tool result was rejected: %v", err)
    }
}
