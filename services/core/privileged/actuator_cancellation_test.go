package privileged

import (
 "context"
 "errors"
 "os"
 "path/filepath"
 "testing"
 "time"
)

// A cancelled administrative call must not leave an inherited child process
// free to perform an action after its owner request has ended.
func TestCancelledActuatorStopsInheritedChild(t *testing.T) {
 dir:=t.TempDir()
 started:=filepath.Join(dir,"started")
 survived:=filepath.Join(dir,"survived")
 runner:=filepath.Join(dir,"runner")
 script:="#!/bin/sh\n(sleep 0.45; echo late > '"+survived+"') &\necho ready > '"+started+"'\nwait\n"
 if err:=os.WriteFile(runner,[]byte(script),0700);err!=nil{t.Fatal(err)}
 cfg:=RustActuatorConfig{
  Binary:runner,Systemctl:"/usr/bin/systemctl",
  ServicePolicy:"/nix/store/fixed-services",
  KubernetesPolicy:"/nix/store/fixed-deployments",MaxReplicas:3,
 }
 run,err:=NewRustRun(cfg)
 if err!=nil{t.Fatal(err)}
 ctx,cancel:=context.WithCancel(context.Background())
 defer cancel()
 done:=make(chan error,1)
 go func(){done<-run(ctx,"/usr/bin/systemctl","restart","homelab-platform.service")}()
 deadline:=time.After(2*time.Second)
 for {
  if _,err:=os.Stat(started);err==nil{break}
  select {
  case err:=<-done:t.Fatalf("actuator returned before spawning its child: %v",err)
  case <-deadline:t.Fatal("actuator did not start")
  case <-time.After(5*time.Millisecond):
  }
 }
 cancel()
 select {
 case err:=<-done:
  if !errors.Is(err,context.Canceled){t.Fatalf("cancelled operation returned %v",err)}
 case <-time.After(2*time.Second):t.Fatal("cancelled process group did not exit")
 }
 time.Sleep(650*time.Millisecond)
 if _,err:=os.Stat(survived);err==nil{t.Fatal("inherited child survived cancelled operation")}
}

func TestPreCancelledActuatorDoesNotLaunch(t *testing.T) {
 dir:=t.TempDir()
 ran:=filepath.Join(dir,"ran")
 binary:=filepath.Join(dir,"runner")
 if err:=os.WriteFile(binary,[]byte("#!/bin/sh\necho yes > '"+ran+"'\n"),0700);err!=nil{t.Fatal(err)}
 run,err:=NewRustRun(RustActuatorConfig{
  Binary:binary,Systemctl:"/usr/bin/systemctl",
  ServicePolicy:"/nix/store/fixed-services",
  KubernetesPolicy:"/nix/store/fixed-deployments",MaxReplicas:3,
 })
 if err!=nil{t.Fatal(err)}
 ctx,cancel:=context.WithCancel(context.Background())
 cancel()
 if err:=run(ctx,"/usr/bin/systemctl","restart","homelab-platform.service");!errors.Is(err,context.Canceled){
  t.Fatalf("pre-cancelled operation returned %v",err)
 }
 if _,err:=os.Stat(ran);err==nil{t.Fatal("pre-cancelled command executed")}
}
