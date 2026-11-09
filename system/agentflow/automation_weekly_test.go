package agentflow

import (
 "encoding/json"
 
 "testing"
 "time"
)

func TestScheduledWeeklyReportAlertsAfterEachConfirmedRun(t *testing.T){
 s:=testStore(t)
 now:=time.Date(2027,3,8,9,0,0,0,time.UTC)
 s.now=func()time.Time{return now}
 a,err:=s.CreateAutomation(Automation{
  ID:"weekly-report",Project:"research",EveryMinutes:10080,
  Enabled:true,NotifyOnEveryRun:true,
  Program:json.RawMessage(`{"project":"research","personal_read":true,"steps":[{"id":"report","action":"weekly_report"}]}`),
 })
 if err!=nil{t.Fatal(err)}
 run:=func(a Automation,value string,failed bool){
  now=a.NextAt.Add(time.Minute)
  w:=Workflow{
   ID:AutomationRunID(a.ID,a.NextAt),Project:a.Project,
   CapabilityPlan:true,PersonalRead:true,
   AutomationID:a.ID,AutomationDue:a.NextAt,
   Steps:[]Step{{ID:"report",Kind:"weekly_report",Input:json.RawMessage(`{}`),MaxAttempts:1}},
  }
  if _,err:=s.FireAutomation(a,w);err!=nil{t.Fatal(err)}
  claim,err:=s.ClaimReady(w.ID,time.Minute)
  if err!=nil{t.Fatal(err)}
  failure:=""
  if failed{failure="weekly report unavailable"}
  if err:=s.Complete(claim,json.RawMessage(value),failure);err!=nil{t.Fatal(err)}
  if _,err:=s.ReconcileAutomationObservations(10);err!=nil{t.Fatal(err)}
 }
 run(a,`{"week_of":"2027-03-01"}`,false)
 alerts,err:=s.ListAutomationAlerts(10)
 if err!=nil||len(alerts)!=1||alerts[0].Acknowledged{
  t.Fatalf("first successful weekly report did not alert: %+v %v",alerts,err)
 }
 if processed,err:=s.ReconcileAutomationObservations(10);err!=nil||processed!=0{
  t.Fatalf("weekly report was observed repeatedly: %d %v",processed,err)
 }
 a,err=s.GetAutomation(a.ID)
 if err!=nil{t.Fatal(err)}
 run(a,`{"week_of":"2027-03-01"}`,false)
 alerts,err=s.ListAutomationAlerts(10)
 if err!=nil||len(alerts)!=2{t.Fatalf("unchanged weekly report lacked its requested alert: %+v %v",alerts,err)}
 a,err=s.GetAutomation(a.ID)
 if err!=nil{t.Fatal(err)}
 run(a,`{}`,true)
 alerts,err=s.ListAutomationAlerts(10)
 if err!=nil||len(alerts)!=2{t.Fatalf("failed report was marked successful: %+v %v",alerts,err)}
 if _,err:=s.AcknowledgeAutomationAlert(a.ID,alerts[0].WorkflowID,alerts[0].Revision);err!=nil{
  t.Fatal(err)
 }
}

func TestWeeklyAutomationRejectsConflictingNotifications(t *testing.T){
 s:=testStore(t)
 for _,a:=range []Automation{
  {ID:"both",Project:"study",EveryMinutes:10080,NotifyOnChange:true,NotifyOnEveryRun:true,Program:json.RawMessage(`{}`)},
  {ID:"too-frequent",Project:"study",EveryMinutes:60,NotifyOnEveryRun:true,Program:json.RawMessage(`{}`)},
 }{
  if _,err:=s.CreateAutomation(a);err==nil{t.Fatalf("invalid weekly notification accepted: %+v",a)}
 }
}
