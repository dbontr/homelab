package transport

import (
 "context"
 "encoding/json"
 "fmt"
 "net/http"
 "net/http/httptest"
 "testing"
 "time"

 "github.com/dbontr/homelab-new/system/security"
)

func TestFederationForwardsOwnerIdentityOnlyToSelectedWorkspace(t *testing.T){
 assertion:="signed-owner-assertion"
 received:=[]string{}
 upstream:=httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter,r *http.Request){
  received=append(received,r.Header.Get("Cf-Access-Jwt-Assertion"))
  w.Header().Set("Content-Type","application/json")
  _,_=fmt.Fprint(w,`{"jsonrpc":"2.0","id":17,"result":{"content":[{"type":"text","text":"ok"}]}}`)
 }))
 defer upstream.Close()
 server:=&Server{client:&http.Client{Timeout:5*time.Second}}
 ctx:=security.WithAccessAssertion(context.Background(),assertion)
 request:=rpcRequest{
  JSONRPC:"2.0",ID:json.RawMessage("17"),Method:"tools/call",
  Params:json.RawMessage(`{"name":"project_info","arguments":{}}`),
 }
 for _,backend:=range []Backend{
  {Name:"workspace",URL:upstream.URL,ForwardOwnerAssertion:true},
  {Name:"workspace",URL:upstream.URL},
  {Name:"health",URL:upstream.URL,ForwardOwnerAssertion:true},
 }{
  var result any
  if err:=server.forward(ctx,backend,request,&result);err!=nil{t.Fatal(err)}
 }
 if len(received)!=3||received[0]!=assertion||received[1]!=""||received[2]!=""{
  t.Fatal("owner token was forwarded outside the explicitly selected Workspace backend")
 }
 var result any
 if err:=server.forward(context.Background(),
  Backend{Name:"workspace",URL:upstream.URL,ForwardOwnerAssertion:true},request,&result);err!=nil{
  t.Fatal(err)
 }
 if len(received)!=4||received[3]!=""{
  t.Fatal("missing owner assertion was synthesized or cached")
 }
}
