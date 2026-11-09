package transport

import (
 "encoding/json"
 "net/http"
 "net/http/httptest"
 "path/filepath"
 "strings"
 "testing"

 "github.com/dbontr/homelab-new/system/agentflow"
 "github.com/dbontr/homelab-new/system/store"
)

type engineeringFixture struct {
 run agentflow.RunSummary
}
func(f *engineeringFixture) Submit(p agentflow.Program)(agentflow.RunSummary,error) {
 f.run=agentflow.RunSummary{ID:"run-test",Project:p.Project,Revision:1,Status:agentflow.Pending}
 return f.run,nil
}
func(f *engineeringFixture) Get(id string)(agentflow.RunSummary,error) {
 if id!="run-test"{return agentflow.RunSummary{},http.ErrMissingFile}
 return f.run,nil
}
func(f *engineeringFixture) List()([]agentflow.RunSummary,error) {
 if f.run.ID==""{return []agentflow.RunSummary{},nil}
 return []agentflow.RunSummary{f.run},nil
}
func(f *engineeringFixture) Cancel(id string,revision uint64)error{
 if id!="run-test"||revision!=f.run.Revision{return http.ErrMissingFile}
 f.run.Revision++
 f.run.Cancelled=true
 f.run.Status=agentflow.Cancelled
 return nil
}

func TestEngineeringLifecycleUsesPlatformAuthorization(t *testing.T){
 db,err:=store.Open(filepath.Join(t.TempDir(),"state.json"),"UTC")
 if err!=nil{t.Fatal(err)}
 s,err:=New(db,strings.Repeat("a",40),nil)
 if err!=nil{t.Fatal(err)}
 f:=&engineeringFixture{}
 s.SetEngineering(f)
 h:=s.Handler()
 token:=strings.Repeat("a",40)
 if response:=request(h,"GET","/v1/engineering/runs","","");response.Code!=http.StatusUnauthorized{
  t.Fatalf("engineering runs escaped owner authentication: %d",response.Code)
 }
 if response:=request(h,"POST","/v1/engineering/runs",`{"project":"research"}`,"");response.Code!=http.StatusUnauthorized{
  t.Fatalf("engineering submission escaped owner authentication: %d",response.Code)
 }
 created:=request(h,"POST","/v1/engineering/runs",`{"project":"research"}`,token)
 if created.Code!=http.StatusCreated{t.Fatalf("submission failed: %d %s",created.Code,created.Body.String())}
 listed:=request(h,"GET","/v1/engineering/runs","",token)
 if listed.Code!=http.StatusOK||!strings.Contains(listed.Body.String(),"run-test"){t.Fatalf("unexpected engineering list: %s",listed.Body.String())}
 detail:=request(h,"GET","/v1/engineering/runs/run-test","",token)
 if detail.Code!=http.StatusOK{t.Fatalf("engine status read failed: %s",detail.Body.String())}
 rejected:=request(h,"POST","/v1/engineering/runs/run-test/cancel",`{"revision":0}`,token)
 if rejected.Code!=http.StatusConflict{t.Fatalf("stale cancellation accepted: %d",rejected.Code)}
 accepted:=request(h,"POST","/v1/engineering/runs/run-test/cancel",`{"revision":1}`,token)
 if accepted.Code!=http.StatusOK||!strings.Contains(accepted.Body.String(),"cancelled"){t.Fatalf("owner cancellation failed: %s",accepted.Body.String())}
 tools:=request(h,"POST","/mcp",`{"jsonrpc":"2.0","id":1,"method":"tools/list"}`,token)
 if tools.Code!=http.StatusOK||!strings.Contains(tools.Body.String(),"engineering_run_submit"){
  t.Fatalf("engineering operation missing from MCP discovery: %s",tools.Body.String())
 }
 invoked:=request(h,"POST","/mcp",`{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"engineering_run_get","arguments":{"id":"run-test"}}}`,token)
 if invoked.Code!=http.StatusOK||!strings.Contains(invoked.Body.String(),"run-test"){
  t.Fatalf("engineering MCP call failed: %s",invoked.Body.String())
 }
 var record map[string]any
 if err:=json.Unmarshal(listed.Body.Bytes(),&record);err!=nil{t.Fatal(err)}
}

func TestComputeToolsUseUnifiedNamespace(t *testing.T){
 downstream:=httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter,r *http.Request){
  var in struct{ID json.RawMessage `json:"id"`;Method string `json:"method"`}
  if err:=json.NewDecoder(r.Body).Decode(&in);err!=nil{t.Fatal(err)}
  w.Header().Set("Content-Type","application/json")
  if in.Method=="tools/list"{
   _,_=w.Write([]byte(`{"jsonrpc":"2.0","id":1,"result":{"tools":[{"name":"workspace_list","description":"List isolated compute workspaces","inputSchema":{"type":"object"}}]}}`))
  }else{
   _,_=w.Write([]byte(`{"jsonrpc":"2.0","id":1,"result":{"content":[{"type":"text","text":"[]"}]}}`))
  }
 }))
 defer downstream.Close()
 h:=makeTestServer(t,[]Backend{{Name:"workspace",URL:downstream.URL}})
 token:=strings.Repeat("a",40)
 response:=request(h,"POST","/mcp",`{"jsonrpc":"2.0","id":1,"method":"tools/list"}`,token)
 if response.Code!=200||!strings.Contains(response.Body.String(),"compute__workspace_list"){
  t.Fatalf("canonical tool missing: %s",response.Body.String())
 }
 if strings.Contains(response.Body.String(),`"name":"workspace__workspace_list"`){
  t.Fatal("historical compute product name leaked into tool catalog")
 }
}
