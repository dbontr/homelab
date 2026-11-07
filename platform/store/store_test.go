package store

import (
 "os"
 "path/filepath"
 "testing"
 "time"
)
func testStore(t *testing.T)(*Store,string){
 t.Helper();path:=filepath.Join(t.TempDir(),"private","state.json")
 s,e:=Open(path,"America/Chicago");if e!=nil{t.Fatal(e)}
 return s,path
}
func TestPersistsTasksAndGoals(t *testing.T){
 s,path:=testStore(t)
 g,e:=s.AddGoal("Understand electromagnetic theory","");if e!=nil{t.Fatal(e)}
 task,e:=s.AddTask(TaskInput{Title:"Derive the multipole expansion",GoalID:g.ID,Priority:5,Minutes:60,Energy:"high"});if e!=nil{t.Fatal(e)}
 t1,e:=s.CompleteTask(task.ID);if e!=nil||t1.CompletedAt==nil{t.Fatalf("complete %v",e)}
 t2,e:=s.CompleteTask(task.ID);if e!=nil||!t2.CompletedAt.Equal(*t1.CompletedAt){t.Fatal("completion not idempotent")}
 loaded,e:=Open(path,"America/Chicago");if e!=nil{t.Fatal(e)}
 if len(loaded.Tasks("completed"))!=1||len(loaded.Goals())!=1{t.Fatal("state not persisted")}
 info,e:=os.Stat(path);if e!=nil{t.Fatal(e)};if info.Mode().Perm()!=0600{t.Fatalf("permissions %o",info.Mode().Perm())}
}
func TestQuickWinsAndDailyMinimum(t *testing.T){
 s,_:=testStore(t)
 _,_=s.AddTask(TaskInput{Title:"Write manuscript",Minutes:120,Priority:5})
 short,e:=s.AddTask(TaskInput{Title:"Pick up package",Minutes:10,Priority:1});if e!=nil{t.Fatal(e)}
 picked:=s.Suggest(20,"","","quick",3)
 if len(picked)!=1||picked[0].ID!=short.ID{t.Fatalf("recommendations %#v",picked)}
 _,e=s.SetRule("minimum_daily_tasks","1","constraint");if e!=nil{t.Fatal(e)}
 _,e=s.CompleteTask(short.ID);if e!=nil{t.Fatal(e)}
 rep,e:=s.Report(time.Now().In(s.zone).Format("2006-01-02"));if e!=nil{t.Fatal(e)}
 if rep.TotalCompleted!=1||rep.MinimumDailyTasks!=1{t.Fatalf("report %#v",rep)}
 today:=time.Now().In(s.zone).Format("2006-01-02")
 for _,d:=range rep.Days{if d.Date==today{if !d.MinimumMet{t.Fatal("threshold not met")};return}}
 t.Fatal("missing current day")
}
func TestRejectsInvalidInput(t *testing.T){
 s,path:=testStore(t)
 if _,e:=s.AddTask(TaskInput{});e==nil{t.Fatal("accepted empty task")}
 if _,e:=s.AddTask(TaskInput{Title:"Invalid reference",GoalID:"missing"});e==nil{t.Fatal("accepted missing goal")}
 if _,e:=s.SetRule("minimum_daily_tasks","a lot","constraint");e==nil{t.Fatal("accepted invalid threshold")}
 if _,e:=s.Report("not-date");e==nil{t.Fatal("accepted invalid report date")}
 if e:=os.WriteFile(path,[]byte("{"),0600);e!=nil{t.Fatal(e)}
 if _,e:=Open(path,"UTC");e==nil{t.Fatal("accepted corrupted state")}
}
