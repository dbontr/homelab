package transport

import (
 "context"
 "encoding/json"
 "testing"

 "github.com/dbontr/homelab-new/system/agentflow"
 "github.com/dbontr/homelab-new/system/store"
)

func TestWorkflowPersonalActionsRequireExplicitAuthority(t *testing.T){
 s,_,_:=flowFixture(t)
 ctx:=context.Background()
 request:=CapabilityProgram{Project:"study",Objective:"Prepare follow-up",
  Steps:[]CapabilityStage{{ID:"task",Action:"task_create",Query:"Revisit the proof"}}}
 if _,err:=s.submitCapabilityWorkflow(ctx,request);err==nil{t.Fatal("write allowed without owner permission")}
 request.PersonalWrite=true
 workflow,err:=s.submitCapabilityWorkflow(ctx,request)
 if err!=nil{t.Fatal(err)}
 if err:=s.advanceCapabilityWorkflow(ctx,workflow.ID,2);err!=nil{t.Fatal(err)}
 tasks:=s.db.Tasks("")
 if len(tasks)!=1||tasks[0].Title!="Revisit the proof"{t.Fatalf("task was not created: %+v",tasks)}
 persisted,err:=s.workflows.Get(workflow.ID)
 if err!=nil||persisted.Steps[0].Status!=agentflow.Succeeded{t.Fatalf("workflow: %v %v",persisted,err)}
 stage:=agentflow.Claim{WorkflowID:workflow.ID,StepID:"task",Kind:"task_create",
  Input:json.RawMessage(`{"query":"Revisit the proof"}`)}
 one,err:=s.executeCapabilityStep(ctx,persisted,stage)
 if err!=nil{t.Fatal(err)}
 two,err:=s.executeCapabilityStep(ctx,persisted,stage)
 if err!=nil{t.Fatal(err)}
 if one.(map[string]any)["task_id"]!=two.(map[string]any)["task_id"]||len(s.db.Tasks(""))!=1 {
  t.Fatal("replayed workflow duplicated task")
 }
 if err:=validCapabilityStage(request.Steps[0],true);err==nil{t.Fatal("model acquired write authority")}
}

func TestWorkflowGoalAndTaskCompletionStayWithinProject(t *testing.T){
 s,_,_:=flowFixture(t)
 ctx:=context.Background()
 task,err:=s.db.AddTask(store.TaskInput{Title:"Complete source review",
  Links:[]string{"homelab:project:study"}})
 if err!=nil{t.Fatal(err)}
 foreign,err:=s.db.AddTask(store.TaskInput{Title:"Private work",
  Links:[]string{"homelab:project:other"}})
 if err!=nil{t.Fatal(err)}
 rejected:=CapabilityProgram{Project:"study",PersonalWrite:true,Objective:"Try foreign task",
  Steps:[]CapabilityStage{{ID:"complete",Action:"task_complete",Query:foreign.ID,ExpectedRevision:foreign.Revision}}}
 wf,err:=s.submitCapabilityWorkflow(ctx,rejected)
 if err!=nil{t.Fatal(err)}
 if err:=s.advanceCapabilityWorkflow(ctx,wf.ID,2);err!=nil{t.Fatal(err)}
 if tasks:=s.db.Tasks("");len(tasks)!=2||tasks[0].Status=="completed"&&tasks[0].ID==foreign.ID{
  t.Fatalf("cross-project task was completed: %+v",tasks)
 }
 request:=CapabilityProgram{Project:"study",PersonalWrite:true,Objective:"Finish work and add goal",
  Steps:[]CapabilityStage{
   {ID:"finish",Action:"task_complete",Query:task.ID,ExpectedRevision:task.Revision},
   {ID:"goal",Action:"goal_create",Query:"Learn scientific reasoning",DependsOn:[]string{"finish"}},
  }}
 allowed,err:=s.submitCapabilityWorkflow(ctx,request)
 if err!=nil{t.Fatal(err)}
 if err:=s.advanceCapabilityWorkflow(ctx,allowed.ID,4);err!=nil{t.Fatal(err)}
 if len(s.db.Goals())!=1{t.Fatal("approved goal not created")}
 for _,item:=range s.db.Tasks("") {
  if item.ID==task.ID&&item.Status!="completed"{t.Fatal("approved task remains open")}
  if item.ID==foreign.ID&&item.Status!="open"{t.Fatal("foreign task changed")}
 }
 old,err:=s.workflows.Get(allowed.ID)
 if err!=nil{t.Fatal(err)}
 if old.Steps[0].Status!=agentflow.Succeeded||old.Steps[1].Status!=agentflow.Succeeded{
  t.Fatalf("workflow did not record both operations: %+v",old)
 }
}
