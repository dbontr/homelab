package transport

import (
 "context"
 "encoding/json"
 "errors"
 "net/http"
 "os"
 "path/filepath"
 "strings"
 "testing"

 "github.com/dbontr/homelab-new/system/store"
)

type taskPreviewModel struct{
 output json.RawMessage
 calls int
 title string
 minutes int
}
func(m *taskPreviewModel)ProposeWorkflowStages(context.Context,string,string,bool)(json.RawMessage,error){
 return nil,errors.New("unrelated capability planner used")
}
func(m *taskPreviewModel)ContinueWorkflowStages(context.Context,string,string,[]map[string]any,int)(json.RawMessage,error){
 return nil,errors.New("unrelated continuation model used")
}
func(m *taskPreviewModel)ProposeTaskSteps(_ context.Context,title string,minutes int)(json.RawMessage,error){
 m.calls++;m.title=title;m.minutes=minutes
 return m.output,nil
}

func TestTaskBreakdownPreviewRequiresDisclosureAndNeverWritesRecords(t *testing.T){
 db,err:=store.Open(filepath.Join(t.TempDir(),"personal.json"),"UTC")
 if err!=nil{t.Fatal(err)}
 parent,err:=db.AddTask(store.TaskInput{Title:"Explain spectral overlap",Notes:"private study notes",
  Minutes:90,GoalID:""})
 if err!=nil{t.Fatal(err)}
 gateway,err:=New(db,strings.Repeat("p",40),nil)
 if err!=nil{t.Fatal(err)}
 model:=&taskPreviewModel{output:json.RawMessage(`{"steps":[
  {"title":"Derive shifted spectra","minutes":35},
  {"title":"Check nonoverlap","minutes":25,"depends_on_steps":[0]}
 ]}`)}
 gateway.SetCapabilityPlanner(model)
 if _,err:=gateway.taskDecompositionPreview(context.Background(),parent.ID,1,false);err==nil||model.calls!=0{
  t.Fatal("model received private title without explicit disclosure")
 }
 plan,err:=gateway.taskDecompositionPreview(context.Background(),parent.ID,1,true)
 if err!=nil{t.Fatal(err)}
 if model.calls!=1||model.title!=parent.Title||model.minutes!=90||
  !plan.RequiresOwnerSubmission||plan.TotalMinutes!=60||len(plan.Steps)!=2||
  len(plan.SharedWithPlanningModel)!=2{
  t.Fatalf("invalid unsaved model proposal: %+v",plan)
 }
 saved:=db.Tasks("")
 if len(saved)!=1||saved[0].Revision!=parent.Revision||saved[0].Decomposed{
  t.Fatal("model preview committed a task mutation")
 }
 if _,err:=db.DecomposeTask(plan.ParentID,plan.ExpectedRevision,plan.Steps);err!=nil{t.Fatal(err)}
 if _,err:=gateway.taskDecompositionPreview(context.Background(),parent.ID,1,true);!errors.Is(err,store.ErrTaskRevision){
  t.Fatalf("stale proposal was not revision-fenced: %v",err)
 }
 if _,err:=gateway.taskDecompositionPreview(context.Background(),"absent",1,true);!errors.Is(err,os.ErrNotExist){
  t.Fatalf("missing task lookup was not rejected: %v",err)
 }
}

func TestTaskBreakdownPreviewRejectsInvalidModelPlans(t *testing.T){
 db,err:=store.Open(filepath.Join(t.TempDir(),"personal.json"),"UTC")
 if err!=nil{t.Fatal(err)}
 parent,err:=db.AddTask(store.TaskInput{Title:"Design FPGA pipeline",Minutes:120})
 if err!=nil{t.Fatal(err)}
 gateway,err:=New(db,strings.Repeat("m",40),nil)
 if err!=nil{t.Fatal(err)}
 model:=&taskPreviewModel{}
 gateway.SetCapabilityPlanner(model)
 cases:=[]string{
  `{"steps":[{"title":"One","minutes":10},{"title":"Two","minutes":10,"depends_on_steps":[1]}]}`,
  `{"steps":[{"title":"One","minutes":10},{"title":"Two","minutes":10}],"run":"true"}`,
  `{"steps":[{"title":"One","minutes":10},{"title":"Two","minutes":0}]}`,
  `{"steps":[{"title":"Only step","minutes":10}]}`,
  `{"steps":[]}`,
 }
 for _,bad:=range cases{
  model.output=json.RawMessage(bad)
  if _,err:=gateway.taskDecompositionPreview(context.Background(),parent.ID,1,true);err==nil{
   t.Fatalf("untrusted task breakdown was accepted: %s",bad)
  }
  if tasks:=db.Tasks("");len(tasks)!=1||tasks[0].Revision!=1||tasks[0].Decomposed{
   t.Fatal("rejected model content mutated personal records")
  }
 }
}

func TestTaskBreakdownPreviewNativeAndMCPParities(t *testing.T){
 db,err:=store.Open(filepath.Join(t.TempDir(),"personal.json"),"UTC")
 if err!=nil{t.Fatal(err)}
 parent,err:=db.AddTask(store.TaskInput{Title:"Analyze accelerator scaling",Minutes:90})
 if err!=nil{t.Fatal(err)}
 gateway,err:=New(db,strings.Repeat("t",40),nil)
 if err!=nil{t.Fatal(err)}
 model:=&taskPreviewModel{output:json.RawMessage(`{"steps":[
  {"title":"Measure baseline","minutes":30},
  {"title":"Analyze scaling","minutes":30,"depends_on_steps":[0]}
 ]}`)}
 gateway.SetCapabilityPlanner(model)
 handler:=gateway.Handler()
 payload:=`{"expected_revision":1,"share_title_with_model":true}`
 endpoint:="/v1/tasks/"+parent.ID+"/decomposition-preview"
 if got:=request(handler,http.MethodPost,endpoint,payload,"");got.Code!=http.StatusUnauthorized{
  t.Fatalf("unauthenticated model preview accepted: %d",got.Code)
 }
 if got:=request(handler,http.MethodPost,endpoint,
  `{"expected_revision":1,"share_title_with_model":false}`,strings.Repeat("t",40));got.Code!=422{
  t.Fatalf("missing disclosure consent accepted: %d",got.Code)
 }
 native:=request(handler,http.MethodPost,endpoint,payload,strings.Repeat("t",40))
 if native.Code!=http.StatusOK{
  t.Fatalf("native planning unavailable: %d %s",native.Code,native.Body.String())
 }
 var plan TaskDecompositionPreview
 if err:=json.Unmarshal(native.Body.Bytes(),&plan);err!=nil||!plan.RequiresOwnerSubmission{
  t.Fatalf("native preview has no review boundary: %+v %v",plan,err)
 }
 discovery:=request(handler,http.MethodPost,"/mcp",
  `{"jsonrpc":"2.0","id":1,"method":"tools/list"}`,strings.Repeat("t",40))
 if !strings.Contains(discovery.Body.String(),`"task_decomposition_preview"`){
  t.Fatal("model preview not discoverable by MCP")
 }
 args,_:=json.Marshal(map[string]any{
  "parent_id":parent.ID,"expected_revision":1,"share_title_with_model":true,
 })
 envelope,_:=json.Marshal(map[string]any{
  "jsonrpc":"2.0","id":2,"method":"tools/call",
  "params":map[string]any{"name":"task_decomposition_preview","arguments":json.RawMessage(args)},
 })
 mcp:=request(handler,http.MethodPost,"/mcp",string(envelope),strings.Repeat("t",40))
 if mcp.Code!=200||strings.Contains(mcp.Body.String(),`"isError":true`)||
  !strings.Contains(mcp.Body.String(),"Measure baseline"){
  t.Fatalf("MCP planning failed: %d %s",mcp.Code,mcp.Body.String())
 }
 if rows:=db.Tasks("");len(rows)!=1||rows[0].Decomposed{
  t.Fatal("MCP planning wrote tasks without an owner submission")
 }
}
