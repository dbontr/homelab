package privileged

import (
 "bufio"
 "encoding/hex"
 "encoding/json"
 "errors"
 "fmt"
 "io"
 "os"
 "path/filepath"
 "sync"
 "syscall"
 "time"
)

const maxAuditBytes int64 = 64 << 20

// AuditJournal owns one append-only inode for the life of its executor.
type AuditJournal struct {
 path string
 file *os.File
 mu sync.Mutex
 seen map[string]auditEntry
 faulted bool
}

type auditEntry struct {
 ID string `json:"id"`
 Operation string `json:"operation"`
 Target string `json:"target"`
 ArgumentsSHA256 string `json:"arguments_sha256"`
 Outcome string `json:"outcome"`
 At time.Time `json:"at"`
}

func validAuditEntry(e auditEntry)bool{
 if len(e.ID)!=32||len(e.ArgumentsSHA256)!=64||len(e.Operation)==0||len(e.Operation)>128||
  !nodePattern.MatchString(e.Target)||len(e.Target)>63||e.At.IsZero(){return false}
 if _,err:=hex.DecodeString(e.ID);err!=nil{return false}
 if _,err:=hex.DecodeString(e.ArgumentsSHA256);err!=nil{return false}
 switch e.Outcome{
 case "started","accepted","succeeded","failed":return true
 default:return false
 }
}

// validateAuditHistory rejects truncated, contradictory or malformed journals
// before an executor is permitted to carry out any privileged side effect.
func validateAuditHistory(file *os.File)(map[string]auditEntry,error){
 stat,err:=file.Stat()
 if err!=nil{return nil,err}
 if stat.Size()>maxAuditBytes{return nil,errors.New("audit journal exceeds size limit")}
 if stat.Size()>0{
  var last [1]byte
  if _,err:=file.ReadAt(last[:],stat.Size()-1);err!=nil||last[0]!='\n'{
   return nil,errors.New("audit journal contains an incomplete record")
  }
 }
 if _,err:=file.Seek(0,io.SeekStart);err!=nil{return nil,err}
 scanner:=bufio.NewScanner(file)
 scanner.Buffer(make([]byte,2048),2048)
 seen:=make(map[string]auditEntry)
 for scanner.Scan(){
  line:=scanner.Bytes()
  var event auditEntry
  if len(line)==0||json.Unmarshal(line,&event)!=nil||!validAuditEntry(event){
   return nil,errors.New("audit journal contains an invalid record")
  }
  previous,exists:=seen[event.ID]
  if event.Outcome=="started"{
   if exists{return nil,errors.New("audit journal repeats an operation start")}
  }else{
   if !exists||previous.Outcome!="started"||previous.Operation!=event.Operation||
    previous.Target!=event.Target||previous.ArgumentsSHA256!=event.ArgumentsSHA256||
    event.At.Before(previous.At){
    return nil,errors.New("audit journal contains a contradictory outcome")
   }
  }
  seen[event.ID]=event
 }
 if err:=scanner.Err();err!=nil{return nil,fmt.Errorf("audit journal validation failed: %w",err)}
 return seen,nil
}

func OpenAuditJournal(path string)(*AuditJournal,error){
 if !filepath.IsAbs(path)||filepath.Clean(path)!=path||path=="/"{
  return nil,errors.New("absolute private audit file required")
 }
 dir:=filepath.Dir(path)
 dirStat,err:=os.Lstat(dir)
 if err!=nil||!dirStat.IsDir()||dirStat.Mode().Perm()&0077!=0{
  return nil,errors.New("audit directory must be owner-only")
 }
 owner,ok:=dirStat.Sys().(*syscall.Stat_t)
 if !ok||owner.Uid!=uint32(os.Geteuid()){
  return nil,errors.New("audit directory has an unexpected owner")
 }
 file,err:=os.OpenFile(path,os.O_RDWR|os.O_APPEND|os.O_CREATE|syscall.O_NOFOLLOW|syscall.O_CLOEXEC,0600)
 if err!=nil{return nil,fmt.Errorf("open audit journal: %w",err)}
 fail:=func(err error)(*AuditJournal,error){_ =file.Close();return nil,err}
 stat,err:=file.Stat()
 if err!=nil{return fail(err)}
 id,ok:=stat.Sys().(*syscall.Stat_t)
 if !ok||!stat.Mode().IsRegular()||stat.Mode().Perm()&0077!=0||id.Uid!=uint32(os.Geteuid()){
  return fail(errors.New("audit file must be an owner-only regular file"))
 }
 // Durably publish the inode before any later handler is permitted to write.
 directory,err:=os.Open(dir)
 if err!=nil{return fail(err)}
 syncErr:=directory.Sync()
 closeErr:=directory.Close()
 if syncErr!=nil{return fail(syncErr)}
 if closeErr!=nil{return fail(closeErr)}
 seen,err:=validateAuditHistory(file)
 if err!=nil{return fail(err)}
 return &AuditJournal{path:path,file:file,seen:seen},nil
}

func(j *AuditJournal)checkInode()error{
 if j.file==nil{return errors.New("audit journal closed")}
 pathStat,err:=os.Lstat(j.path)
 if err!=nil{return errors.New("audit journal path unavailable")}
 opened,err:=j.file.Stat()
 if err!=nil||!opened.Mode().IsRegular()||opened.Mode().Perm()&0077!=0||
  !os.SameFile(pathStat,opened){
  return errors.New("audit journal path was replaced or made unsafe")
 }
 return nil
}

func(j *AuditJournal)Record(id,operation,target,digest,outcome string)error{
 if j==nil{return errors.New("audit journal unavailable")}
 entry:=auditEntry{ID:id,Operation:operation,Target:target,
  ArgumentsSHA256:digest,Outcome:outcome,At:time.Now().UTC()}
 if !validAuditEntry(entry){return errors.New("invalid audit event")}
 raw,err:=json.Marshal(entry)
 if err!=nil{return err}
 if len(raw)>1024{return errors.New("audit event exceeds bound")}
 raw=append(raw,'\n')
 j.mu.Lock()
 defer j.mu.Unlock()
 if j.faulted{return errors.New("audit journal is in an uncertain state")}
 if err:=j.checkInode();err!=nil{j.faulted=true;return err}
 previous,exists:=j.seen[entry.ID]
 if entry.Outcome=="started" {
  if exists{return errors.New("audit ID was already used")}
 }else{
  if !exists||previous.Outcome!="started"||previous.Operation!=entry.Operation||
   previous.Target!=entry.Target||previous.ArgumentsSHA256!=entry.ArgumentsSHA256||
   entry.At.Before(previous.At){
   return errors.New("audit outcome has no matching unfinished operation")
  }
 }
 stat,err:=j.file.Stat()
 if err!=nil{j.faulted=true;return err}
 if stat.Size()+int64(len(raw))>maxAuditBytes{return errors.New("audit journal exceeds size limit")}
 n,err:=j.file.Write(raw)
 if err!=nil||n!=len(raw){
  j.faulted=true
  if err!=nil{return err}
  return io.ErrShortWrite
 }
 if err:=j.file.Sync();err!=nil{
  j.faulted=true
  return err
 }
 j.seen[entry.ID]=entry
 return nil
}

func(j *AuditJournal)Close()error{
 if j==nil{return nil}
 j.mu.Lock()
 defer j.mu.Unlock()
 if j.file==nil{return nil}
 err:=j.file.Close()
 j.file=nil
 return err
}
