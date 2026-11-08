package release

import (
 "context"
 "errors"
 "os"
 "path/filepath"
 "strings"
 "testing"
)

const (
 testRevision="1111111111111111111111111111111111111111"
 priorSystem="/nix/store/aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa-previous-system"
 builtSystem="/nix/store/bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb-next-system"
)

type fakeReleaseRunner struct{
 calls []string
 current string
 failSwitch bool
 failHealth bool
 dirty bool
}

func(f *fakeReleaseRunner) run(_ context.Context,name string,args ...string)(string,error){
 cmd:=name+" "+strings.Join(args," ")
 f.calls=append(f.calls,cmd)
 switch{
 case strings.Contains(cmd,"rev-parse HEAD"):return testRevision+"\n",nil
 case strings.Contains(cmd,"status --porcelain"):
  if f.dirty{return " M flake.nix\n",nil}
  return "",nil
 case strings.HasSuffix(name,"/nix"):
  if strings.Contains(cmd,"--offline")&&strings.Contains(cmd,"--no-link"){return builtSystem+"\n",nil}
  return "",errors.New("offline build options missing")
 case strings.Contains(cmd,"switch-to-configuration"):
  if f.failSwitch && strings.HasPrefix(name,builtSystem){return "",errors.New("switch failed")}
  return "",nil
 case strings.HasSuffix(name,"/nix-env"):
  if len(args)>0{f.current=args[len(args)-1]}
  return "",nil
 case strings.HasSuffix(name,"/systemctl"):
  if f.failHealth{return "failed",errors.New("service down")}
  return "active\n",nil
 default:return "",nil
 }
}

func releaseTestController(t *testing.T,runner *fakeReleaseRunner)*Controller{
 t.Helper()
 runner.current=priorSystem
 root:=t.TempDir()
 if err:=os.Chmod(root,0700);err!=nil{t.Fatal(err)}
 state:=filepath.Join(root,"release")
 c,err:=New(Config{
  Host:"jupiter",FlakeDir:root,StateDir:state,
  Nix:"/usr/bin/nix",NixEnv:"/usr/bin/nix-env",
  Git:"/usr/bin/git",Systemctl:"/usr/bin/systemctl",
  HealthServices:[]string{"homelab-platform.service"},
  Run:runner.run,
  Current:func()(string,error){return runner.current,nil},
 })
 if err!=nil{t.Fatal(err)}
 return c
}

func TestReleaseStagesOnlyCleanExactOfflineGeneration(t *testing.T){
 runner:=&fakeReleaseRunner{}
 ctrl:=releaseTestController(t,runner)
 if _,err:=ctrl.Stage(context.Background(),"main");err==nil{t.Fatal("mutable branch accepted")}
 runner.dirty=true
 if _,err:=ctrl.Stage(context.Background(),testRevision);err==nil{t.Fatal("dirty checkout accepted")}
 runner.dirty=false
 stage,err:=ctrl.Stage(context.Background(),testRevision)
 if err!=nil{t.Fatal(err)}
 if stage.Host!="jupiter"||stage.System!=builtSystem||len(stage.ID)!=48{t.Fatal("bad staged manifest")}
 if _,err:=ctrl.loadStage("../evil");err==nil{t.Fatal("path traversal accepted")}
}

func TestReleaseActivationRecordsAndChecksHealth(t *testing.T){
 runner:=&fakeReleaseRunner{}
 ctrl:=releaseTestController(t,runner)
 stage,err:=ctrl.Stage(context.Background(),testRevision)
 if err!=nil{t.Fatal(err)}
 if err:=ctrl.Activate(context.Background(),stage.ID);err!=nil{t.Fatal(err)}
 journal,err:=ctrl.Status()
 if err!=nil||journal.Phase!="active"||journal.Target!=builtSystem||journal.Previous!=priorSystem{
  t.Fatalf("wrong durable activation journal: %+v %v",journal,err)
 }
 if err:=ctrl.Activate(context.Background(),stage.ID);err==nil{t.Fatal("same-generation activation repeated")}
}

func TestReleaseFailureRestoresPreviousGeneration(t *testing.T){
 runner:=&fakeReleaseRunner{}
 ctrl:=releaseTestController(t,runner)
 staged,err:=ctrl.Stage(context.Background(),testRevision)
 if err!=nil{t.Fatal(err)}
 runner.failSwitch=true
 err=ctrl.Activate(context.Background(),staged.ID)
 if err==nil||!strings.Contains(err.Error(),"restored"){t.Fatalf("rollback not reported: %v",err)}
 journal,err:=ctrl.Status()
 if err!=nil||journal.Phase!="rolled-back"{t.Fatalf("rollback journal invalid: %+v %v",journal,err)}
 restored:=false
 for _,call:=range runner.calls{
  if strings.Contains(call,"--set "+priorSystem){restored=true}
 }
 if !restored{t.Fatal("previous generation was never restored")}
}

func TestReleaseRejectsMissingHealthAndFailedRollbackJournal(t *testing.T){
 runner:=&fakeReleaseRunner{}
 ctrl:=releaseTestController(t,runner)
 if _,err:=New(Config{Host:"jupiter",FlakeDir:ctrl.Config.FlakeDir,StateDir:ctrl.Config.StateDir,Nix:"/usr/bin/nix",NixEnv:"/usr/bin/nix-env",Git:"/usr/bin/git",Systemctl:"/usr/bin/systemctl",Run:runner.run,Current:ctrl.Config.Current});err==nil{
  t.Fatal("missing health checks accepted")
 }
 staged,err:=ctrl.Stage(context.Background(),testRevision)
 if err!=nil{t.Fatal(err)}
 if err:=ctrl.atomic("journal.json",Journal{Phase:"rollback-failed",ID:staged.ID,Previous:priorSystem,Target:builtSystem});err!=nil{t.Fatal(err)}
 if _,err:=ctrl.Stage(context.Background(),testRevision);err==nil{t.Fatal("staging while rollback failed")}
 if err:=ctrl.Activate(context.Background(),staged.ID);err==nil{t.Fatal("activation while rollback failed")}
 if err:=ctrl.Recover(context.Background());err!=nil{t.Fatal(err)}
}

func TestInterruptedGenerationRequiresRecovery(t *testing.T){
 runner:=&fakeReleaseRunner{}
 ctrl:=releaseTestController(t,runner)
 staged,err:=ctrl.Stage(context.Background(),testRevision)
 if err!=nil{t.Fatal(err)}
 if err:=ctrl.atomic("journal.json",Journal{Phase:"activating",ID:staged.ID,Previous:priorSystem,Target:builtSystem});err!=nil{t.Fatal(err)}
 if err:=ctrl.Activate(context.Background(),staged.ID);err==nil{t.Fatal("interrupted generation overwritten")}
 if err:=ctrl.Recover(context.Background());err!=nil{t.Fatal(err)}
 journal,err:=ctrl.Status()
 if err!=nil||journal.Phase!="rolled-back"{t.Fatal("interrupted release not recovered")}
}
