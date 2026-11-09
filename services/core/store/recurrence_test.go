package store

import (
 "errors"
 "testing"
 "time"
)

func TestRecurringTaskCreatesSuccessorAndKeepsHistory(t *testing.T){
 s,_:=testStore(t)
 today:=time.Now().In(s.zone).Format("2006-01-02")
 created,err:=s.AddTask(TaskInput{Title:"Review research notes",Due:today,Recurrence:"daily",Minutes:30})
 if err!=nil{t.Fatal(err)}
 finished,err:=s.CompleteTask(created.ID)
 if err!=nil||finished.Status!="completed"{t.Fatalf("complete: %+v %v",finished,err)}
 again,err:=s.CompleteTask(created.ID)
 if err!=nil||again.ID!=finished.ID{t.Fatalf("repeat completion not idempotent: %v",err)}
 open:=s.Tasks("open")
 if len(open)!=1||open[0].ID==created.ID||open[0].Recurrence!="daily"||open[0].Due<=today{
  t.Fatalf("new occurrence missing: %+v",open)
 }
 if len(s.Tasks("completed"))!=1{t.Fatal("completed history not preserved")}
}

func TestTaskRevisionAndAgenda(t *testing.T){
 s,_:=testStore(t)
 base,err:=s.AddTask(TaskInput{Title:"Study",Priority:3})
 if err!=nil{t.Fatal(err)}
 date:=time.Now().In(s.zone).AddDate(0,0,1).Format("2006-01-02")
 scheduled:=date+"T08:00:00-06:00"
 updated,err:=s.UpdateTask(base.ID,TaskChanges{
  Title:"Study Fourier series",Due:date,Scheduled:scheduled,Minutes:60,
  Priority:4,ExpectedRevision:base.Revision,
 })
 if err!=nil||updated.Revision!=2{t.Fatalf("edit: %+v %v",updated,err)}
 if _,err:=s.UpdateTask(base.ID,TaskChanges{
  Title:"Stale",ExpectedRevision:base.Revision,
 });!errors.Is(err,ErrTaskRevision){t.Fatalf("stale edit accepted: %v",err)}
 agenda,err:=s.Agenda(date)
 if err!=nil||len(agenda)!=1||agenda[0].ID!=base.ID{t.Fatalf("agenda: %+v %v",agenda,err)}
 if _,err:=s.UpdateTask(base.ID,TaskChanges{
  Title:"Invalid",Scheduled:date,Minutes:20,ExpectedRevision:updated.Revision,
 });err==nil{t.Fatal("accepted non-RFC3339 timebox")}
}

func TestTaskCaptureAndRecurrenceRules(t *testing.T){
 s,_:=testStore(t)
 captured,err:=s.Capture("Review proofs tomorrow")
 if err!=nil||captured.Title!="Review proofs"||captured.Due==""{t.Fatalf("capture: %+v %v",captured,err)}
 repeat,err:=s.Capture("Study every monday")
 if err!=nil||repeat.Recurrence!="weekly:mon"||repeat.Due==""{t.Fatalf("repeat: %+v %v",repeat,err)}
 if _,err:=s.Capture(" every weekday");err==nil{t.Fatal("accepted a blank title")}
 if _,err:=nextRecurringTime("2026-11-07","nonsense",time.Now(),s.zone);err==nil{t.Fatal("accepted invalid recurrence")}
}
