package agentflow

import (
 "encoding/json"
 "testing"
 "time"
)

func TestCapabilityReplanRevisionAndDependencyFencing(t *testing.T){
 store:=testStore(t)
 original:=Workflow{ID:"study",Project:"project",CapabilityPlan:true,AutoReplan:true,
   Deadline:time.Now().Add(20*time.Minute),
   Steps:[]Step{{ID:"context",Kind:"project_context",Input:json.RawMessage("{}"),MaxAttempts:2}}}
 if err:=store.Create(original);err!=nil{t.Fatal(err)}
 current,err:=store.Get("study");if err!=nil{t.Fatal(err)}
 pending:=Step{ID:"research",Kind:"research_search",DependsOn:[]string{"context"},Input:json.RawMessage(\`{"query":"science"}\`),MaxAttempts:2}
 if err:=store.ExtendCapabilityPlan("study",current.Revision,[]Step{pending},false,"");err==nil{
  t.Fatal("replan accepted before prerequisite success")
 }
 claim,err:=store.ClaimReady("study",time.Minute);if err!=nil{t.Fatal(err)}
 if err:=store.Complete(claim,json.RawMessage(\`{"status":"ok"}\`),"");err!=nil{t.Fatal(err)}
 completed,err:=store.Get("study");if err!=nil{t.Fatal(err)}
 if err:=store.ExtendCapabilityPlan("study",completed.Revision,[]Step{pending},false,"");err!=nil{t.Fatal(err)}
 if err:=store.ExtendCapabilityPlan("study",completed.Revision,[]Step{pending},false,"");err==nil{
  t.Fatal("stale model result modified workflow")
 }
 extended,err:=store.Get("study");if err!=nil{t.Fatal(err)}
 if extended.Replans!=1||len(extended.Steps)!=2||extended.Steps[1].Status!=Pending{
  t.Fatalf("unexpected continuation: %+v",extended)
 }
 if err:=store.ExtendCapabilityPlan("study",extended.Revision,nil,true,"");err==nil{
  t.Fatal("terminated plan despite pending stage")
 }
 claim,err=store.ClaimReady("study",time.Minute);if err!=nil{t.Fatal(err)}
 if claim.StepID!="research"{t.Fatal("unexpected continuation claim")}
 if err:=store.Complete(claim,json.RawMessage(\`{"matches":[]}\`),"");err!=nil{t.Fatal(err)}
 extended,err=store.Get("study");if err!=nil{t.Fatal(err)}
 if err:=store.ExtendCapabilityPlan("study",extended.Revision,nil,true,"");err!=nil{t.Fatal(err)}
 final,err:=store.Get("study");if err!=nil{t.Fatal(err)}
 if !final.PlanComplete||final.Replans!=1{t.Fatalf("plan did not finish: %+v",final)}
}

func TestCapabilityReplanRejectsPrivilegeAndInvalidStructure(t *testing.T){
 store:=testStore(t)
 original:=Workflow{ID:"ready",Project:"project",CapabilityPlan:true,AutoReplan:true,
   Deadline:time.Now().Add(20*time.Minute),
   Steps:[]Step{{ID:"a",Kind:"project_context",Input:json.RawMessage("{}"),Status:Succeeded,MaxAttempts:2}}}
 if err:=store.Create(original);err!=nil{t.Fatal(err)}
 claim,err:=store.ClaimReady("ready",time.Minute);if err!=nil{t.Fatal(err)}
 if err:=store.Complete(claim,json.RawMessage("{}"),"");err!=nil{t.Fatal(err)}
 get:=func()Workflow{w,e:=store.Get("ready");if e!=nil{t.Fatal(e)};return w}
 for _,steps:=range [][]Step{
  {{ID:"duplicate",Kind:"project_context",Input:json.RawMessage("{}"),MaxAttempts:2}},
  {{ID:"cycle",Kind:"project_context",DependsOn:[]string{"cycle"},Input:json.RawMessage("{}"),MaxAttempts:2}},
  {{ID:"a",Kind:"project_context",DependsOn:[]string{"a"},Input:json.RawMessage("{}"),MaxAttempts:2}},
 } {
  if err:=store.ExtendCapabilityPlan("ready",get().Revision,steps,false,"");err==nil{
   t.Fatalf("invalid dependent continuation accepted: %+v",steps)
  }
 }
 if get().ID!="ready"{t.Fatal("workflow identity drift")}
}
