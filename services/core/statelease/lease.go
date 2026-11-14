// Package statelease prevents independent processes from writing the same
// security, release, or audit state concurrently.
package statelease

import (
 "context"
 "errors"
 "fmt"
 "os"
 "path/filepath"
 "regexp"
 "sync"
 "syscall"
 "time"
)

var lockName=regexp.MustCompile(`^[a-z][a-z0-9-]{0,47}$`)

type Lease struct{
 file *os.File
 mu sync.Mutex
}

// Acquire holds an advisory Linux flock for the lifetime of the caller.
// It never unlinks its lock file: unlinking a locked inode would allow a
// different process to acquire a new inode under the same path.
func Acquire(dir,name string)(*Lease,error){
 if !filepath.IsAbs(dir)||filepath.Clean(dir)!=dir||!lockName.MatchString(name){
  return nil,errors.New("state lease requires a canonical absolute directory and fixed name")
 }
 info,err:=os.Lstat(dir)
 if err!=nil||!info.IsDir()||info.Mode().Perm()&0077!=0{
  return nil,errors.New("state lease directory must be owner-only")
 }
 owner,ok:=info.Sys().(*syscall.Stat_t)
 if !ok||owner.Uid!=uint32(os.Geteuid()){
  return nil,errors.New("state lease directory has an unexpected owner")
 }
 path:=filepath.Join(dir,"."+name+".lock")
 file,err:=os.OpenFile(path,os.O_RDWR|os.O_CREATE|syscall.O_NOFOLLOW|syscall.O_CLOEXEC,0600)
 if err!=nil{return nil,fmt.Errorf("open state lease: %w",err)}
 fail:=func(err error)(*Lease,error){_ =file.Close();return nil,err}
 stat,err:=file.Stat()
 if err!=nil{return fail(err)}
 id,ok:=stat.Sys().(*syscall.Stat_t)
 if !ok||!stat.Mode().IsRegular()||stat.Mode().Perm()&0077!=0||
  id.Uid!=uint32(os.Geteuid())||id.Nlink!=1{
  return fail(errors.New("state lease lock must be an owner-only regular file"))
 }
 if err:=syscall.Flock(int(file.Fd()),syscall.LOCK_EX|syscall.LOCK_NB);err!=nil{
  return fail(fmt.Errorf("state lease is already held or unavailable: %w",err))
 }
 // Verify path identity after taking the lock: a swapped lock filename
 // could otherwise let independent processes own different lock inodes.
 current,err:=os.Lstat(path)
 if err!=nil||!os.SameFile(current,stat){
  return fail(errors.New("state lease lock inode changed during acquisition"))
 }
 return &Lease{file:file},nil
}

// AcquireWhenAvailable waits only for an existing owner of the same lease.
// All path, ownership, and inode validation errors still fail immediately.
// A caller must give this operation a finite context deadline.
func AcquireWhenAvailable(ctx context.Context,dir,name string)(*Lease,error){
 ticker:=time.NewTicker(100*time.Millisecond)
 defer ticker.Stop()
 for{
  if err:=ctx.Err();err!=nil{return nil,err}
  lease,err:=Acquire(dir,name)
  if err==nil{return lease,nil}
  if !errors.Is(err,syscall.EWOULDBLOCK)&&!errors.Is(err,syscall.EAGAIN){return nil,err}
  select{
  case <-ctx.Done():return nil,ctx.Err()
  case <-ticker.C:
  }
 }
}

// Close releases ownership only when the process is terminating or all state
// access has stopped. Callers must retain the lease for the daemon lifetime.
func(l *Lease)Close()error{
 if l==nil{return nil}
 l.mu.Lock()
 defer l.mu.Unlock()
 if l.file==nil{return nil}
 err:=l.file.Close()
 l.file=nil
 return err
}
