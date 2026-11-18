package release

import (
 "context"
 "errors"
 "strings"
 "testing"
 "time"
)

func interruptedRelease(t *testing.T)(*Controller,*fakeReleaseRunner,string) {
 t.Helper()
 runner:=&fakeReleaseRunner{}
 ctrl:=releaseTestController(t,runner)
 stage,err:=ctrl.Stage(context.Background(),testRevision)
 if err!=nil{t.Fatal(err)}
 txn:=Journal{Phase:"activating",ID:stage.ID,Previous:priorSystem,Target:builtSystem,Time:time.Now().UTC()}
 if err:=ctrl.atomic("attempt-"+stage.ID+".json",txn);err!=nil{t.Fatal(err)}
 if err:=ctrl.atomic("journal.json",txn);err!=nil{t.Fatal(err)}
 runner.current=builtSystem
 return ctrl,runner,stage.ID
}

func TestInterruptedReleaseCompletesFromIndependentRunner(t *testing.T) {
 ctrl,runner,id:=interruptedRelease(t)
 if err:=ctrl.Continue(context.Background(),"activate",id);err!=nil{t.Fatal(err)}
 state,err:=ctrl.Status()
 if err!=nil || state.Phase!="active" || runner.current!=builtSystem{t.Fatalf("not active: %+v %v",state,err)}
 if err:=ctrl.Continue(context.Background(),"activate",id);err==nil{t.Fatal("completed release replayed")}
}

func TestIndependentReleaseRunnerRestoresUnhealthyGeneration(t *testing.T) {
 ctrl,runner,id:=interruptedRelease(t)
 baseRun:=runner.run
 ctrl.Config.Run=func(ctx context.Context,cmd string,args ...string)(string,error){
  if strings.HasSuffix(cmd,"/systemctl") && runner.current==builtSystem {
   return "failed",errors.New("target unavailable")
  }
  return baseRun(ctx,cmd,args...)
 }
 if err:=ctrl.Continue(context.Background(),"activate",id);err==nil{t.Fatal("unhealthy release accepted")}
 state,err:=ctrl.Status()
 if err!=nil||state.Phase!="rolled-back"||runner.current!=priorSystem{t.Fatalf("rollback not verified: %+v %v",state,err)}
}

func TestIndependentReleaseRunnerRejectsInconsistentAttempt(t *testing.T) {
 ctrl,runner,id:=interruptedRelease(t)
 state,err:=ctrl.Status()
 if err!=nil{t.Fatal(err)}
 state.Previous="/nix/store/cccccccccccccccccccccccccccccccc-other"
 if err:=ctrl.atomic("journal.json",state);err!=nil{t.Fatal(err)}
 count:=len(runner.calls)
 if err:=ctrl.Continue(context.Background(),"activate",id);err==nil{t.Fatal("inconsistent journal accepted")}
 if len(runner.calls)!=count{t.Fatal("unexpected host mutation")}
}

func TestIndependentReleaseRunnerCompletesRevert(t *testing.T) {
 ctrl,runner,id:=interruptedRelease(t)
 state,err:=ctrl.Status()
 if err!=nil{t.Fatal(err)}
 state.Phase="reverting"
 if err:=ctrl.atomic("journal.json",state);err!=nil{t.Fatal(err)}
 if err:=ctrl.Continue(context.Background(),"revert",id);err!=nil{t.Fatal(err)}
 state,err=ctrl.Status()
 if err!=nil||state.Phase!="rolled-back"||runner.current!=priorSystem{t.Fatalf("reversion not verified: %+v %v",state,err)}
}
