package transport

import (
 "context"
 "encoding/json"
 "testing"

 "github.com/dbontr/homelab-new/system/agentflow"
)

func TestAutomationSubmissionRequiresReadOnlyAuthorizedProject(t *testing.T){
 s,_,_:=flowFixture(t)
 input:=AutomationRequest{EveryMinutes:60,Enabled:true,Program:CapabilityProgram{
  Project:"study",Objective:"Review active research",Steps:[]CapabilityStage{
   {ID:"research",Action:"research_search",Query:"electron"},
  },
 }}
 created,err:=s.createCapabilityAutomation(context.Background(),input)
 if err!=nil{t.Fatal(err)}
 if created.Project!="study"||created.Revision!=1||created.LastWorkflowID!=""{
  t.Fatalf("invalid newly created scheduled record: %+v",created)
 }
 runtime,err:=s.scheduledWorkflow(context.Background(),created)
 if err!=nil||runtime.ID!=agentflow.AutomationRunID(created.ID,created.NextAt)||
  runtime.PersonalWrite||runtime.AutoReplan{t.Fatalf("scheduled workflow bypassed read-only policy: %+v %v",runtime,err)}
 input.Program.Steps[0].Action="task_create"
 if _,err:=s.createCapabilityAutomation(context.Background(),input);err==nil{
  t.Fatal("scheduled personal write accepted")
 }
 input.Program.Steps[0].Action="research_search"
 input.Program.AutoReplan=true
 if _,err:=s.createCapabilityAutomation(context.Background(),input);err==nil{
  t.Fatal("background model continuation accepted without explicit route")
 }
 input.Program.AutoReplan=false
 input.Program.Steps[0].Action="learning_summary"
 input.Program.Steps[0].Query=""
 if _,err:=s.createCapabilityAutomation(context.Background(),input);err==nil{
  t.Fatal("background personal-read consent bypassed")
 }
 input.Program.PersonalRead=true
 if _,err:=s.createCapabilityAutomation(context.Background(),input);err!=nil{
  t.Fatalf("explicit personal read scope rejected: %v",err)
 }
 input.EveryMinutes=1
 if _,err:=s.createCapabilityAutomation(context.Background(),input);err==nil{
  t.Fatal("rapid background polling accepted")
 }
 records,err:=s.workflows.ListAutomations()
 if err!=nil||len(records)!=2{t.Fatalf("scheduler did not persist exactly approved programs: %+v %v",records,err)}
 var result CapabilityProgram
 if err:=json.Unmarshal(records[0].Program,&result);err!=nil||result.PersonalWrite{
  t.Fatalf("stored schedule gained personal write permission: %+v %v",result,err)
 }
}
