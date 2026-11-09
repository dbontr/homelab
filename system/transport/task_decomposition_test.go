package transport

import (
 "encoding/json"
 "net/http"
 "path/filepath"
 "strings"
 "testing"

 "github.com/dbontr/homelab-new/system/store"
)

func TestTaskDecompositionNativeEndpointEnforcesAuthAndRevisions(t *testing.T){
 db,err:=store.Open(filepath.Join(t.TempDir(),"tasks.json"),"UTC")
 if err!=nil{t.Fatal(err)}
 gateway,err:=New(db,strings.Repeat("x",40),nil)
 if err!=nil{t.Fatal(err)}
 token:=strings.Repeat("x",40)
 handler:=gateway.Handler()
 parent,err:=db.AddTask(store.TaskInput{Title:"Complete manuscript",Minutes:180})
 if err!=nil{t.Fatal(err)}
 body:=`{"expected_revision":1,"steps":[{"title":"Outline","minutes":30},{"title":"Draft","minutes":60,"depends_on_steps":[0]}]}`
 url:="/v1/tasks/"+parent.ID+"/decompose"
 if got:=request(handler,http.MethodPost,url,body,"");got.Code!=http.StatusUnauthorized{
  t.Fatalf("anonymous decomposition accepted: %d",got.Code)
 }
 result:=request(handler,http.MethodPost,url,body,token)
 if result.Code!=http.StatusCreated{
  t.Fatalf("authorized decomposition failed: %d %s",result.Code,result.Body.String())
 }
 var created store.TaskDecomposition
 if err:=json.Unmarshal(result.Body.Bytes(),&created);err!=nil{t.Fatal(err)}
 if len(created.Steps)!=2||created.Steps[1].DependsOn[0]!=created.Steps[0].ID||
  !created.Parent.Decomposed||created.Parent.Revision!=2{
  t.Fatalf("native decomposition returned inconsistent graph: %+v",created)
 }
 if got:=request(handler,http.MethodPost,url,body,token);got.Code!=http.StatusConflict{
  t.Fatalf("stale revision was accepted: %d %s",got.Code,got.Body.String())
 }
 if got:=request(handler,http.MethodPost,"/v1/tasks/"+parent.ID+"/complete","",token);got.Code!=http.StatusConflict{
  t.Fatalf("parent was completed before its children: %d",got.Code)
 }
 for _,step:=range created.Steps{
  if got:=request(handler,http.MethodPost,"/v1/tasks/"+step.ID+"/complete","",token);got.Code!=http.StatusOK{
   t.Fatalf("subtask completion failed: %s",got.Body.String())
  }
 }
 if got:=request(handler,http.MethodPost,"/v1/tasks/"+parent.ID+"/complete","",token);got.Code!=http.StatusOK{
  t.Fatalf("parent completion remained blocked: %s",got.Body.String())
 }
}

func TestMCPDecompositionUsesCanonicalTaskStore(t *testing.T){
 db,err:=store.Open(filepath.Join(t.TempDir(),"tasks.json"),"UTC")
 if err!=nil{t.Fatal(err)}
 gateway,err:=New(db,strings.Repeat("y",40),nil)
 if err!=nil{t.Fatal(err)}
 parent,err:=db.AddTask(store.TaskInput{Title:"Model experiment",Minutes:90})
 if err!=nil{t.Fatal(err)}
 handler:=gateway.Handler()
 token:=strings.Repeat("y",40)
 discovery:=request(handler,http.MethodPost,"/mcp",`{"jsonrpc":"2.0","id":1,"method":"tools/list"}`,token)
 if discovery.Code!=http.StatusOK||!strings.Contains(discovery.Body.String(),`"task_decompose"`){
  t.Fatal("MCP discovery omitted decomposition capability")
 }
 arguments,_:=json.Marshal(map[string]any{
  "parent_id":parent.ID,"expected_revision":1,
  "steps":[]map[string]any{
   {"title":"Set up model","minutes":20},
   {"title":"Compare prediction","minutes":20,"depends_on_steps":[]int{0}},
  },
 })
 payload,_:=json.Marshal(map[string]any{
  "jsonrpc":"2.0","id":2,"method":"tools/call",
  "params":map[string]any{"name":"task_decompose","arguments":json.RawMessage(arguments)},
 })
 reply:=request(handler,http.MethodPost,"/mcp",string(payload),token)
 if reply.Code!=http.StatusOK||strings.Contains(reply.Body.String(),`"isError":true`){
  t.Fatalf("MCP decomposition failed: %d %s",reply.Code,reply.Body.String())
 }
 taskList:=db.Tasks("")
 if len(taskList)!=3{t.Fatalf("MCP did not persist complete decomposition: %+v",taskList)}
 var childCount int
 for _,task:=range taskList{
  if task.ParentID==parent.ID{childCount++}
 }
 if childCount!=2{t.Fatalf("MCP child graph incomplete: %+v",taskList)}
}
