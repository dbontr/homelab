package motivation

import (
 "errors"
 "path/filepath"
 "testing"
)

func TestAttentionPlanVersionedOptInAndRestart(t *testing.T){
 path:=filepath.Join(t.TempDir(),"motivation.json")
 engine,err:=Open(path);if err!=nil{t.Fatal(err)}
 input:=AttentionPlanChange{
  Enabled:true,Trigger:"Opening distracting news",
  Alternative:"News feed",Context:"reading",
  Response:"Open the saved proof and solve one case",
  Boundary:"Deliberate leisure is permitted after the research block",
  Outcome:"One written derivation",TaskID:"task_123",
  CooldownMinutes:45,
 }
 state,err:=engine.PutAttentionPlan("news",input);if err!=nil{t.Fatal(err)}
 if len(state.AttentionPlans)!=1||state.AttentionPlans[0].Revision!=1{t.Fatal("plan not persisted")}
 if _,err=engine.PutAttentionPlan("news",input);err==nil{t.Fatal("stale revision accepted")}
 if _,err=engine.AttentionResponse(input.Trigger,input.Context);err==nil{t.Fatal("disabled motivation supplied response")}
 if err=engine.Close();err!=nil{t.Fatal(err)}
 reopened,err:=Open(path);if err!=nil{t.Fatal(err)}
 defer reopened.Close()
 if len(reopened.Snapshot().AttentionPlans)!=1{t.Fatal("plan lost after restart")}
 input.ExpectedRevision=1;input.Paused=true
 state,err=reopened.PutAttentionPlan("news",input);if err!=nil{t.Fatal(err)}
 if state.AttentionPlans[0].Revision!=2||!state.AttentionPlans[0].Paused{t.Fatal("paused plan not revisioned")}
 bad:=input;bad.ExpectedRevision=2;bad.Response="\x00"
 if _,err=reopened.PutAttentionPlan("news",bad);err==nil{t.Fatal("control character accepted")}
}

func TestAttentionPlanDisabledAndReferenceValidation(t *testing.T){
 path:=filepath.Join(t.TempDir(),"motivation.json")
 e,err:=Open(path);if err!=nil{t.Fatal(err)}
 defer e.Close()
 _,err=e.PutAttentionPlan("x",AttentionPlanChange{Enabled:true,Context:"study",CooldownMinutes:25})
 var invalid InvalidError
 if !errors.As(err,&invalid){t.Fatalf("missing required trigger accepted: %v",err)}
 _,err=e.PutAttentionPlan("x",AttentionPlanChange{Enabled:false,CooldownMinutes:25,GoalID:"../private"})
 if !errors.As(err,&invalid){t.Fatalf("unsafe cross-record reference accepted: %v",err)}
}
