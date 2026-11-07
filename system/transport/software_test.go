package transport

import (
 "encoding/json"
 "net/http"
 "net/http/httptest"
 "path/filepath"
 "strings"
 "testing"

 "github.com/dbontr/homelab-new/system/devices"
 "github.com/dbontr/homelab-new/system/store"
)

func TestSoftwareLibraryUsesWorkspacePoolWithoutCopyingState(t *testing.T){
 mock:=httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter,r *http.Request){
   var call rpcRequest
   if err:=json.NewDecoder(r.Body).Decode(&call);err!=nil{t.Error(err);return}
   w.Header().Set("Content-Type","application/json")
   if call.Method=="tools/list"{
     _,_=w.Write([]byte(`{"jsonrpc":"2.0","id":1,"result":{"tools":[]}}`));return
   }
   var input struct {Name string `json:"name"`}
   if err:=json.Unmarshal(call.Params,&input);err!=nil{t.Error(err);return}
   item:=""
   switch input.Name {
   case "pool_list_images":item=`[{"id":"image-1"}]`
   case "pool_list_software":item=`[{"id":"software-1"}]`
   case "pool_list_programs":item=`[{"id":"program-1"}]`
   default:http.Error(w,"unknown pool",400);return
   }
   encoded,_:=json.Marshal(map[string]any{"jsonrpc":"2.0","id":1,"result":map[string]any{"content":[]any{map[string]any{"type":"text","text":item}},"isError":false}})
   _,_=w.Write(encoded)
 }))
 defer mock.Close()
 db,err:=store.Open(filepath.Join(t.TempDir(),"state.json"),"UTC");if err!=nil{t.Fatal(err)}
 server,err:=New(db,strings.Repeat("x",40),[]Backend{{Name:"workspace",URL:mock.URL}})
 if err!=nil{t.Fatal(err)}
 token:=strings.Repeat("x",40)
 native:=request(server.Handler(),"GET","/v1/software","",token)
 mcp:=request(server.Handler(),"POST","/mcp",`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"software_catalog","arguments":{}}}`,token)
 if native.Code!=200||mcp.Code!=200 {t.Fatalf("catalog unavailable: native=%s mcp=%s",native.Body.String(),mcp.Body.String())}
 for _,id:=range []string{"image-1","software-1","program-1"}{
   if !strings.Contains(native.Body.String(),id)||!strings.Contains(mcp.Body.String(),id){t.Fatalf("missing canonical pool entry %s",id)}
 }
 if unauthorized:=request(server.Handler(),"GET","/v1/software","","");unauthorized.Code!=401{t.Fatal("catalog bypassed authorization")}
}

func TestNodeInventoryIsConsistentAcrossInterfaces(t *testing.T){
 db,err:=store.Open(filepath.Join(t.TempDir(),"state.json"),"UTC");if err!=nil{t.Fatal(err)}
 server,err:=New(db,strings.Repeat("n",40),nil);if err!=nil{t.Fatal(err)}
 nodes,err:=devices.NewNodes([]devices.Node{{ID:"jupiter",Mode:devices.ModeEnrolled,Role:"compute",System:"x86_64-linux"},{ID:"external",Mode:devices.ModeSSH}})
 if err!=nil{t.Fatal(err)}
 server.SetNodeCatalog(nodes)
 token:=strings.Repeat("n",40)
 native:=request(server.Handler(),"GET","/v1/nodes","",token)
 mcp:=request(server.Handler(),"POST","/mcp",`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"node_inventory","arguments":{}}}`,token)
 for _,name:=range []string{"jupiter","external"}{
   if !strings.Contains(native.Body.String(),name)||!strings.Contains(mcp.Body.String(),name){t.Fatalf("node missing from one interface: %s",name)}
 }
}
