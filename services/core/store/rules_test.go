package store

import (
 "errors"
 "path/filepath"
 "testing"
 "time"
)

func TestOwnerRuleRevisionPersistenceAndAdvisoryRecommendation(t *testing.T){
 path:=filepath.Join(t.TempDir(),"state.json")
 db,err:=Open(path,"UTC");if err!=nil{t.Fatal(err)}
 goal,err:=db.AddGoal("Understand proof systems","");if err!=nil{t.Fatal(err)}
 small,err:=db.AddTask(TaskInput{Title:"Prove the first lemma",GoalID:goal.ID,Minutes:20,Energy:"medium",Priority:3})
 if err!=nil{t.Fatal(err)}
 large,err:=db.AddTask(TaskInput{Title:"Study the whole chapter",GoalID:goal.ID,Minutes:90,Energy:"high",Priority:4})
 if err!=nil{t.Fatal(err)}
 input:=RuleChange{ExpectedRevision:0,Text:"Prefer bounded study blocks",Kind:"constraint",Enabled:true,Priority:5,Scope:"goal",GoalID:goal.ID,Effect:"max_minutes",Value:"30"}
 rule,err:=db.PutRule("study-block",input);if err!=nil{t.Fatal(err)}
 if rule.Revision!=1{t.Fatal("missing initial revision")}
 if _,err=db.PutRule("study-block",input);!errors.Is(err,ErrRuleRevision){t.Fatalf("stale write: %v",err)}
 got,err:=db.Recommend(120,"","","",10);if err!=nil{t.Fatal(err)}
 if len(got.Recommendations)!=1||got.Recommendations[0].Task.ID!=small.ID||
  len(got.Excluded)!=1||got.Excluded[0].TaskID!=large.ID||len(got.Recommendations[0].Rationale)==0{
  t.Fatalf("rule constraint or rationale missing: %+v",got)
 }
 restarted,err:=Open(path,"UTC");if err!=nil{t.Fatal(err)}
 after,err:=restarted.Recommend(120,"","","",10);if err!=nil{t.Fatal(err)}
 if len(after.Recommendations)!=1||after.Recommendations[0].Task.ID!=small.ID{t.Fatal("policy not durable")}
 input.ExpectedRevision=rule.Revision;input.Paused=true
 paused,err:=restarted.PutRule(rule.Key,input);if err!=nil{t.Fatal(err)}
 after,err=restarted.Recommend(120,"","","",10);if err!=nil{t.Fatal(err)}
 if len(after.Recommendations)!=2{t.Fatal("paused policy still excludes work")}
 input.ExpectedRevision=paused.Revision;input.Paused=false;input.Exceptions=[]string{large.ID}
 if _,err:=restarted.PutRule(rule.Key,input);err!=nil{t.Fatal(err)}
 after,err=restarted.Recommend(120,"","","",10);if err!=nil{t.Fatal(err)}
 if len(after.Recommendations)!=2{t.Fatal("owner exception not honored")}
 input.ExpectedRevision++
 input.Exceptions=nil
 input.ExpiresAt=time.Now().UTC().Add(-time.Minute).Format(time.RFC3339)
 if _,err:=restarted.PutRule(rule.Key,input);err!=nil{t.Fatal(err)}
 after,err=restarted.Recommend(120,"","","",10);if err!=nil{t.Fatal(err)}
 if len(after.Recommendations)!=2{t.Fatal("expired rule remained active")}
}

func TestRuleConflictsFailClosedAndInvalidSemantics(t *testing.T){
 db,err:=Open(filepath.Join(t.TempDir(),"state.json"),"UTC");if err!=nil{t.Fatal(err)}
 if _,err=db.AddTask(TaskInput{Title:"Research",Minutes:25,Energy:"high"});err!=nil{t.Fatal(err)}
 a:=RuleChange{Text:"Avoid high energy",Kind:"constraint",Enabled:true,Scope:"global",Effect:"exclude_energy",Value:"high"}
 if _,err=db.PutRule("a",a);err!=nil{t.Fatal(err)}
 b:=a;b.Text="Avoid low energy";b.Value="low"
 if _,err=db.PutRule("b",b);err!=nil{t.Fatal(err)}
 if _,err=db.Recommend(60,"","","",3);!errors.Is(err,ErrRuleConflict){t.Fatalf("expected explicit conflict, got %v",err)}
 b.Enabled=false;b.ExpectedRevision=1
 if _,err=db.PutRule("b",b);err!=nil{t.Fatal(err)}
 result,err:=db.Recommend(60,"","","",3);if err!=nil{t.Fatal(err)}
 if len(result.Recommendations)!=0||len(result.Excluded)!=1{t.Fatalf("constraint not applied: %+v",result)}
 invalid:=a;invalid.Value="extreme"
 if _,err=db.PutRule("invalid",invalid);err==nil{t.Fatal("invalid typed value accepted")}
}
