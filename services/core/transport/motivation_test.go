package transport

import (
 "net/http"
 "net/http/httptest"
 "path/filepath"
 "strings"
 "testing"

 "github.com/dbontr/homelab-new/system/internal/motivationhttp"
 "github.com/dbontr/homelab-new/system/store"
)

func TestMotivationHasOnePrivateBackendAndOwnerAuthorization(t *testing.T){
 backendToken:=strings.Repeat("m",48)
 backend:=httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter,r *http.Request){
  if r.Header.Get("Authorization")!="Bearer "+backendToken{
   t.Error("motivation backend did not receive its own credential")
   w.WriteHeader(http.StatusUnauthorized);return
  }
  w.Header().Set("Content-Type","application/json")
  switch r.Method+" "+r.URL.Path{
  case "GET /v1/motivation/state":
   _,_=w.Write([]byte(`{"profile":{"revision":0,"enabled":false},"interventions":[],"next_id":1}`))
  case "POST /v1/motivation/interventions":
   _,_=w.Write([]byte(`{"id":1,"strategy":"outcome","message":"Chosen outcome"}`))
  case "POST /v1/motivation/pause":
   _,_=w.Write([]byte(`{"profile":{"revision":1,"enabled":false}}`))
  default:
   t.Errorf("unexpected backend route: %s %s",r.Method,r.URL.Path)
   w.WriteHeader(http.StatusNotFound)
  }
 }))
 defer backend.Close()
 client,err:=motivationhttp.New(backend.URL,backendToken)
 if err!=nil{t.Fatal(err)}
 db,err:=store.Open(filepath.Join(t.TempDir(),"state.json"),"UTC")
 if err!=nil{t.Fatal(err)}
 server,err:=New(db,strings.Repeat("a",40),nil)
 if err!=nil{t.Fatal(err)}
 server.SetMotivation(client)
 h:=server.Handler()
 if w:=request(h,"GET","/v1/motivation/state","","");w.Code!=http.StatusUnauthorized{
  t.Fatalf("unauthenticated read status %d",w.Code)
 }
 if w:=request(h,"POST","/v1/motivation/interventions","{}", "");w.Code!=http.StatusUnauthorized{
  t.Fatalf("unauthenticated intervention status %d",w.Code)
 }
 authorized:=strings.Repeat("a",40)
 if w:=request(h,"GET","/v1/motivation/state","",authorized);w.Code!=http.StatusOK{
  t.Fatalf("owner read status %d: %s",w.Code,w.Body.String())
 }
 if w:=request(h,"POST","/v1/motivation/interventions","{}",authorized);w.Code!=http.StatusOK{
  t.Fatalf("owner intervention status %d: %s",w.Code,w.Body.String())
 }
 tools:=request(h,"POST","/mcp",`{"jsonrpc":"2.0","id":1,"method":"tools/list"}`,authorized)
 if !strings.Contains(tools.Body.String(),"motivation_suggest"){
  t.Fatalf("motivation tool not exposed: %s",tools.Body.String())
 }
 if strings.Contains(tools.Body.String(),"motivation_profile_update") ||
   strings.Contains(tools.Body.String(),"motivation_feedback"){
  t.Fatal("MCP must not alter aspirational identity or forge self-reported feedback")
 }
 pause:=request(h,"POST","/mcp",`{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"motivation_pause","arguments":{}}}`,authorized)
 if !strings.Contains(pause.Body.String(),`\"enabled\":false`){
  t.Fatalf("pause tool failed: %s",pause.Body.String())
 }
}
