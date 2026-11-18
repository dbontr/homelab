package privileged

import (
 "context"
 "errors"
 "strings"
 "testing"
)

func TestDetachedReleaseDispatchUsesFixedUnitAndArguments(t *testing.T){
 id:=strings.Repeat("a",32)
 stage:=strings.Repeat("b",48)
 calls:=0
 d:=SystemdReleaseDispatcher{
  SystemdRun:"/nix/store/aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa-systemd/bin/systemd-run",
  Runner:"/nix/store/bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb-release/bin/homelab-release-runner",
  Run:func(_ context.Context,cmd string,args ...string)error{
   calls++
   if !strings.HasSuffix(cmd,"/systemd-run")||len(args)!=11{
    t.Fatalf("unexpected detached command: %s %v",cmd,args)
   }
   if args[0]!="--no-block"||args[1]!="--collect"||
    args[2]!="--unit=homelab-release-"+id||
    args[3]!="--property=Type=oneshot"||
    args[4]!="--property=TimeoutStartSec=20min"||
    args[5]!="--property=Restart=no"||
    args[6]!="--property=IgnoreOnIsolate=yes"||
    args[7]!="/nix/store/bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb-release/bin/homelab-release-runner"||
    args[8]!="guard-activate"||args[9]!=id||args[10]!=stage{
    t.Fatalf("wrong detached release arguments: %v",args)
   }
   return nil
  },
 }
 if err:=d.Dispatch(context.Background(),"infrastructure__release_activate",id,stage);err!=nil{t.Fatal(err)}
 if calls!=1{t.Fatalf("dispatch count %d",calls)}
}

func TestDetachedReleaseDispatchFailsClosed(t *testing.T){
 called:=0
 d:=SystemdReleaseDispatcher{SystemdRun:"/nix/store/systemd/bin/systemd-run",
  Runner:"/nix/store/release/bin/homelab-release-runner",
  Run:func(context.Context,string,...string)error{called++;return nil}}
 stage:=strings.Repeat("b",48)
 valid:=strings.Repeat("a",32)
 for _,tc:=range []struct{op,id,stage string}{
  {"shell.run",valid,stage},
  {"infrastructure__release_activate",strings.Repeat("A",32),stage},
  {"infrastructure__release_revert",valid,"bad"},
  {"infrastructure__release_activate",valid,stage+"extra"},
 } {
  if err:=d.Dispatch(context.Background(),tc.op,tc.id,tc.stage);err==nil{t.Fatalf("invalid detached dispatch admitted: %+v",tc)}
 }
 if called!=0{t.Fatal("invalid dispatch launched a privileged service")}
 d.Run=func(context.Context,string,...string)error{return errors.New("unit was not created")}
 if err:=d.Dispatch(context.Background(),"infrastructure__release_activate",valid,stage);err==nil{t.Fatal("systemd failure ignored")}
}
