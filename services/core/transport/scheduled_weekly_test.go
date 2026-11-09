package transport

import (
 "context"
 "encoding/json"
 "strings"
 "testing"
 "time"

 "github.com/dbontr/homelab-new/system/agentflow"
 "github.com/dbontr/homelab-new/system/store"
)

func TestScheduledWeeklyReportRequiresExplicitPersonalReadAndOptIn(t *testing.T){
 s,_,_:=flowFixture(t)
 program:=CapabilityProgram{
  Project:"study",Objective:"Summarize the completed week",PersonalRead:true,
  Steps:[]CapabilityStage{{ID:"report",Action:"weekly_report"}},
 }
 input:=AutomationRequest{
  Program:program,EveryMinutes:10080,Enabled:true,NotifyOnEveryRun:true,
 }
 created,err:=s.createCapabilityAutomation(context.Background(),input)
 if err!=nil{t.Fatal(err)}
 if !created.NotifyOnEveryRun||created.NotifyOnChange||created.EveryMinutes!=10080{
  t.Fatalf("scheduled report lacks explicit notification scope: %+v",created)
 }
 workflow,err:=s.scheduledWorkflow(context.Background(),created)
 if err!=nil||!workflow.PersonalRead||workflow.PersonalWrite||
  len(workflow.Steps)!=1||workflow.Steps[0].Kind!="weekly_report"{
  t.Fatalf("scheduled report expanded into an invalid program: %+v %v",workflow,err)
 }
 input.Program.PersonalRead=false
 if _,err:=s.createCapabilityAutomation(context.Background(),input);err==nil{
  t.Fatal("personal weekly report accepted without owner read consent")
 }
 input.Program.PersonalRead=true
 input.EveryMinutes=60
 if _,err:=s.createCapabilityAutomation(context.Background(),input);err==nil{
  t.Fatal("noisy hourly report notification accepted")
 }
 input.EveryMinutes=10080
 input.NotifyOnChange=true
 if _,err:=s.createCapabilityAutomation(context.Background(),input);err==nil{
  t.Fatal("conflicting notification semantics accepted")
 }
 input.NotifyOnChange=false
 input.Program.Steps[0].Action="project_context"
 if _,err:=s.createCapabilityAutomation(context.Background(),input);err==nil{
  t.Fatal("completion notifications approved for unrelated background activity")
 }
}

func TestWeeklyCapabilityReadsPriorWeekWithoutChangingPersonalRecords(t *testing.T){
 s,_,_:=flowFixture(t)
 original,err:=s.db.AddTask(store.TaskInput{Title:"Keep detailed notes",Minutes:20})
 if err!=nil{t.Fatal(err)}
 w:=agentflow.Workflow{
  Project:"study",CapabilityPlan:true,PersonalRead:true,
  AutomationDue:time.Date(2027,3,15,16,0,0,0,time.UTC),
 }
 claim:=agentflow.Claim{StepID:"report",Kind:"weekly_report",Input:json.RawMessage(`{}`)}
 value,err:=s.executeCapabilityStep(context.Background(),w,claim)
 if err!=nil{t.Fatal(err)}
 report,ok:=value.(store.Report)
 if !ok||report.WeekOf!="2027-03-08"||
  !strings.Contains(report.Note,"recorded"){
  t.Fatalf("weekly report returned inaccurate evidence: %+v",value)
 }
 if got:=s.db.Tasks("");len(got)!=1||got[0].Revision!=original.Revision{
  t.Fatal("scheduled read mutated personal tasks")
 }
 w.PersonalRead=false
 if _,err:=s.executeCapabilityStep(context.Background(),w,claim);err==nil{
  t.Fatal("weekly report bypassed explicit personal-read consent")
 }
}
