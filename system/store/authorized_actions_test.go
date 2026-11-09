package store

import (
 "path/filepath"
 "strings"
 "testing"
)

func TestAuthorizedActionReceiptsPersist(t *testing.T) {
 path:=filepath.Join(t.TempDir(),"personal.json")
 s,err:=Open(path,"UTC")
 if err!=nil{t.Fatal(err)}
 goalKey:=strings.Repeat("e",64)
 firstGoal,err:=s.AddGoalOnce(goalKey,"Understand field theory","")
 if err!=nil{t.Fatal(err)}
 repeat,err:=s.AddGoalOnce(goalKey,"Understand field theory","")
 if err!=nil||firstGoal.ID!=repeat.ID{t.Fatalf("goal duplicated: %v %v",repeat,err)}
 if _,err:=s.AddGoalOnce(goalKey,"Different goal","");err==nil{t.Fatal("goal key reused with different contents")}

 task,err:=s.AddTask(TaskInput{Title:"Read chapter",GoalID:firstGoal.ID})
 if err!=nil{t.Fatal(err)}
 key:=strings.Repeat("f",64)
 completed,err:=s.CompleteTaskOnce(key,task.ID,task.Revision)
 if err!=nil||completed.Status!="completed"{t.Fatalf("completion: %v %v",completed,err)}
 if _,err:=s.CompleteTaskOnce(strings.Repeat("a",64),task.ID,task.Revision);err==nil{
  t.Fatal("unrelated workflow completed the same task again")
 }
 next,err:=s.CompleteTaskOnce(key,task.ID,task.Revision)
 if err!=nil||next.Revision!=completed.Revision{t.Fatalf("receipt replay: %v %v",next,err)}
 s,err=Open(path,"UTC")
 if err!=nil{t.Fatal(err)}
 again,err:=s.AddGoalOnce(goalKey,"Understand field theory","")
 if err!=nil||again.ID!=firstGoal.ID{t.Fatal("goal receipt lost on restart")}
 againTask,err:=s.CompleteTaskOnce(key,task.ID,task.Revision)
 if err!=nil||againTask.ID!=task.ID{t.Fatal("completion receipt lost on restart")}
 if len(s.Goals())!=1||len(s.Tasks(""))!=1{t.Fatal("replayed action changed personal records")}
}
func TestTaskCompletionRevisionIsEnforced(t *testing.T) {
 s,err:=Open(filepath.Join(t.TempDir(),"db.json"),"UTC")
 if err!=nil{t.Fatal(err)}
 task,err:=s.AddTask(TaskInput{Title:"First attempt"})
 if err!=nil{t.Fatal(err)}
 if _,err:=s.CompleteTaskOnce(strings.Repeat("b",64),task.ID,task.Revision+1);err==nil{t.Fatal("stale revision accepted")}
 if taskList:=s.Tasks("");len(taskList)!=1||taskList[0].Status!="open"{t.Fatal("rejected operation mutated task")}
}
