package privileged

import (
 "context"
 "encoding/json"
 "errors"
 "net/http"
 "net/http/httptest"
 "testing"

 "github.com/dbontr/homelab-new/system/security"
)

func TestVaultWorkspaceRequiresIndependentAuthorization(t *testing.T){
 token:="executor-scoped-token-initial-credential"
 gets,calls:=0,0
 upstream:=httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter,r *http.Request){
  calls++
  if r.Header.Get("Authorization")!="Bearer "+token{t.Error("wrong scoped token")}
  w.Header().Set("Content-Type","application/json")
  _,_=w.Write([]byte(`{"jsonrpc":"2.0","id":1,"result":{"isError":false}}`))
 }))
 defer upstream.Close()
 policy:=&WorkspacePolicy{Endpoint:upstream.URL+"/mcp",Projects:[]string{"research"},Client:upstream.Client(),
  TokenSource:func()(string,error){gets++;return token,nil}}
 auth:=&fakeAuth{decision:security.Authorization{Reason:"owner confirmation required"}}
 executor,err:=New(Config{Host:"saturn",Systemctl:"/usr/bin/systemctl",Workspace:policy,
  Auth:auth,Releases:&fakeRelease{},Audit:testExecutorAudit(t),
  Run:func(context.Context,string,...string)error{t.Fatal("unexpected shell");return nil}})
 if err!=nil{t.Fatal(err)}
 args:=json.RawMessage(`{"host":"saturn","project_id":"research","workspace_id":"ws1"}`)
 denied,err:=executor.Execute(context.Background(),"infrastructure__workspace_stop","saturn",args)
 if err!=nil||denied.Allowed||gets!=0||calls!=0||auth.calls!=1{
  t.Fatal("unconfirmed mutation reached the credential vault",denied,err)
 }
 bad:=json.RawMessage(`{"host":"saturn","project_id":"wrong","workspace_id":"ws1"}`)
 denied,err=executor.Execute(context.Background(),"infrastructure__workspace_stop","saturn",bad)
 if err!=nil||denied.Allowed||gets!=0||auth.calls!=1{
  t.Fatal("resource substitution reached the authorizer or vault",denied,err)
 }
 auth.decision=security.Authorization{Allowed:true}
 ok,err:=executor.Execute(context.Background(),"infrastructure__workspace_stop","saturn",args)
 if err!=nil||!ok.Allowed||gets!=1||calls!=1{
  t.Fatal("approved operation was not delegated",ok,err)
 }
 token="executor-scoped-token-rotated-credential"
 ok,err=executor.Execute(context.Background(),"infrastructure__workspace_stop","saturn",args)
 if err!=nil||!ok.Allowed||gets!=2||calls!=2{
  t.Fatal("rotated vault credential was not used",ok,err)
 }
 policy.TokenSource=func()(string,error){gets++;return "",errors.New("revoked")}
 failed,err:=executor.Execute(context.Background(),"infrastructure__workspace_stop","saturn",args)
 if err==nil||failed.Allowed||calls!=2{
  t.Fatal("revoked credential reached Workspace",failed,err)
 }
}

func TestWorkspaceVaultEnforcesSingleAuthorityAndResourceScope(t *testing.T){
 policy:=&WorkspacePolicy{Endpoint:"https://workspace.example/mcp",
  Projects:[]string{"research"},TokenSource:func()(string,error){return "bad token",nil}}
 if err:=policy.validate();err!=nil{t.Fatal(err)}
 policy.TokenFile="/private/credential"
 if err:=policy.validate();err==nil{t.Fatal("mixed token authorities accepted")}
 policy.TokenFile=""
 policy.Projects=nil
 if err:=policy.validate();err==nil{t.Fatal("unscoped vault use accepted")}
 policy.Projects=[]string{"research"}
 if err:=policy.Call(context.Background(),"infrastructure__workspace_stop",
  map[string]any{"host":"saturn","project_id":"research","workspace_id":"ws1"});err==nil{
  t.Fatal("invalid vault credential reached the provider")
 }
 if _,_,err:=policy.operationArgs("infrastructure__workspace_shell",
  map[string]any{"host":"saturn","command":"id"});err==nil{
  t.Fatal("arbitrary command permitted through vault bridge")
 }
}
