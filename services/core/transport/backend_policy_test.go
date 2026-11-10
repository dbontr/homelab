package transport

import (
 "context"
 "encoding/json"
 "net/http"
 "net/http/httptest"
 "path/filepath"
 "strings"
 "testing"

 "github.com/dbontr/homelab-new/system/store"
)

func TestUnadvertisedBackendToolIsNeverForwarded(t *testing.T){
 calls:=0
 backend:=httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter,r *http.Request){
  var req rpcRequest
  if err:=json.NewDecoder(r.Body).Decode(&req);err!=nil{t.Fatal(err)}
  w.Header().Set("Content-Type","application/json")
  if req.Method=="tools/list"{
   _,_=w.Write([]byte(`{"jsonrpc":"2.0","id":1,"result":{"tools":[{"name":"list_projects","description":"List","inputSchema":{"type":"object"}},{"name":"worker_revoke","description":"Revoke worker","inputSchema":{"type":"object"}}]}}`))
   return
  }
  calls++
  _,_=w.Write([]byte(`{"jsonrpc":"2.0","id":1,"result":{"content":[]}}`))
 }))
 defer backend.Close()
 db,err:=store.Open(filepath.Join(t.TempDir(),"state.json"),"UTC");if err!=nil{t.Fatal(err)}
 s,err:=New(db,strings.Repeat("t",40),[]Backend{{Name:"workspace",URL:backend.URL}});if err!=nil{t.Fatal(err)}
 w:=request(s.Handler(),"POST","/v1/tools/workspace__worker_revoke/call",`{"arguments":{"node_id":"worker"}}`,strings.Repeat("t",40))
 if calls!=0||!strings.Contains(w.Body.String(),"executor-side authorization"){
  t.Fatalf("privileged tool must not be forwarded: %s",w.Body.String())
 }
 w=request(s.Handler(),"POST","/v1/tools/workspace__unadvertised/call",`{"arguments":{}}`,strings.Repeat("t",40))
 if calls!=0||!strings.Contains(w.Body.String(),"not advertised"){
  t.Fatalf("unadvertised tool forwarded: %s",w.Body.String())
 }
}

func TestFederatedRejectsOversizedAndMismatchedResponses(t *testing.T){
 backend:=httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter,r *http.Request){
  w.Header().Set("Content-Type","application/json")
  var req rpcRequest
  if err:=json.NewDecoder(r.Body).Decode(&req);err!=nil{t.Fatal(err)}
  if req.Method=="large"{
    _,_=w.Write([]byte(strings.Repeat("A",maxFederatedResponse+1)))
    return
  }
  _,_=w.Write([]byte(`{"jsonrpc":"2.0","id":"wrong","result":{"tools":[]}}`))
 }))
 defer backend.Close()
 db,err:=store.Open(filepath.Join(t.TempDir(),"state.json"),"UTC");if err!=nil{t.Fatal(err)}
 s,err:=New(db,strings.Repeat("t",40),[]Backend{{Name:"learning",URL:backend.URL}});if err!=nil{t.Fatal(err)}
 b:=s.backend[0]
 var out any
 if err:=s.forward(context.Background(),b,rpcRequest{JSONRPC:"2.0",ID:json.RawMessage("1"),Method:"large"},&out);err==nil{t.Fatal("oversized reply accepted")}
 if err:=s.forward(context.Background(),b,rpcRequest{JSONRPC:"2.0",ID:json.RawMessage("1"),Method:"tools/list"},&out);err==nil{t.Fatal("mismatched response ID accepted")}
}
