package motivation

import (
 "errors"
 "path/filepath"
 "testing"
)

func TestRecoveryAnchorRevisionRestartPauseAndClear(t *testing.T){
 path:=filepath.Join(t.TempDir(),"motivation.json")
 e,err:=Open(path);if err!=nil{t.Fatal(err)}
 input:=RecoveryChange{NextAction:"Prove the remaining implication",ArtifactRef:"project:proof:42",Draft:"Case A holds",Context:"Stopped after finding a counterexample",TaskID:"task_24"}
 next,err:=e.SaveRecovery(input);if err!=nil{t.Fatal(err)}
 if !next.Recovery.Active||next.Recovery.Revision!=1{t.Fatal("recovery not persisted")}
 if _,err=e.SaveRecovery(input);err==nil{t.Fatal("stale revision accepted")}
 if _,err=e.Pause();err!=nil{t.Fatal(err)}
 if anchor:=e.Recovery();!anchor.Active||anchor.NextAction!=input.NextAction{t.Fatal("manual recovery was erased by pause")}
 if err=e.Close();err!=nil{t.Fatal(err)}
 reopened,err:=Open(path);if err!=nil{t.Fatal(err)}
 defer reopened.Close()
 if anchor:=reopened.Recovery();anchor.ArtifactRef!=input.ArtifactRef||anchor.Draft!=input.Draft{t.Fatal("anchor lost on restart")}
 if _,err=reopened.ClearRecovery(0);err==nil{t.Fatal("stale clear accepted")}
 state,err:=reopened.ClearRecovery(1);if err!=nil{t.Fatal(err)}
 if state.Recovery.Active||state.Recovery.Draft!=""||state.Recovery.Revision!=2{t.Fatal("clear leaked recovery payload")}
 _,err=reopened.ClearRecovery(1)
 var conflict ConflictError
 if !errors.As(err,&conflict){t.Fatalf("stale clear did not conflict: %v",err)}
}

func TestRecoveryAnchorValidationAndPrivateBounds(t *testing.T){
 e,err:=Open(filepath.Join(t.TempDir(),"motivation.json"));if err!=nil{t.Fatal(err)}
 defer e.Close()
 for _,c:=range []RecoveryChange{
  {NextAction:""},
  {NextAction:"Continue",ArtifactRef:string(make([]byte,513))},
  {NextAction:"Continue",GoalID:"../../escape"},
 }{
  if _,err=e.SaveRecovery(c);err==nil{t.Fatalf("invalid recovery was accepted: %+v",c)}
 }
}
