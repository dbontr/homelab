package transport

import (
 "encoding/json"
 "net/http"
 "net/http/httptest"
 "path/filepath"
 "strings"
 "sync/atomic"
 "testing"

 "github.com/dbontr/homelab-new/system/store"
)

// Backend discovery is not authority to execute a privileged operation.
func TestPrivilegedBackendToolCannotUseGatewayCredentials(t *testing.T){
 var calls atomic.Int32
 backend:=httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter,r *http.Request){
  var input rpcRequest
  if err:=json.NewDecoder(r.Body).Decode(&input);err!=nil{t.Error(err);return}
  w.Header().Set("Content-Type","application/json")
  var outcome any
  switch input.Method {
  case "tools/list":
   outcome=map[string]any{"tools":[]any{
    map[string]any{"name":"configure","description":"Configure","inputSchema":map[string]any{"type":"object"}},
    map[string]any{"name":"worker_revoke","description":"Revoke","inputSchema":map[string]any{"type":"object"}},
    map[string]any{"name":"delete_all","description":"Delete","inputSchema":map[string]any{"type":"object"}},
    map[string]any{"name":"list_projects","description":"List","inputSchema":map[string]any{"type":"object"}},
   }}
  case "tools/call":
   calls.Add(1)
   outcome=map[string]any{"content":[]any{map[string]any{"type":"text","text":"forwarded"}}}
  default:
   t.Errorf("unexpected method %q",input.Method)
   w.WriteHeader(http.StatusBadRequest)
   return
  }
  _=json.NewEncoder(w).Encode(map[string]any{"jsonrpc":"2.0","id":input.ID,"result":outcome})
 }))
 defer backend.Close()
 db,err:=store.Open(filepath.Join(t.TempDir(),"state.json"),"UTC")
 if err!=nil{t.Fatal(err)}
 auth:=strings.Repeat("a",40)
 server,err:=New(db,auth,[]Backend{
  {Name:"cluster",URL:backend.URL},
  {Name:"workspace",URL:backend.URL},
  {Name:"health",URL:backend.URL},
 })
 if err!=nil{t.Fatal(err)}
 handler:=server.Handler()
 for _,name:=range []string{"cluster__configure","compute__worker_revoke","health__delete_all"}{
  payload:="{" + `"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"`+name+`","arguments":{"host":"saturn"}}}`
  for _,entry:=range []struct{path,body string}{
   {"/mcp",payload},
   {"/v1/tools/"+name+"/call",`{"arguments":{"host":"saturn"}}`},
  }{
   response:=request(handler,http.MethodPost,entry.path,entry.body,auth)
   if response.Code!=http.StatusOK||!strings.Contains(response.Body.String(),"executor-side authorization"){
    t.Fatalf("%s %s: %d %s",name,entry.path,response.Code,response.Body.String())
   }
  }
 }
 if calls.Load()!=0{t.Fatal("privileged tool reached a backend")}
 catalog:=request(handler,http.MethodGet,"/v1/tools","",auth)
 if strings.Contains(catalog.Body.String(),`"compute__worker_revoke"`)||
    strings.Contains(catalog.Body.String(),`"cluster__configure"`)||
    !strings.Contains(catalog.Body.String(),`"compute__list_projects"`){
  t.Fatalf("unsafe tool catalog: %s",catalog.Body.String())
 }
 read:=request(handler,http.MethodPost,"/v1/tools/compute__list_projects/call",`{"arguments":{}}`,auth)
 if read.Code!=http.StatusOK||!strings.Contains(read.Body.String(),"forwarded")||calls.Load()!=1{
  t.Fatalf("routine backend read failed: %d %s",read.Code,read.Body.String())
 }
}

func TestMissingIndependentSecurityDeniesPrivilegedOperations(t *testing.T){
 server:=&Server{}
 for _,name:=range []string{"cluster__configure","workspace__workspace_create","health__delete_all"}{
  response,allowed:=server.checkPrivilege(name,json.RawMessage(`{"host":"saturn"}`))
  encoded,_:=json.Marshal(response)
  if allowed||!strings.Contains(string(encoded),"independent security authorization unavailable"){
   t.Errorf("%s accepted without independent authorization: %s",name,encoded)
  }
 }
 if result,allowed:=server.checkPrivilege("workspace__list_projects",json.RawMessage(`{}`));!allowed||result!=nil{
  t.Fatal("nonprivileged read became dependent on owner authorization")
 }
}
