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
 "sort"
 "strings"
 "sync"
 "syscall"
 "time"

 "github.com/dbontr/homelab-new/system/canonicaljson"
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
 ReconciliationSHA256 string `json:"reconciliation_sha256,omitempty"`
}

func validAuditEntry(e auditEntry)bool{
 if len(e.ID)!=32||len(e.ArgumentsSHA256)!=64||len(e.Operation)==0||len(e.Operation)>128||
  !nodePattern.MatchString(e.Target)||len(e.Target)>63||e.At.IsZero(){return false}
 if _,err:=hex.DecodeString(e.ID);err!=nil||e.ID!=strings.ToLower(e.ID){return false}
 if _,err:=hex.DecodeString(e.ArgumentsSHA256);err!=nil||
  e.ArgumentsSHA256!=strings.ToLower(e.ArgumentsSHA256){return false}
 if e.At.Year()<=1{return false}
 if e.Outcome=="reconciled"{
  if len(e.ReconciliationSHA256)!=64{return false}
  _,err:=hex.DecodeString(e.ReconciliationSHA256)
  return err==nil&&e.ReconciliationSHA256==strings.ToLower(e.ReconciliationSHA256)
 }
 if e.ReconciliationSHA256!=""{return false}
 switch e.Outcome{
 case "started","accepted","succeeded","failed","indeterminate":return true
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
  if len(line)==0{return nil,errors.New("audit journal contains an empty record")}
  canonical,fields,parseErr:=canonicaljson.Object(line)
  if parseErr!=nil||len(fields)<6||len(fields)>7{
   return nil,errors.New("audit journal contains invalid JSON")
  }
  for key:=range fields{
   switch key{
   case "id","operation","target","arguments_sha256","outcome","at","reconciliation_sha256":
   default:return nil,errors.New("audit journal contains an unknown field")
   }
  }
  var event auditEntry
  if json.Unmarshal(canonical,&event)!=nil||!validAuditEntry(event){
   return nil,errors.New("audit journal contains an invalid record")
  }
  previous,exists:=seen[event.ID]
  if event.Outcome=="started"{
   if exists{return nil,errors.New("audit journal repeats an operation start")}
  }else{
   validPrior:=exists&&previous.Outcome=="started"
   if event.Outcome=="reconciled"{
    validPrior=exists&&(previous.Outcome=="started"||previous.Outcome=="indeterminate"||previous.Outcome=="failed")
   }
   if !validPrior||previous.Operation!=event.Operation||
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
 if !ok||!stat.Mode().IsRegular()||stat.Mode().Perm()&0077!=0||
  id.Uid!=uint32(os.Geteuid())||id.Nlink!=1{
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
 inode,ok:=opened.Sys().(*syscall.Stat_t)
 if !ok||inode.Nlink!=1||inode.Uid!=uint32(os.Geteuid()){
  return errors.New("audit journal path was replaced or made unsafe")
 }
 return nil
}

// appendLocked checks custody, transitions, replay barriers and durability.
// Callers hold j.mu; a failed write or sync disables further journal use.
func(j *AuditJournal)appendLocked(entry auditEntry)error{
 if j.faulted{return errors.New("audit journal is in an uncertain state")}
 if !validAuditEntry(entry){return errors.New("invalid audit event")}
 raw,err:=json.Marshal(entry)
 if err!=nil{return err}
 if len(raw)>1024{return errors.New("audit event exceeds bound")}
 raw=append(raw,'\n')
 if err:=j.checkInode();err!=nil{j.faulted=true;return err}
 previous,exists:=j.seen[entry.ID]
 if entry.Outcome=="started" {
  if exists{return errors.New("audit ID was already used")}
  for _,prior:=range j.seen{
   if prior.Operation==entry.Operation&&prior.Target==entry.Target&&
    prior.ArgumentsSHA256==entry.ArgumentsSHA256&&
    (prior.Outcome=="started"||prior.Outcome=="indeterminate"||prior.Outcome=="failed"){
    return errors.New("matching unresolved execution requires reconciliation")
   }
  }
 }else{
  validPrevious:=exists&&previous.Outcome=="started"
  if entry.Outcome=="reconciled"{
   validPrevious=exists&&(previous.Outcome=="started"||
    previous.Outcome=="indeterminate"||previous.Outcome=="failed")
  }
  if !validPrevious||previous.Operation!=entry.Operation||
   previous.Target!=entry.Target||previous.ArgumentsSHA256!=entry.ArgumentsSHA256||
   entry.At.Before(previous.At){
   return errors.New("audit outcome has no matching unresolved operation")
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

// RequestOutcome reads the durable disposition for an exact caller request.
// A reused identity with a different scope never inherits the prior approval.
// The journal inode and fault state are checked before returning any result.
func (j *AuditJournal) RequestOutcome(id,operation,target,digest string)(string,bool,error){
 if j==nil{return "",false,errors.New("audit journal unavailable")}
 if len(id)!=32||len(digest)!=64||id!=strings.ToLower(id)||
  digest!=strings.ToLower(digest){
  return "",false,errors.New("invalid execution request identity")
 }
 if _,err:=hex.DecodeString(id);err!=nil{return "",false,err}
 if _,err:=hex.DecodeString(digest);err!=nil{return "",false,err}
 j.mu.Lock()
 defer j.mu.Unlock()
 if j.faulted{return "",false,errors.New("audit journal is in an uncertain state")}
 if err:=j.checkInode();err!=nil{j.faulted=true;return "",false,err}
 entry,found:=j.seen[id]
 if !found{return "",false,nil}
 if entry.Operation!=operation||entry.Target!=target||entry.ArgumentsSHA256!=digest{
  return "",false,errors.New("execution request identity reused for another operation")
 }
 return entry.Outcome,true,nil
}

func(j *AuditJournal)Record(id,operation,target,digest,outcome string)error{
 if j==nil{return errors.New("audit journal unavailable")}
 if outcome=="reconciled" {
  return errors.New("operator reconciliation requires separate evidence")
 }
 entry:=auditEntry{ID:id,Operation:operation,Target:target,
  ArgumentsSHA256:digest,Outcome:outcome,At:time.Now().UTC()}
 j.mu.Lock();defer j.mu.Unlock()
 return j.appendLocked(entry)
}

// Reconcile is intended only for a trusted root recovery command that has
// acquired the executor's process-lifetime state lease. It appends an evidence
// digest, not a claim that an unknown operation succeeded.
func(j *AuditJournal)Reconcile(id,evidenceSHA256 string)error{
 if j==nil{return errors.New("audit journal unavailable")}
 if len(evidenceSHA256)!=64||evidenceSHA256!=strings.ToLower(evidenceSHA256){
  return errors.New("invalid reconciliation evidence digest")
 }
 if _,err:=hex.DecodeString(evidenceSHA256);err!=nil{return err}
 j.mu.Lock();defer j.mu.Unlock()
 if j.faulted{return errors.New("audit journal is in an uncertain state")}
 previous,ok:=j.seen[id]
 if !ok||!matchesUnresolved(previous.Outcome){
  return errors.New("audit identity is not unresolved")
 }
 entry:=previous
 entry.Outcome="reconciled"
 entry.ReconciliationSHA256=evidenceSHA256
 entry.At=time.Now().UTC()
 return j.appendLocked(entry)
}

func matchesUnresolved(outcome string)bool{
 return outcome=="started"||outcome=="indeterminate"||outcome=="failed"
}

// Unresolved returns attempts that can have taken effect but have no
// verified terminal state. The caller must reconcile them out of band.
func(j *AuditJournal)Unresolved()([]auditEntry,error){
 if j==nil{return nil,errors.New("audit journal unavailable")}
 j.mu.Lock();defer j.mu.Unlock()
 if j.faulted{return nil,errors.New("audit journal is in an uncertain state")}
 if err:=j.checkInode();err!=nil{
  j.faulted=true
  return nil,err
 }
 pending:=make([]auditEntry,0)
 for _,entry:=range j.seen{
  if entry.Outcome=="started"||entry.Outcome=="indeterminate"||entry.Outcome=="failed"{
   pending=append(pending,entry)
  }
 }
 sort.Slice(pending,func(i,k int)bool{return pending[i].ID<pending[k].ID})
 return pending,nil
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
