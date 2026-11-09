package transport

import (
 "path/filepath"
 "strings"
 "testing"

 "github.com/dbontr/homelab-new/system/store"
)

func TestInterfaceTokensCannotCrossDomainRuntimeRoutes(t *testing.T){
 db,err:=store.Open(filepath.Join(t.TempDir(),"state.json"),"UTC")
 if err!=nil{t.Fatal(err)}
 root:=strings.Repeat("r",40)
 mcp:=strings.Repeat("m",40)
 api:=strings.Repeat("a",40)
 server,err:=New(db,root,nil)
 if err!=nil{t.Fatal(err)}
 if err:=server.SetInterfaceTokens(mcp,api,nil);err!=nil{t.Fatal(err)}
 h:=server.Handler()
 if w:=request(h,"GET","/v1/tasks","",api);w.Code!=200{
  t.Fatalf("native API token could not read a resource: %d",w.Code)
 }
 if w:=request(h,"POST","/v1/tasks",`{"title":"No write"}`,api);w.Code!=401{
  t.Fatalf("read-only native token could write: %d",w.Code)
 }
 if w:=request(h,"GET","/v1/tasks","",mcp);w.Code!=401{
  t.Fatalf("MCP token acquired native API authority: %d",w.Code)
 }
 if w:=request(h,"POST","/mcp",`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{}}`,api);w.Code!=401{
  t.Fatalf("native API token acquired MCP authority: %d",w.Code)
 }
 if w:=request(h,"GET","/v1/tools","",api);w.Code!=401{
  t.Fatalf("native API exposed privileged tool discovery: %d",w.Code)
 }
 if w:=request(h,"POST","/mcp",`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{}}`,mcp);w.Code!=200{
  t.Fatalf("approved MCP transport denied: %d",w.Code)
 }
 if w:=request(h,"POST","/v1/tasks",`{"title":"Root write"}`,root);w.Code!=201{
  t.Fatalf("existing private service token lost behavior: %d",w.Code)
 }
}

func TestNativeInterfaceWritePrefixIsEnforcedByService(t *testing.T){
 db,err:=store.Open(filepath.Join(t.TempDir(),"state.json"),"UTC")
 if err!=nil{t.Fatal(err)}
 server,err:=New(db,strings.Repeat("r",40),nil)
 if err!=nil{t.Fatal(err)}
 api:=strings.Repeat("a",40)
 if err:=server.SetInterfaceTokens("",api,[]string{"/v1/tasks"});err!=nil{t.Fatal(err)}
 h:=server.Handler()
 if w:=request(h,"POST","/v1/tasks",`{"title":"Allowed task"}`,api);w.Code!=201{
  t.Fatalf("allowlisted API task was denied: %d %s",w.Code,w.Body.String())
 }
 if w:=request(h,"POST","/v1/goals",`{"title":"Not allowed"}`,api);w.Code!=401{
  t.Fatalf("unapproved resource mutation reached handler: %d",w.Code)
 }
 if w:=request(h,"POST","/v1/tools/task_add/call",`{}`,api);w.Code!=401{
  t.Fatalf("API interface invoked generic tool endpoint: %d",w.Code)
 }
}

func TestInterfaceCredentialsRejectAmbiguousPolicies(t *testing.T){
 db,err:=store.Open(filepath.Join(t.TempDir(),"state.json"),"UTC")
 if err!=nil{t.Fatal(err)}
 root:=strings.Repeat("r",40)
 s,err:=New(db,root,nil)
 if err!=nil{t.Fatal(err)}
 for _,tc:=range []struct{mcp,api string;paths []string}{
  {root,"",nil},
  {"",root,nil},
  {strings.Repeat("a",40),strings.Repeat("a",40),nil},
  {"","",[]string{"/v1/tasks"}},
  {"",strings.Repeat("a",40),[]string{"/v1/tools"}},
  {"",strings.Repeat("a",40),[]string{"/v1/tasks/../goals"}},
 }{
  if err:=s.SetInterfaceTokens(tc.mcp,tc.api,tc.paths);err==nil{
   t.Fatalf("invalid interface policy admitted: %+v",tc)
  }
 }
}
