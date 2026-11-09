package store

import (
 "errors"
 "os"
 "testing"
)

func TestTaskDecompositionPersistsAllStepsAndDependenciesAtomically(t *testing.T){
 s,path:=testStore(t)
 goal,err:=s.AddGoal("Derive a result","")
 if err!=nil{t.Fatal(err)}
 setup,err:=s.AddTask(TaskInput{Title:"Prepare notes",Minutes:15})
 if err!=nil{t.Fatal(err)}
 parent,err:=s.AddTask(TaskInput{
  Title:"Write derivation",GoalID:goal.ID,Minutes:180,Priority:4,
  Energy:"high",Location:"office",Due:"2027-04-15",
  DependsOn:[]string{setup.ID},Links:[]string{"homelab:project:research"},
 })
 if err!=nil{t.Fatal(err)}
 priority:=0
 result,err:=s.DecomposeTask(parent.ID,parent.Revision,[]TaskStepInput{
  {Title:"Define variables",Minutes:20},
  {Title:"Prove lemma",Minutes:45,DependsOnSteps:[]int{0}},
  {Title:"Write conclusion",Minutes:30,DependsOnSteps:[]int{0,1},Priority:&priority},
 })
 if err!=nil{t.Fatal(err)}
 if !result.Parent.Decomposed||result.Parent.Revision!=parent.Revision+1||len(result.Steps)!=3{
  t.Fatalf("decomposition was not persisted as one revision: %+v",result)
 }
 first,second,last:=result.Steps[0],result.Steps[1],result.Steps[2]
 if first.GoalID!=goal.ID||first.ParentID!=parent.ID||first.Due!=parent.Due||
  first.Energy!="high"||first.Location!="office"||first.Priority!=4||
  len(first.DependsOn)!=1||first.DependsOn[0]!=setup.ID||
  len(first.Links)!=1||first.Links[0]!="homelab:project:research"{
  t.Fatalf("subtask lost inherited context or prerequisites: %+v",first)
 }
 if len(second.DependsOn)!=2||second.DependsOn[1]!=first.ID||
  len(last.DependsOn)!=3||last.DependsOn[1]!=first.ID||last.DependsOn[2]!=second.ID||
  last.Priority!=0{
  t.Fatalf("step dependencies or explicit zero priority were not preserved: %+v",result.Steps)
 }
 reopened,err:=Open(path,"America/Chicago")
 if err!=nil{t.Fatal(err)}
 if len(reopened.Tasks(""))!=5{
  t.Fatal("decomposition did not survive restart")
 }
 result.Steps[1].DependsOn[0]="mutated"
 for _,item:=range s.Tasks(""){
  if item.ID==second.ID && item.DependsOn[0]!="mutated"{goto isolated}
 }
 t.Fatal("returned decomposition has an alias to persistent task dependencies")
isolated:
 if _,err:=s.DecomposeTask(parent.ID,parent.Revision,[]TaskStepInput{
  {Title:"Another 1",Minutes:10},{Title:"Another 2",Minutes:10},
 });!errors.Is(err,ErrTaskRevision){
  t.Fatalf("stale parent revision allowed another decomposition: %v",err)
 }
 if _,err:=s.DecomposeTask(parent.ID,parent.Revision+1,[]TaskStepInput{
  {Title:"Another 1",Minutes:10},{Title:"Another 2",Minutes:10},
 });!errors.Is(err,ErrTaskRevision){
  t.Fatalf("decomposed parent accepted a second task graph: %v",err)
 }
}

func TestTaskDecompositionRejectsInvalidDependenciesWithoutPartialWrites(t *testing.T){
 s,_:=testStore(t)
 parent,err:=s.AddTask(TaskInput{Title:"Task"})
 if err!=nil{t.Fatal(err)}
 cases:=[][]TaskStepInput{
  {{Title:"A",Minutes:10},{Title:"B",Minutes:10,DependsOnSteps:[]int{1}}},
  {{Title:"A",Minutes:10},{Title:"B",Minutes:10,DependsOnSteps:[]int{0,0}}},
  {{Title:"A",Minutes:0},{Title:"B",Minutes:10}},
  {{Title:"A",Minutes:10},{Title:" ",Minutes:10}},
  {{Title:"A",Minutes:10},{Title:"B",Minutes:10,Energy:"exhausted"}},
 }
 for i,steps:=range cases{
  if _,err:=s.DecomposeTask(parent.ID,1,steps);err==nil{
   t.Fatalf("invalid decomposition case %d was committed",i)
  }
  tasks:=s.Tasks("")
  if len(tasks)!=1||tasks[0].Decomposed||tasks[0].Revision!=parent.Revision{
   t.Fatalf("failed step mutated task store: %+v",tasks)
  }
 }
 if _,err:=s.DecomposeTask("absent",1,[]TaskStepInput{{Title:"A",Minutes:10},{Title:"B",Minutes:10}});!errors.Is(err,os.ErrNotExist){
  t.Fatalf("absent parent did not return not-found: %v",err)
 }
}

func TestDecomposedParentsAreNotSuggestedOrScheduledAndCompletionNeedsChildren(t *testing.T){
 s,_:=testStore(t)
 parent,err:=s.AddTask(TaskInput{Title:"Conduct experiment",Minutes:120,Priority:5})
 if err!=nil{t.Fatal(err)}
 result,err:=s.DecomposeTask(parent.ID,1,[]TaskStepInput{
  {Title:"Set up apparatus",Minutes:30},
  {Title:"Record results",Minutes:30,DependsOnSteps:[]int{0}},
 })
 if err!=nil{t.Fatal(err)}
 for _,task:=range s.Suggest(0,"","","",30){
  if task.ID==parent.ID{t.Fatal("container task appeared in actionable suggestions")}
 }
 plan,err:=s.PlanWeek("2027-04-05",60,9,nil)
 if err!=nil{t.Fatal(err)}
 for _,slot:=range plan.Slots{
  if slot.TaskID==parent.ID{t.Fatal("container task double-counted during weekly planning")}
 }
 if _,err:=s.CompleteTask(parent.ID);!errors.Is(err,ErrTaskChildrenIncomplete){
  t.Fatalf("parent completed with unfinished children: %v",err)
 }
 if _,err:=s.CompleteTask(result.Steps[0].ID);err!=nil{t.Fatal(err)}
 if _,err:=s.CompleteTask(result.Steps[1].ID);err!=nil{t.Fatal(err)}
 if _,err:=s.CompleteTask(parent.ID);err!=nil{t.Fatal(err)}
 if _,err:=s.DecomposeTask(parent.ID,2,[]TaskStepInput{{Title:"Another",Minutes:10},{Title:"Again",Minutes:10}});!errors.Is(err,ErrTaskRevision){
  t.Fatalf("completed task accepted decomposition: %v",err)
 }
}
