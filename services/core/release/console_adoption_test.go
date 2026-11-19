package release

import (
 "context"
 "encoding/json"
 "os"
 "path/filepath"
 "strings"
 "testing"
 "time"
)

func adoptionFixture(t *testing.T) (*Controller,*fakeReleaseRunner,Journal) {
 t.Helper()
 runner:=&fakeReleaseRunner{}
 ctrl:=releaseTestController(t,runner)
 runner.current=builtSystem
 previous:=Journal{
  Phase:"active",ID:strings.Repeat("9",48),
  Target:priorSystem,Previous:"/nix/store/cccccccccccccccccccccccccccccccc-older-system",
  Time:time.Now().Add(-time.Hour).UTC(),
 }
 if err:=ctrl.locked(func()error{return ctrl.atomic("journal.json",previous)});err!=nil{t.Fatal(err)}
 return ctrl,runner,previous
}

func TestConsoleReconcilePreservesJournalAndNeverSwitches(t *testing.T){
 ctrl,runner,previous:=adoptionFixture(t)
 adoption,err:=ctrl.reconcileVerifiedConsole(context.Background(),testRevision,func()(string,error){return builtSystem,nil})
 if err!=nil{t.Fatal(err)}
 if adoption.Authority!="physical-root-console"||adoption.Revision!=testRevision||
  adoption.System!=builtSystem||adoption.PreviousJournal!=previous{
  t.Fatalf("unverifiable adoption: %+v",adoption)
 }
 status,err:=ctrl.Status()
 if err!=nil||status.Phase!="active"||status.Target!=builtSystem||
  status.Previous!=""||status.ID!=adoption.ID{
  t.Fatalf("invalid reconciled status: %+v, %v",status,err)
 }
 raw,err:=os.ReadFile(filepath.Join(ctrl.Config.StateDir,"console-adoption-"+adoption.ID+".json"))
 if err!=nil{t.Fatal(err)}
 var archived ConsoleAdoption
 if err:=json.Unmarshal(raw,&archived);err!=nil||archived!=adoption{
  t.Fatal("prior journal was not preserved")
 }
 for _,call:=range runner.calls{
  if strings.Contains(call,"/nix-env")||strings.Contains(call,"switch-to-configuration"){
   t.Fatalf("reconciliation changed a system generation: %s",call)
  }
 }
 if err:=ctrl.Revert(context.Background(),adoption.ID);err==nil{
  t.Fatal("adopted generation was incorrectly granted a rollback target")
 }
 if _,err:=ctrl.reconcileVerifiedConsole(context.Background(),testRevision,func()(string,error){return builtSystem,nil});err==nil{
  t.Fatal("duplicate adoption was accepted")
 }
}

func TestConsoleReconcileRejectsUnverifiedGeneration(t *testing.T){
 cases:=[]struct{
  name string
  modify func(*fakeReleaseRunner)
  running string
  revision string
 }{
  {"wrong-running-generation",nil,priorSystem,testRevision},
  {"wrong-source-revision",nil,builtSystem,strings.Repeat("2",40)},
  {"dirty-source",func(r *fakeReleaseRunner){r.dirty=true},builtSystem,testRevision},
  {"unhealthy-generation",func(r *fakeReleaseRunner){r.failHealth=true},builtSystem,testRevision},
 }
 for _,tc:=range cases{
  t.Run(tc.name,func(t *testing.T){
   ctrl,runner,previous:=adoptionFixture(t)
   if tc.modify!=nil{tc.modify(runner)}
   if _,err:=ctrl.reconcileVerifiedConsole(context.Background(),tc.revision,func()(string,error){return tc.running,nil});err==nil{
    t.Fatal("unverified generation was adopted")
   }
   status,err:=ctrl.Status()
   if err!=nil||status!=previous{t.Fatal("failed reconciliation altered journal")}
  })
 }
}

func TestConsoleReconcileRejectsUnsettledAndRemoteContext(t *testing.T){
 ctrl,_,previous:=adoptionFixture(t)
 if _,err:=ctrl.ReconcileConsole(context.Background(),testRevision,func()(string,error){return builtSystem,nil});err==nil{
  t.Fatal("untrusted controller accepted console authority")
 }
 previous.Phase="activating"
 if err:=ctrl.locked(func()error{return ctrl.atomic("journal.json",previous)});err!=nil{t.Fatal(err)}
 if _,err:=ctrl.reconcileVerifiedConsole(context.Background(),testRevision,func()(string,error){return builtSystem,nil});err==nil{
  t.Fatal("unsettled release journal was overwritten")
 }
}
