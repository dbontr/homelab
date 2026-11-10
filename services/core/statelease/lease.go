// Package statelease prevents independent processes from writing the same
// security, release, or audit state concurrently.
package statelease

import (
 "errors"
 "fmt"
 "os"
 "path/filepath"
 "regexp"
 "sync"
 "syscall"
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
 if !ok||!stat.Mode().IsRegular()||stat.Mode().Perm()&0077!=0||id.Uid!=uint32(os.Geteuid()){
  return fail(errors.New("state lease lock must be an owner-only regular file"))
 }
 if err:=syscall.Flock(int(file.Fd()),syscall.LOCK_EX|syscall.LOCK_NB);err!=nil{
  return fail(fmt.Errorf("state lease is already held or unavailable: %w",err))
 }
 return &Lease{file:file},nil
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
