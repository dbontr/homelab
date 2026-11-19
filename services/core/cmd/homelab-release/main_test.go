package main

import (
 "context"
 "os"
 "path/filepath"
 "testing"

 "github.com/dbontr/homelab-new/system/statelease"
)

func TestMutatingReleaseCLIRequiresDaemonStateOwnership(t *testing.T){
 state:=t.TempDir()
 if err:=os.Chmod(state,0700);err!=nil{t.Fatal(err)}
 active,err:=statelease.Acquire(state,"executord")
 if err!=nil{t.Fatal(err)}
 defer active.Close()
 for _,op:=range []string{"stage","activate","revert","recover"}{
  lease,err:=acquireReleaseLease(state,op)
  if err==nil{lease.Close();t.Fatalf("%s bypassed active executor lease",op)}
 }
 status,err:=acquireReleaseLease(state,"status")
 if err!=nil||status!=nil{t.Fatal("read-only release status should remain available")}
 if err:=active.Close();err!=nil{t.Fatal(err)}
 for _,op:=range []string{"stage","activate","revert","recover"}{
  lease,err:=acquireReleaseLease(state,op)
  if err!=nil{t.Fatalf("%s cannot acquire state after executor stop: %v",op,err)}
  if lease==nil{t.Fatalf("%s did not acquire a lease",op)}
  if err:=lease.Close();err!=nil{t.Fatal(err)}
 }
 if lease,err:=acquireReleaseLease(state,"unregistered");err==nil{
  if lease!=nil{lease.Close()}
  t.Fatal("unsupported command received a privileged state lease")
 }
 if _,err:=os.Lstat(filepath.Join(state,".executord.lock"));err!=nil{
  t.Fatal("release lease inode must remain stable across acquisitions")
 }
}

func TestProductionRootCLICannotReplaceSignedOwnerApproval(t *testing.T){
 for _,op:=range []string{"activate","revert"}{
  if err:=authorizeReleaseCLI(true,op);err==nil{
   t.Fatalf("root accepted unaudited %s on protected production host",op)
  }
  if err:=authorizeReleaseCLI(false,op);err!=nil{
   t.Fatalf("isolated test release unexpectedly denied: %v",err)
  }
 }
 for _,op:=range []string{"status","stage","recover","guard-activate","guard-revert"}{
  if err:=authorizeReleaseCLI(true,op);err!=nil{
   t.Fatalf("unexpected operation denial for %s: %v",op,err)
  }
 }
}

func TestConsoleStageDoesNotRequireExecutorLease(t *testing.T){
 state:=t.TempDir()
 if err:=os.Chmod(state,0700);err!=nil{t.Fatal(err)}
 held,err:=statelease.Acquire(state,"executord")
 if err!=nil{t.Fatal(err)}
 defer held.Close()
 for _,operation:=range []string{"console-stage","console-activate","console-reconcile"}{
  lease,err:=acquireReleaseLease(state,operation)
  if err!=nil||lease!=nil{t.Fatalf("%s required executor lease: %v",operation,err)}
 }
 if _,err:=acquireReleaseLease(state,"stage");err==nil{t.Fatal("managed stage acquired live executor lease")}
 if err:=authorizeReleaseCLI(true,"console-stage");err!=nil{t.Fatal(err)}
 ctrl,_,_:=guardianFixture(t)
 executor,err:=statelease.Acquire(ctrl.Config.StateDir,"executord")
 if err!=nil{t.Fatal(err)}
 defer executor.Close()
 staged,err:=ctrl.Stage(context.Background(),guardianRev)
 if err!=nil{t.Fatalf("staging under active executor lease failed: %v",err)}
 if err:=ctrl.VerifyStaged(staged.ID);err!=nil{t.Fatal(err)}
}
