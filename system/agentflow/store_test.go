package agentflow

import (
 "encoding/json"
 "errors"
 "os"
 "path/filepath"
 "testing"
 "time"
)

func testStore(t *testing.T)*Store{
 t.Helper()
 s,err:=Open(filepath.Join(t.TempDir(),"state"));if err!=nil{t.Fatal(err)}
 return s
}
func sample()Workflow{
 return Workflow{ID:"engineering-1",Project:"project-1",Steps:[]Step{
  {ID:"build",Kind:"workspace_command",Input:json.RawMessage(`{"command":"go build ./..."}`),MaxAttempts:2},
  {ID:"review",Kind:"workspace_command",Input:json.RawMessage(`{"command":"go test ./..."}`),DependsOn:[]string{"build"},MaxAttempts:1},
 }}
}
func TestRejectCyclesUnknownDependenciesAndInvalidInputs(t *testing.T){
 cases:=[]struct{name string;change func(*Workflow)}{
  {"cycle",func(w *Workflow){w.Steps[0].DependsOn=[]string{"review"}}},
  {"missing dependency",func(w *Workflow){w.Steps[1].DependsOn=[]string{"missing"}}},
  {"duplicate identifier",func(w *Workflow){w.Steps[1].ID="build"}},
  {"invalid input",func(w *Workflow){w.Steps[0].Input=[]byte("{")}},
  {"invalid attempts",func(w *Workflow){w.Steps[0].MaxAttempts=0}},
 }
 for _,tc:=range cases{t.Run(tc.name,func(t *testing.T){w:=sample();tc.change(&w);if err:=Validate(w);err==nil{t.Fatal("accepted invalid workflow")}})}
}
func TestDependencyProgressAndDurability(t *testing.T){
 s:=testStore(t)
 if err:=s.Create(sample());err!=nil{t.Fatal(err)}
 c,err:=s.ClaimReady("engineering-1",time.Minute);if err!=nil{t.Fatal(err)}
 if c.StepID!="build"{t.Fatalf("unexpected first step: %s",c.StepID)}
 if _,err:=s.ClaimReady("engineering-1",time.Minute);!errors.Is(err,os.ErrNotExist){t.Fatalf("expected blocked dependent step, got %v",err)}
 if err:=s.Complete(c,json.RawMessage(`{"ok":true}`),"");err!=nil{t.Fatal(err)}
 other,err:=Open(s.dir);if err!=nil{t.Fatal(err)}
 next,err:=other.ClaimReady("engineering-1",time.Minute);if err!=nil{t.Fatal(err)}
 if next.StepID!="review"{t.Fatalf("unexpected second step: %s",next.StepID)}
 if err:=other.Complete(next,json.RawMessage(`{"done":true}`),"");err!=nil{t.Fatal(err)}
 w,err:=other.Get("engineering-1");if err!=nil{t.Fatal(err)}
 if w.Steps[0].Status!=Succeeded||w.Steps[1].Status!=Succeeded{t.Fatalf("incomplete workflow: %+v",w.Steps)}
}
func TestTokenAndGenerationFence(t *testing.T){
 s:=testStore(t);if err:=s.Create(sample());err!=nil{t.Fatal(err)}
 clock:=time.Now();s.now=func()time.Time{return clock}
 first,err:=s.ClaimReady("engineering-1",time.Second);if err!=nil{t.Fatal(err)}
 forged:=first;forged.Token="aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
 if err:=s.Complete(forged,nil,"");err==nil{t.Fatal("accepted forged token")}
 clock=clock.Add(2*time.Second)
 replacement,err:=s.ClaimReady("engineering-1",time.Second);if err!=nil{t.Fatal(err)}
 if replacement.Generation<=first.Generation{t.Fatal("lease generation not incremented")}
 if err:=s.Complete(first,nil,"");err==nil{t.Fatal("accepted stale generation")}
 if err:=s.Complete(replacement,nil,"");err!=nil{t.Fatal(err)}
 if err:=s.Complete(replacement,nil,"");err==nil{t.Fatal("accepted replay")}
}
func TestRetryExhaustion(t *testing.T){
 s:=testStore(t);if err:=s.Create(sample());err!=nil{t.Fatal(err)}
 for i:=0;i<2;i++{
  c,err:=s.ClaimReady("engineering-1",time.Second);if err!=nil{t.Fatal(err)}
  if err:=s.Complete(c,nil,"failed");err!=nil{t.Fatal(err)}
 }
 if _,err:=s.ClaimReady("engineering-1",time.Second);!errors.Is(err,os.ErrNotExist){t.Fatalf("unexpected claim: %v",err)}
 w,err:=s.Get("engineering-1");if err!=nil{t.Fatal(err)}
 if w.Steps[0].Status!=Failed{t.Fatalf("expected terminal failure: %+v",w.Steps[0])}
}
func TestCancelRequiresExpectedRevision(t *testing.T){
 s:=testStore(t);if err:=s.Create(sample());err!=nil{t.Fatal(err)}
 if err:=s.Cancel("engineering-1",0);err==nil{t.Fatal("accepted stale revision")}
 if err:=s.Cancel("engineering-1",1);err!=nil{t.Fatal(err)}
 if _,err:=s.ClaimReady("engineering-1",time.Second);err==nil{t.Fatal("claimed cancelled workflow")}
 w,err:=s.Get("engineering-1");if err!=nil{t.Fatal(err)}
 if !w.Cancelled||w.Steps[0].Status!=Cancelled{t.Fatalf("not cancelled: %+v",w)}
}
