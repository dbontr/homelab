package agentflow

import (
 "encoding/json"
 "errors"
 "os"
 "path/filepath"
 "testing"
 "time"
)

func TestAutomationSchedulesImmutableReadOnlyWorkflows(t *testing.T){
 root:=t.TempDir()
 s,err:=Open(root)
 if err!=nil{t.Fatal(err)}
 clock:=time.Date(2026,11,9,8,0,0,0,time.UTC)
 s.now=func()time.Time{return clock}
 program:=json.RawMessage(`{"project":"research","steps":[{"id":"sources","action":"research_search","query":"beam"}]}`)
 a,err:=s.CreateAutomation(Automation{ID:"morning",Project:"research",Program:program,EveryMinutes:60,Enabled:true})
 if err!=nil{t.Fatal(err)}
 before,err:=s.DueAutomations(clock,4)
 if err!=nil||len(before)!=0{t.Fatalf("automation fired early: %+v %v",before,err)}
 clock=clock.Add(61*time.Minute)
 due,err:=s.DueAutomations(clock,4)
 if err!=nil||len(due)!=1{t.Fatalf("schedule not due: %+v %v",due,err)}
 w:=Workflow{ID:AutomationRunID(a.ID,a.NextAt),AutomationID:a.ID,AutomationDue:a.NextAt,
  CapabilityPlan:true,Project:a.Project,Objective:"Find source research",
  Steps:[]Step{{ID:"sources",Kind:"research_search",Input:json.RawMessage("{}"),MaxAttempts:2}}}
 created,err:=s.FireAutomation(a,w)
 if err!=nil||created.ID!=w.ID{t.Fatalf("no scheduled run: %+v %v",created,err)}
 if _,err:=s.FireAutomation(a,w);err==nil{t.Fatal("stale schedule revision double-fired")}
 got,err:=s.GetAutomation(a.ID)
 if err!=nil||got.LastWorkflowID!=w.ID||got.Revision!=2{t.Fatalf("schedule did not advance: %+v %v",got,err)}
 list,err:=s.List()
 if err!=nil||len(list)!=1{t.Fatalf("duplicate workflow created: %+v %v",list,err)}
 s,err=Open(root)
 if err!=nil{t.Fatal(err)}
 current,err:=s.GetAutomation(a.ID)
 if err!=nil||current.LastWorkflowID!=w.ID{t.Fatal("scheduled state not durable")}
 if _,err:=s.SetAutomationEnabled(a.ID,current.Revision,false);err!=nil{t.Fatal(err)}
 future,err:=s.DueAutomations(time.Now().Add(8*24*time.Hour),5)
 if err!=nil||len(future)!=0{t.Fatalf("disabled automation fired: %+v %v",future,err)}
}

func TestAutomationRejectsUnsafeRecordsAndConflicts(t *testing.T){
 s,err:=Open(t.TempDir())
 if err!=nil{t.Fatal(err)}
 _,err=s.CreateAutomation(Automation{ID:"read",Project:"project",EveryMinutes:5,Program:json.RawMessage("{}")})
 if err==nil{t.Fatal("five-minute polling accepted")}
 a,err:=s.CreateAutomation(Automation{ID:"read",Project:"project",EveryMinutes:60,Program:json.RawMessage("{}")})
 if err!=nil{t.Fatal(err)}
 if _,err:=s.CreateAutomation(a);!errors.Is(err,os.ErrExist){t.Fatalf("duplicate schedule: %v",err)}
 if _,err:=s.SetAutomationEnabled("read",a.Revision+1,false);err==nil{t.Fatal("stale automation update")}
 path:=filepath.Join(s.dir,"automations","read.json")
 if err:=os.Remove(path);err!=nil{t.Fatal(err)}
 if err:=os.Symlink(filepath.Join(s.dir,".lock"),path);err!=nil{t.Fatal(err)}
 if _,err:=s.GetAutomation("read");err==nil{t.Fatal("symlinked automation accepted")}
}
