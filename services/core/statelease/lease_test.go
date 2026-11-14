package statelease

import (
 "context"
 "os"
 "path/filepath"
 "testing"
 "time"
)

func TestExclusiveOwnershipAndReacquisition(t *testing.T){
 dir:=privateDir(t)
 first,err:=Acquire(dir,"security")
 if err!=nil{t.Fatal(err)}
 if _,err:=Acquire(dir,"security");err==nil{
  t.Fatal("second state owner acquired the same lock")
 }
 if _,err:=Acquire(dir,"executor");err!=nil{
  t.Fatal("separate state owner must not share the same lock identity:",err)
 }
 if err:=first.Close();err!=nil{t.Fatal(err)}
 next,err:=Acquire(dir,"security")
 if err!=nil{t.Fatal("lease did not become available after clean shutdown:",err)}
 defer next.Close()
 if err:=next.Close();err!=nil{t.Fatal(err)}
}

func TestRejectsUnsafePathsPermissionsAndSymlinks(t *testing.T){
 dir:=privateDir(t)
 for _,tc:=range []struct{dir,name string}{
  {"relative","security"},{dir,"../security"},{dir,"security/other"},
 }{
  if lease,err:=Acquire(tc.dir,tc.name);err==nil{
   lease.Close()
   t.Fatalf("unsafe lease path accepted: %+v",tc)
  }
 }
 unsafe:=filepath.Join(t.TempDir(),"shared")
 if err:=os.Mkdir(unsafe,0755);err!=nil{t.Fatal(err)}
 // Establish the unsafe mode explicitly; release staging runs with umask 077.
 if err:=os.Chmod(unsafe,0755);err!=nil{t.Fatal(err)}
 if lease,err:=Acquire(unsafe,"security");err==nil{
  lease.Close();t.Fatal("shared state directory was accepted")
 }
 sym:=filepath.Join(t.TempDir(),"link")
 if err:=os.Symlink(dir,sym);err!=nil{t.Fatal(err)}
 if lease,err:=Acquire(sym,"security");err==nil{
  lease.Close();t.Fatal("directory symlink was accepted")
 }
 target:=filepath.Join(dir,"target")
 if err:=os.WriteFile(target,[]byte("protected"),0600);err!=nil{t.Fatal(err)}
 if err:=os.Symlink(target,filepath.Join(dir,".security.lock"));err!=nil{t.Fatal(err)}
 if lease,err:=Acquire(dir,"security");err==nil{
  lease.Close();t.Fatal("lock file symlink was accepted")
 }
 if data,err:=os.ReadFile(target);err!=nil||string(data)!="protected"{t.Fatal("unsafe path modified its target")}
}

func TestRejectsWritableOrMismatchedLockFile(t *testing.T){
 dir:=privateDir(t)
 path:=filepath.Join(dir,".executor.lock")
 if err:=os.WriteFile(path,nil,0660);err!=nil{t.Fatal(err)}
 if err:=os.Chmod(path,0660);err!=nil{t.Fatal(err)}
 if lease,err:=Acquire(dir,"executor");err==nil{
  lease.Close();t.Fatal("group-accessible lock accepted")
 }
 if err:=os.Chmod(path,0600);err!=nil{t.Fatal(err)}
 lease,err:=Acquire(dir,"executor")
 if err!=nil{t.Fatal(err)}
 defer lease.Close()
 if _,err:=os.Lstat(path);err!=nil{t.Fatal("lock inode unexpectedly removed")}
}

func TestLeaseRejectsSharedInodes(t *testing.T){
 dir:=privateDir(t)
 path:=filepath.Join(dir,".securityd.lock")
 if err:=os.WriteFile(path,nil,0600);err!=nil{t.Fatal(err)}
 second:=filepath.Join(dir,"shared-lock-inode")
 if err:=os.Link(path,second);err!=nil{t.Fatal(err)}
 if lease,err:=Acquire(dir,"securityd");err==nil{
  lease.Close()
  t.Fatal("security owner accepted a multiply-linked lock inode")
 }
 if err:=os.Remove(second);err!=nil{t.Fatal(err)}
 lease,err:=Acquire(dir,"securityd")
 if err!=nil{t.Fatal(err)}
 if err:=lease.Close();err!=nil{t.Fatal(err)}
}

func TestAcquireWhenAvailableWaitsForOwner(t *testing.T){
 dir:=privateDir(t)
 first,err:=Acquire(dir,"executord");if err!=nil{t.Fatal(err)}
 defer first.Close()
 ctx,cancel:=context.WithTimeout(context.Background(),2*time.Second);defer cancel()
 acquired:=make(chan *Lease,1)
 failures:=make(chan error,1)
 go func(){lease,err:=AcquireWhenAvailable(ctx,dir,"executord");if err!=nil{failures<-err;return};acquired<-lease}()
 select{
 case lease:=<-acquired:lease.Close();t.Fatal("second owner acquired lease before release")
 case err:=<-failures:t.Fatal(err)
 case <-time.After(30*time.Millisecond):
 }
 if err:=first.Close();err!=nil{t.Fatal(err)}
 select{
 case lease:=<-acquired:defer lease.Close()
 case err:=<-failures:t.Fatal(err)
 case <-ctx.Done():t.Fatal("second owner never acquired released lease")
 }
}

func TestAcquireWhenAvailableHonorsCancellationAndUnsafeState(t *testing.T){
 dir:=privateDir(t)
 owner,err:=Acquire(dir,"executord");if err!=nil{t.Fatal(err)}
 defer owner.Close()
 ctx,cancel:=context.WithTimeout(context.Background(),30*time.Millisecond);defer cancel()
 if lease,err:=AcquireWhenAvailable(ctx,dir,"executord");lease!=nil||err!=context.DeadlineExceeded{
  if lease!=nil{lease.Close()};t.Fatalf("expected cancellation, got %v",err)
 }
 if lease,err:=AcquireWhenAvailable(context.Background(),dir+"/../escape","executord");lease!=nil||err==nil{
  if lease!=nil{lease.Close()};t.Fatal("unsafe path was retried or accepted")
 }
}

func privateDir(t *testing.T)string{
 t.Helper()
 dir:=t.TempDir()
 if err:=os.Chmod(dir,0700);err!=nil{t.Fatal(err)}
 return dir
}
