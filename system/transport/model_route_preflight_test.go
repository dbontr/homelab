package transport

import (
 "context"
 "testing"

 "github.com/dbontr/homelab-new/system/agentflow"
)

func TestWorkflowAdmissionRequiresDeclaredContinuationPrivacy(t *testing.T){
 srv,_,_:=flowFixture(t)
 endpoint:="http://127.0.0.1:9801/v1/chat/completions"
 planner,err:=agentflow.NewModelRouter([]agentflow.ModelRoute{
  {Name:"planning",Purpose:"planning",Endpoint:endpoint,Model:"planning"},
 })
 if err!=nil{t.Fatal(err)}
 srv.SetCapabilityPlanner(planner)
 program:=CapabilityProgram{Project:"study",Objective:"Explore findings",AutoReplan:true,
  Steps:[]CapabilityStage{{ID:"start",Action:"project_context"}}}
 if _,err:=srv.submitCapabilityWorkflow(context.Background(),program);err==nil{
  t.Fatal("unexecutable continuation route was accepted")
 }
 planner,err=agentflow.NewModelRouter([]agentflow.ModelRoute{
  {Name:"planning",Purpose:"planning",Endpoint:endpoint,Model:"planning"},
  {Name:"normal",Purpose:"continuation",Endpoint:endpoint,Model:"normal"},
 })
 if err!=nil{t.Fatal(err)}
 srv.SetCapabilityPlanner(planner)
 if _,err:=srv.submitCapabilityWorkflow(context.Background(),program);err!=nil{
  t.Fatalf("normal read-only continuation rejected: %v",err)
 }
 program.ShareProjectEvidence=true
 if _,err:=srv.submitCapabilityWorkflow(context.Background(),program);err==nil{
  t.Fatal("evidence sharing accepted without explicitly configured evidence recipient")
 }
}
