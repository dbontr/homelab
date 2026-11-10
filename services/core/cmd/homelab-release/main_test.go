package main

import (
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
