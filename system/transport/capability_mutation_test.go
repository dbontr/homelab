package transport

import (
 "context"
 "encoding/json"
 "testing"

 "github.com/dbontr/homelab-new/system/agentflow"
)

func TestWorkflowWritesRequireIndependentOwnerConsent(t *testing.T){
 s,_,_:=flowFixture(t)
 program:=CapabilityProgram{Project:"study",Objective:"Prepare research follow-up",
  Steps:[]CapabilityStage{{ID:"create",Action:"task_create",Query:"Revisit the proof"}}}
 if _,err:=s.submitCapabilityWorkflow(context.Background(),program);err==nil{
  t.Fatal("task mutation accepted without personal_write")
 }
 program.PersonalWrite=true
 created,err:=s.submitCapabilityWorkflow(context.Background(),program)
 if err!=nil{t.Fatal(err)}
 if err:=s.advanceCapabilityWorkflow(context.Background(),created.ID,2);err!=nil{t.Fatal(err)}
 tasks:=s.db.Tasks("")
 if len(tasks)!=1||tasks[0].Title!="Revisit the proof"{t.Fatalf("task not confirmed: %+v",tasks)}
 persisted,err:=s.workflows.Get(created.ID)
 if err!=nil||persisted.Steps[0].Status!=agentflow.Succeeded{t.Fatalf("workflow: %+v %v",persisted,err)}
 replay:=agentflow.Claim{WorkflowID:created.ID,StepID:"create",Kind:"task_create",
  Input:json.RawMessage(`{"query":"Revisit the proof"}`)}
 first,err:=s.executeCapabilityStep(context.Background(),persisted,replay)
 if err!=nil{t.Fatal(err)}
 again,err:=s.executeCapabilityStep(context.Background(),persisted,replay)
 if err!=nil{t.Fatal(err)}
 if first.(map[string]any)["task_id"]!=again.(map[string]any)["task_id"]||len(s.db.Tasks(""))!=1{
  t.Fatal("workflow replay duplicated a task")
 }
 if err:=validCapabilityStage(program.Steps[0],true);err==nil{
  t.Fatal("model-authored plan acquired task mutation authority")
 }
}
