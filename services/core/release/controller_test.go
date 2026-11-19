package release

import (
 "context"
 "errors"
 "net"
 "os"
 "os/exec"
 "path/filepath"
 "strings"
 "testing"
 "time"
)

func TestExecUsesPrivateWritableCache(t *testing.T){
 shell,err:=exec.LookPath("sh")
 if err!=nil{t.Skip("shell is unavailable")}
 output,err:=Exec(context.Background(),shell,"-c",
  `test "$HOME" = /var/empty && test -d "$XDG_CACHE_HOME" && printf probe > "$XDG_CACHE_HOME/probe" && printf %s "$XDG_CACHE_HOME"`)
 if err!=nil{t.Fatalf("release command could not use its private cache: %v",err)}
 cache:=strings.TrimSpace(output)
 if !filepath.IsAbs(cache){t.Fatalf("cache path is not absolute: %q",cache)}
 if _,err:=os.Stat(cache);!errors.Is(err,os.ErrNotExist){
  t.Fatalf("private cache was not removed: %q, %v",cache,err)
 }
}

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
 transientHealthFailures int
 dirty bool
}

func(f *fakeReleaseRunner) run(_ context.Context,name string,args ...string)(string,error){
 cmd:=name+" "+strings.Join(args," ")
 f.calls=append(f.calls,cmd)
 switch{
 case strings.Contains(cmd,"rev-parse --verify"):return testRevision+"\n",nil
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
  if f.transientHealthFailures>0{f.transientHealthFailures--;return "activating",errors.New("starting")}
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
 pinned:=false
 for _,call:=range runner.calls {
  if strings.Contains(call,"build --offline") {
   pinned=strings.Contains(call,"git+file://")&&
    strings.Contains(call,"?rev="+testRevision+"#nixosConfigurations.jupiter.config.system.build.toplevel")
   if !pinned{t.Fatalf("Nix build used mutable source: %s",call)}
  }
 }
 if !pinned{t.Fatal("exact-commit Nix reference not observed")}
 if _,err:=ctrl.loadStage("../evil");err==nil{t.Fatal("path traversal accepted")}
}

func TestStageVerificationRequiresMatchingAuthorityReceipt(t *testing.T){
 runner:=&fakeReleaseRunner{}
 ctrl:=releaseTestController(t,runner)
 stage,err:=ctrl.Stage(context.Background(),testRevision)
 if err!=nil{t.Fatal(err)}
 if err:=ctrl.VerifyStaged(stage.ID);err!=nil{t.Fatalf("valid stage rejected: %v",err)}
 receipt:="stage-ready-"+stage.ID+".json"
 if err:=os.Remove(filepath.Join(ctrl.Config.StateDir,receipt));err!=nil{t.Fatal(err)}
 if err:=ctrl.VerifyStaged(stage.ID);err==nil||!strings.Contains(err.Error(),"authority receipt"){
  t.Fatalf("stage without authority receipt passed verification: %v",err)
 }
 mismatched:=stage
 mismatched.System=priorSystem
 if err:=ctrl.atomic(receipt,mismatched);err!=nil{t.Fatal(err)}
 if err:=ctrl.VerifyStaged(stage.ID);err==nil||!strings.Contains(err.Error(),"differs"){
  t.Fatalf("mismatched authority receipt passed verification: %v",err)
 }
}

func TestAuditedStageRequalificationRequiresExactNixBuild(t *testing.T){
 runner:=&fakeReleaseRunner{}
 ctrl:=releaseTestController(t,runner)
 stage,err:=ctrl.Stage(context.Background(),testRevision)
 if err!=nil{t.Fatal(err)}
 if err:=ctrl.verifyImmutableStageBuild(context.Background(),stage);err!=nil{
  t.Fatalf("immutable signed source rejected: %v",err)
 }
 stage.System=priorSystem
 if err:=ctrl.verifyImmutableStageBuild(context.Background(),stage);err==nil||
  !strings.Contains(err.Error(),"does not match rebuilt"){
  t.Fatalf("mismatched staged system accepted: %v",err)
 }
}

func TestUntrustedContinuationCannotRequalifyLegacyStage(t *testing.T){
 runner:=&fakeReleaseRunner{}
 ctrl:=releaseTestController(t,runner)
 stage,err:=ctrl.Stage(context.Background(),testRevision)
 if err!=nil{t.Fatal(err)}
 if err:=os.Remove(filepath.Join(ctrl.Config.StateDir,"stage-ready-"+stage.ID+".json"));err!=nil{t.Fatal(err)}
 txn:=Journal{ID:stage.ID,Phase:"activating",Previous:priorSystem,Target:builtSystem,Time:time.Now().UTC()}
 if err:=ctrl.atomic("journal.json",txn);err!=nil{t.Fatal(err)}
 if err:=ctrl.ContinueAudited(context.Background(),"activate",stage.ID);err==nil||
  !strings.Contains(err.Error(),"authority receipt"){
  t.Fatalf("untrusted staged handoff accepted: %v",err)
 }
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

func TestReleaseWaitsForDependentServicesBeforeAdmission(t *testing.T){
 runner:=&fakeReleaseRunner{}
 ctrl:=releaseTestController(t,runner)
 stage,err:=ctrl.Stage(context.Background(),testRevision)
 if err!=nil{t.Fatal(err)}
 runner.transientHealthFailures=2
 if err:=ctrl.Activate(context.Background(),stage.ID);err!=nil{t.Fatalf("startup delay triggered release failure: %v",err)}
 if runner.transientHealthFailures!=0{t.Fatal("not all readiness attempts were consumed")}
 journal,err:=ctrl.Status()
 if err!=nil||journal.Phase!="active"{t.Fatalf("startup failed release: %+v %v",journal,err)}
}

func TestReleaseReadinessDeadlineRejectsUnhealthyService(t *testing.T){
 runner:=&fakeReleaseRunner{failHealth:true}
 ctrl:=releaseTestController(t,runner)
 ctx,cancel:=context.WithTimeout(context.Background(),25*time.Millisecond)
 defer cancel()
 if err:=ctrl.waitForHealth(ctx);err==nil||!strings.Contains(err.Error(),"did not become ready"){
  t.Fatalf("missing readiness failure at deadline: %v",err)
 }
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

func TestExplicitReleaseRevertAndReplayDenial(t *testing.T) {
 runner:=&fakeReleaseRunner{}
 ctrl:=releaseTestController(t,runner)
 staged,err:=ctrl.Stage(context.Background(),testRevision)
 if err!=nil{t.Fatal(err)}
 if err:=ctrl.Activate(context.Background(),staged.ID);err!=nil{t.Fatal(err)}
 if err:=ctrl.Revert(context.Background(),strings.Repeat("f",48));err==nil{t.Fatal("wrong staged release accepted")}
 if err:=ctrl.Revert(context.Background(),staged.ID);err!=nil{t.Fatal(err)}
 if runner.current!=priorSystem{t.Fatal("prior generation not restored")}
 journal,err:=ctrl.Status()
 if err!=nil||journal.Phase!="rolled-back"{t.Fatalf("invalid rollback journal: %+v %v",journal,err)}
 if err:=ctrl.Revert(context.Background(),staged.ID);err==nil{t.Fatal("replayed rollback accepted")}
}

func TestExplicitRevertRequiresExactActiveGeneration(t *testing.T) {
 runner:=&fakeReleaseRunner{}
 ctrl:=releaseTestController(t,runner)
 staged,err:=ctrl.Stage(context.Background(),testRevision)
 if err!=nil{t.Fatal(err)}
 if err:=ctrl.Activate(context.Background(),staged.ID);err!=nil{t.Fatal(err)}
 runner.current="/nix/store/cccccccccccccccccccccccccccccccc-out-of-band"
 if err:=ctrl.Revert(context.Background(),staged.ID);err==nil{t.Fatal("out-of-band generation overwritten")}
}

func TestRecoveryAlreadyRestoredAvoidsAnotherSwitch(t *testing.T){
 runner:=&fakeReleaseRunner{}
 ctrl:=releaseTestController(t,runner)
 staged,err:=ctrl.Stage(context.Background(),testRevision);if err!=nil{t.Fatal(err)}
 if err:=ctrl.atomic("journal.json",Journal{Phase:"rollback-failed",ID:staged.ID,Previous:priorSystem,Target:builtSystem});err!=nil{t.Fatal(err)}
 start:=len(runner.calls)
 if err:=ctrl.Recover(context.Background());err!=nil{t.Fatal(err)}
 for _,call:=range runner.calls[start:]{
  if strings.Contains(call,"switch-to-configuration")||strings.Contains(call,"/nix-env"){
   t.Fatalf("already restored system was switched again: %s",call)
  }
 }
 journal,err:=ctrl.Status();if err!=nil||journal.Phase!="rolled-back"{t.Fatalf("recovery not recorded: %+v %v",journal,err)}
}

func TestRecoveryAlreadyRestoredStillRequiresHealth(t *testing.T){
 runner:=&fakeReleaseRunner{failHealth:true}
 ctrl:=releaseTestController(t,runner)
 staged,err:=ctrl.Stage(context.Background(),testRevision);if err!=nil{t.Fatal(err)}
 if err:=ctrl.atomic("journal.json",Journal{Phase:"rollback-failed",ID:staged.ID,Previous:priorSystem,Target:builtSystem});err!=nil{t.Fatal(err)}
 if err:=ctrl.Recover(context.Background());err==nil{t.Fatal("unhealthy runtime accepted as recovered")}
 journal,err:=ctrl.Status();if err!=nil||journal.Phase!="rollback-failed"{t.Fatalf("failed recovery changed journal: %+v %v",journal,err)}
}

func TestCriticalReleaseHealthProbesAreReadOnlyAndValidated(t *testing.T){
 cfg:=releaseTestController(t,&fakeReleaseRunner{}).Config
 cfg.HealthServices=[]string{
  "sshd.service",
  "systemd-networkd.service",
  "dnsmasq.service",
  "cloudflared-saturn.service",
  "k3s.service",
  "homelab-mcp.service",
 }
 if _,err:=New(cfg);err!=nil{t.Fatalf("critical health checks denied: %v",err)}
 for _,unit:=range []string{"","sshd","sshd.socket","../sshd.service","ssh d.service","-sshd.service",strings.Repeat("s",129)+".service"}{
  cfg.HealthServices=[]string{unit}
  if _,err:=New(cfg);err==nil{t.Fatalf("invalid unit accepted: %q",unit)}
 }
}

func TestPrivateReleaseReadinessSocketChecks(t *testing.T){
 runner:=&fakeReleaseRunner{}
 ctrl:=releaseTestController(t,runner)
 for _,bad:=range []string{"0.0.0.0:22","8.8.8.8:53","example.com:443","10.77.30.1:0"}{
  copy:=ctrl.Config
  copy.HealthSockets=[]string{bad}
  if _,err:=New(copy);err==nil{t.Fatalf("unsafe health socket accepted: %s",bad)}
 }
 copy:=ctrl.Config
 copy.HealthSockets=[]string{"127.0.0.1:8104","127.0.0.1:8104"}
 if _,err:=New(copy);err==nil{t.Fatal("duplicate health sockets accepted")}
 listener,err:=net.Listen("tcp","127.0.0.1:0")
 if err!=nil{t.Fatal(err)}
 copy.HealthSockets=[]string{listener.Addr().String()}
 protected,err:=New(copy)
 if err!=nil{t.Fatal(err)}
 if err:=protected.health(context.Background());err!=nil{t.Fatal("ready private socket denied:",err)}
 if err:=listener.Close();err!=nil{t.Fatal(err)}
 if err:=protected.health(context.Background());err==nil{
  t.Fatal("closed protected service socket passed readiness")
 }
}

func TestReleaseStageAttemptCannotReplayAcrossOtherDeployments(t *testing.T) {
 runner:=&fakeReleaseRunner{}
 ctrl:=releaseTestController(t,runner)
 first,err:=ctrl.Stage(context.Background(),testRevision);if err!=nil{t.Fatal(err)}
 if err:=ctrl.Activate(context.Background(),first.ID);err!=nil{t.Fatal(err)}
 if err:=ctrl.Revert(context.Background(),first.ID);err!=nil{t.Fatal(err)}
 second,err:=ctrl.Stage(context.Background(),testRevision);if err!=nil{t.Fatal(err)}
 if err:=ctrl.Activate(context.Background(),second.ID);err!=nil{t.Fatal(err)}
 if err:=ctrl.Revert(context.Background(),second.ID);err!=nil{t.Fatal(err)}
 restarted,err:=New(ctrl.Config);if err!=nil{t.Fatal(err)}
 count:=len(runner.calls)
 if err:=restarted.Activate(context.Background(),first.ID);err==nil||!strings.Contains(err.Error(),"already attempted"){
   t.Fatalf("reused stage passed durable attempt fencing: %v",err)
 }
 if len(runner.calls)!=count{t.Fatal("replayed stage initiated a host operation")}
}

func TestGoReleaseRejectsStageWithoutMatchingAuthorityReceipt(t *testing.T) {
 runner:=&fakeReleaseRunner{}
 ctrl:=releaseTestController(t,runner)
 stage,err:=ctrl.Stage(context.Background(),testRevision);if err!=nil{t.Fatal(err)}
 if err:=os.Remove(filepath.Join(ctrl.Config.StateDir,"stage-ready-"+stage.ID+".json"));err!=nil{t.Fatal(err)}
 if err:=ctrl.Activate(context.Background(),stage.ID);err==nil||!strings.Contains(err.Error(),"authority receipt"){
  t.Fatalf("legacy or corrupt stage admitted: %v",err)
 }
}

func TestSecurityCutoverRunsAfterDurableAttemptAndBeforeSwitch(t *testing.T){
 runner:=&fakeReleaseRunner{}
 ctrl:=releaseTestController(t,runner)
 staged,err:=ctrl.Stage(context.Background(),testRevision)
 if err!=nil{t.Fatal(err)}
 calls:=[]string{}
 ctrl.Config.BeforeSwitch=func(_ context.Context,previous,target string)error{
  if previous!=priorSystem||target!=builtSystem{t.Fatal("unexpected generation")}
  var txn Journal
  if err:=ctrl.read("attempt-"+staged.ID+".json",&txn);err!=nil||txn.Phase!="activating" {
   t.Fatalf("security preparation preceded durable release attempt: %+v %v",txn,err)
  }
  calls=append(calls,"prepare")
  return nil
 }
 ctrl.Config.BeforeRollback=func(_ context.Context,target,previous string)error{
  calls=append(calls,"recover")
  if target!=builtSystem||previous!=priorSystem{t.Fatal("wrong rollback generations")}
  return nil
 }
 original:=ctrl.Config.Run
 ctrl.Config.Run=func(ctx context.Context,name string,args ...string)(string,error){
  if strings.HasSuffix(name,"/nix-env"){
   if len(calls)!=1||calls[0]!="prepare"{t.Fatal("generation switched before security preparation")}
  }
  return original(ctx,name,args...)
 }
 if err:=ctrl.Activate(context.Background(),staged.ID);err!=nil{t.Fatal(err)}
 if len(calls)!=1||calls[0]!="prepare"{t.Fatalf("unexpected security lifecycle: %v",calls)}
}

func TestSecurityPreparationFailureRestoresHealthyPreviousGeneration(t *testing.T){
 runner:=&fakeReleaseRunner{}
 ctrl:=releaseTestController(t,runner)
 staged,err:=ctrl.Stage(context.Background(),testRevision)
 if err!=nil{t.Fatal(err)}
 ctrl.Config.BeforeSwitch=func(context.Context,string,string)error{return errors.New("state preflight failed")}
 recovered:=false
 ctrl.Config.BeforeRollback=func(context.Context,string,string)error{recovered=true;return nil}
 err=ctrl.Activate(context.Background(),staged.ID)
 if err==nil||!strings.Contains(err.Error(),"state preflight failed"){t.Fatalf("failed preparation accepted: %v",err)}
 journal,jerr:=ctrl.Status()
 if jerr!=nil||journal.Phase!="rolled-back"||recovered||runner.current!=priorSystem {
  t.Fatalf("failed preparation did not recover: %+v, %v, %t",journal,jerr,recovered)
 }
}

func TestRollbackSecurityPreparationFailurePreventsUnsafeGenerationRestore(t *testing.T){
 runner:=&fakeReleaseRunner{}
 ctrl:=releaseTestController(t,runner)
 staged,err:=ctrl.Stage(context.Background(),testRevision)
 if err!=nil{t.Fatal(err)}
 ctrl.Config.BeforeSwitch=func(context.Context,string,string)error{return nil}
 runner.failHealth=true
 ctrl.Config.BeforeRollback=func(context.Context,string,string)error{return errors.New("parent-bound rollback receipt unavailable")}
 if err:=ctrl.Activate(context.Background(),staged.ID);err==nil{t.Fatal("failed rollback reported success")}
 journal,err:=ctrl.Status()
 if err!=nil||journal.Phase!="rollback-failed"||runner.current!=builtSystem {
  t.Fatalf("unsafe rollback behavior: %+v, %v",journal,err)
 }
}

func TestUncertainPreparationAfterProfileChangeRequiresProtectedRollback(t *testing.T){
 runner:=&fakeReleaseRunner{}
 ctrl:=releaseTestController(t,runner)
 staged,err:=ctrl.Stage(context.Background(),testRevision)
 if err!=nil{t.Fatal(err)}
 ctrl.Config.BeforeSwitch=func(context.Context,string,string)error{
  runner.current=builtSystem
  return errors.New("transition interrupted after profile selection")
 }
 rollbackPrepared:=false
 ctrl.Config.BeforeRollback=func(context.Context,string,string)error{
  rollbackPrepared=true
  return nil
 }
 if err:=ctrl.Activate(context.Background(),staged.ID);err==nil{
  t.Fatal("incomplete authority switch was admitted")
 }
 journal,err:=ctrl.Status()
 if err!=nil||journal.Phase!="rolled-back"||runner.current!=priorSystem||!rollbackPrepared{
  t.Fatalf("unverified authority generation was retained: %+v %v",journal,err)
 }
}
