package transport

import (
 "encoding/json"
 "net/http"
 "net/http/httptest"
 "path/filepath"
 "strings"
 "sync/atomic"
 "testing"

 "github.com/dbontr/homelab-new/system/profiles"
 "github.com/dbontr/homelab-new/system/store"
)

func TestNativeAndMCPShareIntegrationDispatch(t *testing.T) {
 var calls atomic.Int32
 backend:=httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter,r *http.Request){
   var rpc rpcRequest
   if err:=json.NewDecoder(r.Body).Decode(&rpc);err!=nil{t.Error(err);return}
   w.Header().Set("Content-Type","application/json")
   switch rpc.Method {
   case "tools/list":
      _,_=w.Write([]byte(`{"jsonrpc":"2.0","id":1,"result":{"tools":[{"name":"lesson_list","description":"List lessons","inputSchema":{"type":"object"}}]}}`))
   case "tools/call":
      calls.Add(1)
      _,_=w.Write([]byte(`{"jsonrpc":"2.0","id":`+string(rpc.ID)+`,"result":{"content":[{"type":"text","text":"mock-learning"}],"isError":false}}`))
   default: http.Error(w,"unsupported",400)
   }
 }))
 defer backend.Close()
 db,err:=store.Open(filepath.Join(t.TempDir(),"state.json"),"UTC");if err!=nil{t.Fatal(err)}
 server,err:=New(db,strings.Repeat("s",40),[]Backend{{Name:"learning",URL:backend.URL}})
 if err!=nil{t.Fatal(err)}
 h:=server.Handler()
 token:=strings.Repeat("s",40)
 if w:=request(h,"GET","/v1/tools","",token);w.Code!=200||!strings.Contains(w.Body.String(),"learning__lesson_list"){t.Fatalf("native discovery: %s",w.Body.String())}
 if w:=request(h,"POST","/mcp",`{"jsonrpc":"2.0","id":1,"method":"tools/list"}`,token);!strings.Contains(w.Body.String(),"learning__lesson_list"){t.Fatalf("MCP discovery: %s",w.Body.String())}
 a:=request(h,"POST","/v1/tools/learning__lesson_list/call",`{"arguments":{}}`,token)
 b:=request(h,"POST","/mcp",`{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"learning__lesson_list","arguments":{}}}`,token)
 if a.Code!=200||b.Code!=200||!strings.Contains(a.Body.String(),"mock-learning")||!strings.Contains(b.Body.String(),"mock-learning")||calls.Load()!=2 {
    t.Fatalf("tool dispatch differs: native=%s MCP=%s calls=%d",a.Body.String(),b.Body.String(),calls.Load())
 }
 if w:=request(h,"POST","/v1/tools/learning__lesson_list/call",`{"arguments":{}}`,"");w.Code!=401{t.Fatal("unauthorized native call was accepted")}
 if w:=request(h,"POST","/v1/tools/learning__lesson_list/call",`{"arguments":{},"password":"secret"}`,token);w.Code!=400{t.Fatal("unknown field accepted")}
}

func TestBuildProfilesUseOneNativeAndMCPStore(t *testing.T) {
 path:=filepath.Join(t.TempDir(),"profiles.json")
 registry,err:=profiles.Open(path);if err!=nil{t.Fatal(err)}
 db,err:=store.Open(filepath.Join(t.TempDir(),"state.json"),"UTC");if err!=nil{t.Fatal(err)}
 server,err:=New(db,strings.Repeat("p",40),nil);if err!=nil{t.Fatal(err)}
 server.SetProfiles(registry)
 token:=strings.Repeat("p",40)
 h:=server.Handler()
 initial:=`{"spec":{"id":"pynq-builder","title":"AUP-ZU3 builder","parameters":{"RAM_GB":"8"},"steps":[{"id":"build","command":"echo build"}],"requires":{"architecture":"amd64","cpu":8}},"expected_version":0}`
 w:=request(h,"PUT","/v1/compute/profiles/pynq-builder",initial,token)
 if w.Code!=200||!strings.Contains(w.Body.String(),"pynq-builder"){t.Fatalf("native save failed: %s",w.Body.String())}
 updated:=`{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"compute_profile_save","arguments":{"spec":{"id":"pynq-builder","title":"AUP-ZU3 builder","parameters":{"RAM_GB":"4"},"steps":[{"id":"build","command":"echo build"}],"requires":{"architecture":"amd64","cpu":8}},"expected_version":1}}}`
 w=request(h,"POST","/mcp",updated,token)
 if w.Code!=200||!strings.Contains(w.Body.String(),"pynq-builder"){t.Fatalf("MCP save failed: %s",w.Body.String())}
 old:=request(h,"GET","/v1/compute/profiles/pynq-builder?version=1","",token)
 latest:=request(h,"GET","/v1/compute/profiles/pynq-builder","",token)
 if !strings.Contains(old.Body.String(),`"RAM_GB":"8"`)||!strings.Contains(latest.Body.String(),`"RAM_GB":"4"`){
   t.Fatalf("revisions changed: old=%s latest=%s",old.Body.String(),latest.Body.String())
 }
 if w:=request(h,"PUT","/v1/compute/profiles/other",initial,token);w.Code!=400{t.Fatal("mismatched profile ID accepted")}
 if w:=request(h,"PUT","/v1/compute/profiles/pynq-builder",initial,token);w.Code!=400{t.Fatal("stale profile revision accepted")}
}
