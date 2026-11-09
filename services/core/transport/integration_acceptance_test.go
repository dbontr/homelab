package transport

import (
 "encoding/json"
 "net/http"
 "net/http/httptest"
 "path/filepath"
 "strings"
 "testing"

 "github.com/dbontr/homelab-new/system/store"
)

func TestTeachingAndHealthRemainIsolatedBehindOneGateway(t *testing.T){
 service:=func(name,secret string)*httptest.Server{
  return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter,r *http.Request){
   if r.Header.Get("Authorization")!="Bearer "+secret{http.Error(w,"unauthorized",401);return}
   var req rpcRequest
   if err:=json.NewDecoder(r.Body).Decode(&req);err!=nil{t.Error(err);return}
   w.Header().Set("Content-Type","application/json")
   if req.Method=="tools/list"{
     _,_=w.Write([]byte(`{"jsonrpc":"2.0","id":1,"result":{"tools":[{"name":"status","description":"Backend status","inputSchema":{"type":"object"}}]}}`))
     return
   }
   if req.Method!="tools/call"{http.Error(w,"bad method",400);return}
   reply,_:=json.Marshal(map[string]any{"jsonrpc":"2.0","id":1,"result":map[string]any{"content":[]any{map[string]string{"type":"text","text":name}},"isError":false}})
   _,_=w.Write(reply)
  }))
 }
 teaching:=service("learning","learning-secret");defer teaching.Close()
 health:=service("health","health-secret");defer health.Close()
 db,err:=store.Open(filepath.Join(t.TempDir(),"state.json"),"UTC");if err!=nil{t.Fatal(err)}
 server,err:=New(db,strings.Repeat("o",40),[]Backend{{Name:"learning",URL:teaching.URL,Token:"learning-secret"},{Name:"health",URL:health.URL,Token:"health-secret"}})
 if err!=nil{t.Fatal(err)}
 token:=strings.Repeat("o",40)
 list:=request(server.Handler(),"GET","/v1/tools","",token)
 for _,name:=range []string{"learning__status","health__status"}{
   if !strings.Contains(list.Body.String(),name){t.Fatalf("missing integrated capability: %s",name)}
   call:=request(server.Handler(),"POST","/v1/tools/"+name+"/call",`{"arguments":{}}`,token)
   expected:=strings.Split(name,"__")[0]
   if call.Code!=200||!strings.Contains(call.Body.String(),expected)||strings.Contains(call.Body.String(),"secret"){
      t.Fatalf("invalid %s result: %s",name,call.Body.String())
   }
 }
 health.Close()
 failed:=request(server.Handler(),"POST","/v1/tools/health__status/call",`{"arguments":{}}`,token)
 if failed.Code!=200||!strings.Contains(failed.Body.String(),`"isError":true`){
   t.Fatalf("backend failure not reported: %s",failed.Body.String())
 }
}
