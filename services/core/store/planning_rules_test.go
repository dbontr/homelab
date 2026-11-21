package store

import (
 "errors"
 "path/filepath"
 "strings"
 "testing"
 "time"
)

func TestWeeklyPlanRespectsActiveRulesWithoutTaskMutation(t *testing.T){
 db,err:=Open(filepath.Join(t.TempDir(),"store.json"),"UTC");if err!=nil{t.Fatal(err)}
 short,err:=db.AddTask(TaskInput{Title:"Review lemma",Minutes:20,Priority:2});if err!=nil{t.Fatal(err)}
 long,err:=db.AddTask(TaskInput{Title:"Full proof sprint",Minutes:90,Priority:4});if err!=nil{t.Fatal(err)}
 _,err=db.PutRule("bounded",RuleChange{Text:"Use small focused blocks",Kind:"constraint",Enabled:true,Scope:"global",Effect:"max_minutes",Value:"30"})
 if err!=nil{t.Fatal(err)}
 day:=time.Now().UTC().AddDate(0,0,2).Format("2006-01-02")
 plan,err:=db.PlanWeek(day,180,9,nil);if err!=nil{t.Fatal(err)}
 found:=false;for _,item:=range plan.Unplanned{
  if item.TaskID==long.ID&&strings.Contains(item.Reason,"bounded"){found=true}
 }
 if !found{t.Fatalf("missing owner rule exclusion: %+v",plan.Unplanned)}
 if len(plan.Slots)!=1||plan.Slots[0].TaskID!=short.ID||len(plan.Slots[0].Rationale)==0{
  t.Fatalf("rule-aware plan omitted safe task: %+v",plan.Slots)
 }
 if len(db.Tasks("open"))!=2{t.Fatal("advisory policy mutated task completion")}
}

func TestWeeklyPlanExplicitPolicyConflict(t *testing.T){
 db,err:=Open(filepath.Join(t.TempDir(),"store.json"),"UTC");if err!=nil{t.Fatal(err)}
 if _,err=db.AddTask(TaskInput{Title:"Study",Minutes:20,Energy:"high"});err!=nil{t.Fatal(err)}
 for _,spec:=range []struct{key,value string}{{"high","high"},{"low","low"}}{
  if _,err=db.PutRule(spec.key,RuleChange{Text:"Reserve declared effort",Kind:"constraint",Enabled:true,
   Scope:"global",Effect:"require_energy",Value:spec.value});err!=nil{t.Fatal(err)}
 }
 _,err=db.PlanWeek(time.Now().UTC().AddDate(0,0,1).Format("2006-01-02"),180,9,nil)
 if !errors.Is(err,ErrRuleConflict){t.Fatalf("conflict was hidden: %v",err)}
}
