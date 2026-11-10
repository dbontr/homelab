package privileged

import (
  "encoding/hex"
 "encoding/json"
 "errors"
 "fmt"
 "os"
 "path/filepath"
  "sync"
 "syscall"
 "time"
)

// AuditJournal keeps a durable record of privileged execution attempts. It
// never stores secret values, canonical arguments or bearer credentials.
type AuditJournal struct {
 path string
 mu sync.Mutex
}
type auditEntry struct {
 ID string `json:"id"`
 Operation string `json:"operation"`
 Target string `json:"target"`
 ArgumentsSHA256 string `json:"arguments_sha256"`
 Outcome string `json:"outcome"`
 At time.Time `json:"at"`
}

func OpenAuditJournal(path string)(*AuditJournal,error){
 if !filepath.IsAbs(path)||filepath.Clean(path)!=path||path=="/"{
  return nil,errors.New("absolute private audit file required")
 }
 dir:=filepath.Dir(path)
 info,err:=os.Lstat(dir)
 if err!=nil||!info.IsDir()||info.Mode().Perm()&0077!=0||
   info.Mode()&os.ModeSymlink!=0 {
  return nil,errors.New("audit directory must be private and regular")
 }
 if existing,err:=os.Lstat(path);err==nil {
  if !existing.Mode().IsRegular()||existing.Mode().Perm()&0077!=0{
   return nil,errors.New("audit file must be owner-only")
  }
 }else if !errors.Is(err,os.ErrNotExist){return nil,err}
 return &AuditJournal{path:path},nil
}

func(j *AuditJournal)Record(id,operation,target,digest,outcome string)error{
 if j==nil{return errors.New("audit journal unavailable")}
 if len(id)!=32||len(digest)!=64||len(operation)==0||len(operation)>128||
  !nodePattern.MatchString(target)||len(target)>63||
  (outcome!="started"&&outcome!="accepted"&&outcome!="succeeded"&&outcome!="failed"){
  return errors.New("invalid audit event")
 }
 if _,err:=hex.DecodeString(id);err!=nil{return errors.New("invalid audit ID")}
 if _,err:=hex.DecodeString(digest);err!=nil{return errors.New("invalid audit digest")}
 event:=auditEntry{ID:id,Operation:operation,Target:target,
  ArgumentsSHA256:digest,Outcome:outcome,At:time.Now().UTC()}
 raw,err:=json.Marshal(event)
 if err!=nil{return err}
 if len(raw)>1024{return errors.New("audit event exceeds bound")}
 raw=append(raw,'\n')
 j.mu.Lock()
 defer j.mu.Unlock()
 file,err:=os.OpenFile(j.path,os.O_WRONLY|os.O_APPEND|os.O_CREATE|syscall.O_NOFOLLOW,0600)
 if err!=nil{return fmt.Errorf("cannot open audit journal: %w",err)}
 defer file.Close()
 stat,err:=file.Stat()
 if err!=nil||!stat.Mode().IsRegular()||stat.Mode().Perm()&0077!=0{
  return errors.New("unsafe audit journal")
 }
 if _,err:=file.Write(raw);err!=nil{return err}
 return file.Sync()
}
