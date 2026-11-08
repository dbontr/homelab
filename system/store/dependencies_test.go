package store

import "testing"

func TestTaskDependenciesAffectRecommendations(t *testing.T) {
 s,path:=testStore(t)
 first,err:=s.AddTask(TaskInput{Title:"Prepare source",Minutes:20})
 if err!=nil{t.Fatal(err)}
 blocked,err:=s.AddTask(TaskInput{Title:"Run build",Minutes:20,ParentID:first.ID,DependsOn:[]string{first.ID}})
 if err!=nil{t.Fatal(err)}
 if blocked.ParentID!=first.ID||len(blocked.DependsOn)!=1{t.Fatal("task relations not persisted")}
 for _,task:=range s.Suggest(0,"","","",10){
   if task.ID==blocked.ID{t.Fatal("blocked task recommended")}
 }
 if _,err:=s.CompleteTask(first.ID);err!=nil{t.Fatal(err)}
 found:=false
 for _,task:=range s.Suggest(0,"","","",10){
   if task.ID==blocked.ID{found=true}
 }
 if !found{t.Fatal("prerequisite completion did not unlock task")}
 reopened,err:=Open(path,"UTC")
 if err!=nil{t.Fatal(err)}
 tasks:=reopened.Tasks("open")
 if len(tasks)!=1||tasks[0].ParentID!=first.ID||tasks[0].DependsOn[0]!=first.ID{t.Fatalf("relations not durable: %+v",tasks)}
 tasks[0].DependsOn[0]="changed"
 if reopened.Tasks("open")[0].DependsOn[0]!=first.ID{t.Fatal("returned task mutated internal state")}
}

func TestTaskRelationsRequireKnownUniqueReferences(t *testing.T){
 s,_:=testStore(t)
 first,err:=s.AddTask(TaskInput{Title:"Prerequisite"});if err!=nil{t.Fatal(err)}
 for _,bad:=range []TaskInput{
  {Title:"Missing parent",ParentID:"absent"},
  {Title:"Missing dependency",DependsOn:[]string{"absent"}},
  {Title:"Duplicate dependency",DependsOn:[]string{first.ID,first.ID}},
 }{
   if _,err:=s.AddTask(bad);err==nil{t.Fatalf("invalid relation accepted: %+v",bad)}
 }
}
