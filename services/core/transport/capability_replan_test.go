package transport

import (
 "context"
 "encoding/json"
 "net/http"
 "net/http/httptest"
 "strings"
 "sync/atomic"
 "testing"

 "github.com/dbontr/homelab-new/system/agentflow"
 "github.com/dbontr/homelab-new/system/store"
)

func TestAutonomousCapabilityContinuationUsesConfirmedEvidence(t *testing.T){
 s,_,_:=flowFixture(t)
 var requests atomic.Int32
 model:=httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter,r *http.Request){
  count:=requests.Add(1)
  var request map[string]any
  if err:=json.NewDecoder(r.Body).Decode(&request);err!=nil{t.Error(err);return}
  payload,_:=json.Marshal(request)
  if strings.Contains(string(payload),"personal-private-note"){t.Error("personal data leaked to planning model")}
  content:=`{"finish":true,"steps":[]}`
  if count==1{content=`{"finish":false,"steps":[{"id":"research","action":"research_search","query":"spectral analysis","depends_on":["context"]}]}`}
  _=json.NewEncoder(w).Encode(map[string]any{"choices":[]any{map[string]any{"finish_reason":"stop","message":map[string]any{"content":content}}}})
 }))
 defer model.Close()
 planner,err:=agentflow.NewModelAuthor(model.URL+"/v1/chat/completions","planner-model","")
 if err!=nil{t.Fatal(err)}
 s.SetCapabilityPlanner(planner)
 _,err=s.db.AddTask(store.TaskInput{Title:"personal-private-note",Links:[]string{"homelab:project:study"}})
 if err!=nil{t.Fatal(err)}
 program:=CapabilityProgram{Project:"study",Objective:"Examine source evidence",AutoReplan:true,
   ShareProjectEvidence:true,Steps:[]CapabilityStage{{ID:"context",Action:"project_context"}}}
 submitted,err:=s.submitCapabilityWorkflow(context.Background(),program)
 if err!=nil{t.Fatal(err)}
 if err:=s.advanceCapabilityWorkflow(context.Background(),submitted.ID,3);err!=nil{t.Fatal(err)}
 intermediate,err:=s.capabilityWorkflow(context.Background(),submitted.ID)
 if err!=nil||len(intermediate.Steps)!=2||intermediate.Replans!=1{t.Fatalf("continuation not stored: %+v %v",intermediate,err)}
 if err:=s.advanceCapabilityWorkflow(context.Background(),submitted.ID,3);err!=nil{t.Fatal(err)}
 completed,err:=s.capabilityWorkflow(context.Background(),submitted.ID)
 if err!=nil||completed.Status!="succeeded"||len(completed.Steps)!=2||requests.Load()!=2{
  t.Fatalf("unexpected completed continuation: %+v %v",completed,err)
 }
 if err:=s.advanceCapabilityWorkflow(context.Background(),submitted.ID,3);err!=nil{t.Fatal(err)}
 if requests.Load()!=2{t.Fatal("finished workflow was replanned")}
}

func TestModelCannotExpandAutomaticCapabilityScope(t *testing.T){
 s,_,_:=flowFixture(t)
 model:=httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter,r *http.Request){
  content:=`{"finish":false,"steps":[{"id":"bad","action":"health_records","depends_on":["context"]}]}`
  _=json.NewEncoder(w).Encode(map[string]any{"choices":[]any{map[string]any{"finish_reason":"stop","message":map[string]any{"content":content}}}})
 }))
 defer model.Close()
 author,err:=agentflow.NewModelAuthor(model.URL+"/v1/chat/completions","test","")
 if err!=nil{t.Fatal(err)}
 s.SetCapabilityPlanner(author)
 program:=CapabilityProgram{Project:"study",Objective:"Find source material",AutoReplan:true,
  Steps:[]CapabilityStage{{ID:"context",Action:"project_context"}}}
 submitted,err:=s.submitCapabilityWorkflow(context.Background(),program)
 if err!=nil{t.Fatal(err)}
 if err:=s.advanceCapabilityWorkflow(context.Background(),submitted.ID,3);err!=nil{t.Fatal(err)}
 current,err:=s.capabilityWorkflow(context.Background(),submitted.ID)
 if err!=nil||current.Status!="planning_failed"||len(current.Steps)!=1{
  t.Fatalf("unauthorized model action accepted: %+v %v",current,err)
 }
}
