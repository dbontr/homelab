package release

import (
 "bytes"
 "context"
 "crypto/rand"
 "crypto/sha256"
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

// ReleaseObservation is reported by a host's protected release journal.
type ReleaseObservation struct {
 ID string `json:"id"`
 Phase string `json:"phase"`
}

type ReleaseObserver interface {
 Observe(context.Context)(ReleaseObservation,error)
}

type FleetTransaction struct {
 ID string `json:"id"`
 Phase string `json:"phase"`
 Targets []FleetTarget `json:"targets"`
 Steps []FleetStep `json:"steps"`
 UpdatedAt time.Time `json:"updated_at"`
}

// DurableFleet records intent before a host operation. An interrupted action
// is never retried without a matching independent host observation.
type DurableFleet struct {
 StateFile string
 Agents map[string]HostReleaseAgent
 mu sync.Mutex
}

func(d *DurableFleet)locked(fn func()error)error{
 if d==nil||!filepath.IsAbs(d.StateFile)||filepath.Base(d.StateFile)=="."||filepath.Base(d.StateFile)=="/"||d.Agents==nil{
  return errors.New("invalid durable fleet configuration")
 }
 d.mu.Lock();defer d.mu.Unlock()
 dir:=filepath.Dir(d.StateFile)
 if err:=os.MkdirAll(dir,0700);err!=nil{return err}
 info,err:=os.Lstat(dir)
 if err!=nil||!info.IsDir()||info.Mode()&os.ModeSymlink!=0||info.Mode().Perm()&0077!=0{
  return errors.New("fleet state directory is not private")
 }
 lock,err:=os.OpenFile(d.StateFile+".lock",os.O_CREATE|os.O_RDWR,0600)
 if err!=nil{return err}
 defer lock.Close()
 if info,err:=lock.Stat();err!=nil||!info.Mode().IsRegular()||info.Mode().Perm()&0077!=0{
  return errors.New("unsafe fleet lock file")
 }
 if err:=syscall.Flock(int(lock.Fd()),syscall.LOCK_EX);err!=nil{return err}
 defer syscall.Flock(int(lock.Fd()),syscall.LOCK_UN)
 return fn()
}

func(d *DurableFleet)read()(FleetTransaction,error){
 var tx FleetTransaction
 info,err:=os.Lstat(d.StateFile)
 if err!=nil{return tx,err}
 if !info.Mode().IsRegular()||info.Mode().Perm()&0077!=0{return tx,errors.New("unsafe fleet journal")}
 f,err:=os.Open(d.StateFile);if err!=nil{return tx,err}
 defer f.Close()
 data,err:=io.ReadAll(io.LimitReader(f,32769))
 if err!=nil{return tx,err}
 if len(data)>32768{return tx,errors.New("fleet journal exceeds limit")}
 decoder:=json.NewDecoder(bytes.NewReader(data))
 decoder.DisallowUnknownFields()
 if err:=decoder.Decode(&tx);err!=nil{return tx,errors.New("invalid fleet journal")}
 var extra any
 if err:=decoder.Decode(&extra);err!=io.EOF{return tx,errors.New("invalid fleet journal trailing content")}
 if !idPattern.MatchString(tx.ID)||len(tx.Targets)==0||len(tx.Targets)!=len(tx.Steps){return tx,errors.New("invalid fleet journal identity")}
 if err:=validateFleet(tx.Targets,d.Agents);err!=nil{return tx,err}
 for i,step:=range tx.Steps{
  target:=tx.Targets[i]
  if step.Host!=target.Host||step.Role!=target.Role||step.ReleaseID!=target.ReleaseID{return tx,errors.New("fleet journal target mismatch")}
 }
 return tx,nil
}

func(d *DurableFleet)write(tx *FleetTransaction)error{
 tx.UpdatedAt=time.Now().UTC()
 data,err:=json.Marshal(tx);if err!=nil{return err}
 if len(data)>32768{return errors.New("fleet journal exceeds limit")}
 dir:=filepath.Dir(d.StateFile)
 f,err:=os.CreateTemp(dir,".fleet-*");if err!=nil{return err}
 defer os.Remove(f.Name())
 if err:=f.Chmod(0600);err!=nil{f.Close();return err}
 if _,err:=f.Write(append(data,'\n'));err!=nil{f.Close();return err}
 if err:=f.Sync();err!=nil{f.Close();return err}
 if err:=f.Close();err!=nil{return err}
 if err:=os.Rename(f.Name(),d.StateFile);err!=nil{return err}
 directory,err:=os.Open(dir);if err!=nil{return err}
 defer directory.Close()
 return directory.Sync()
}

func(d *DurableFleet)Status()(FleetTransaction,error){
 var tx FleetTransaction
 err:=d.locked(func()error{var err error;tx,err=d.read();return err})
 return tx,err
}

func(d *DurableFleet)Start(ctx context.Context,targets []FleetTarget)(FleetTransaction,error){
 var tx FleetTransaction
 err:=d.locked(func()error{var err error;tx,err=d.startLocked(ctx,targets);return err})
 return tx,err
}

// Derive one stable host request identity from a durable transaction. The
// domain prefix separates executor identities from other transaction digests.
func fleetExecutionRequestID(transactionID,operation,host,releaseID string)string{
 digest:=sha256.Sum256([]byte("homelab:executor-request:v1\x00"+
  transactionID+"\x00"+operation+"\x00"+host+"\x00"+releaseID))
 return hex.EncodeToString(digest[:16])
}

func(d *DurableFleet) identifiedAgent(host string)(RequestIdentifiedReleaseAgent,error){
 agent,ok:=d.Agents[host].(RequestIdentifiedReleaseAgent)
 if !ok{return nil,errors.New("durable fleet host requires request-identified execution")}
 return agent,nil
}

// startLocked is called only while the exclusive fleet state lock is held.
func(d *DurableFleet)startLocked(ctx context.Context,targets []FleetTarget)(FleetTransaction,error){
 var tx FleetTransaction
 if err:=validateFleet(targets,d.Agents);err!=nil{return tx,err}
 for _,target:=range targets{
  if _,err:=d.identifiedAgent(target.Host);err!=nil{return tx,err}
 }
 if _,err:=os.Lstat(d.StateFile);err==nil{return tx,errors.New("existing fleet transaction requires explicit resolution")}else if !errors.Is(err,os.ErrNotExist){return tx,err}
 token:=make([]byte,24);if _,err:=rand.Read(token);err!=nil{return tx,err}
 tx=FleetTransaction{ID:hex.EncodeToString(token),Phase:"activating",Targets:append([]FleetTarget(nil),targets...)}
 for _,target:=range targets{tx.Steps=append(tx.Steps,FleetStep{Host:target.Host,Role:target.Role,ReleaseID:target.ReleaseID,State:"pending"})}
 if err:=d.write(&tx);err!=nil{return tx,err}
 return tx,d.activate(ctx,&tx)
}

func(d *DurableFleet)activate(ctx context.Context,tx *FleetTransaction)error{
 for i,target:=range tx.Targets{
  if err:=ctx.Err();err!=nil{return d.fail(tx,fmt.Errorf("rollout cancelled: %w",err))}
  tx.Steps[i].State="activating"
  if err:=d.write(tx);err!=nil{return err}
  agent,err:=d.identifiedAgent(target.Host)
  if err!=nil{return err}
  requestID:=fleetExecutionRequestID(tx.ID,"infrastructure__release_activate",
   target.Host,target.ReleaseID)
  if err:=agent.ActivateWithID(ctx,requestID,target.ReleaseID);err!=nil{
   tx.Steps[i].State="unknown";tx.Steps[i].Error="activation outcome unknown; check the host journal"
   if writeErr:=d.write(tx);writeErr!=nil{return errors.Join(err,writeErr)}
   return d.fail(tx,fmt.Errorf("host %s activation unverified: %w",target.Host,err))
  }
  tx.Steps[i].State="active"
  if err:=d.write(tx);err!=nil{return err}
 }
 tx.Phase="complete"
 return d.write(tx)
}

func(d *DurableFleet)fail(tx *FleetTransaction,cause error)error{
 tx.Phase="rolling-back"
 if err:=d.write(tx);err!=nil{return errors.Join(cause,err)}
 cleanup,cancel:=context.WithTimeout(context.Background(),3*time.Minute)
 defer cancel()
 if err:=d.rollback(cleanup,tx);err!=nil{return errors.Join(cause,err)}
 return cause
}

func(d *DurableFleet)rollback(ctx context.Context,tx *FleetTransaction)error{
 unresolved:=false
 for i:=len(tx.Steps)-1;i>=0;i--{
  step:=&tx.Steps[i]
  if step.State!="active"{
   if step.State!="reverted"&&step.State!="pending"{unresolved=true}
   continue
  }
  step.State="reverting"
  if err:=d.write(tx);err!=nil{return err}
  agent,err:=d.identifiedAgent(step.Host)
  if err!=nil{return err}
  requestID:=fleetExecutionRequestID(tx.ID,"infrastructure__release_revert",
   step.Host,step.ReleaseID)
  if err:=agent.RevertWithID(ctx,requestID,step.ReleaseID);err!=nil{
   step.State="revert-unknown";step.Error="rollback outcome unknown; check the host journal";unresolved=true
  }else{step.State="reverted";step.Error=""}
  if err:=d.write(tx);err!=nil{return err}
 }
 if unresolved{tx.Phase="attention"}else{tx.Phase="reverted"}
 return d.write(tx)
}

// Recover reconciles in-flight hosts before attempting reverse rollback.
// Missing or inconsistent observations never authorize a replay.
func(d *DurableFleet)Recover(ctx context.Context,transactionID string)(FleetTransaction,error){
 var tx FleetTransaction
 err:=d.locked(func()error{
  var err error
  tx,err=d.read();if err!=nil{return err}
  if tx.ID!=transactionID{return errors.New("fleet transaction identity mismatch")}
  if tx.Phase=="complete"||tx.Phase=="reverted"{return nil}
  for i:=range tx.Steps{
   step:=&tx.Steps[i]
   if step.State!="activating"&&step.State!="unknown"&&step.State!="reverting"&&step.State!="revert-unknown"{continue}
   observer,ok:=d.Agents[step.Host].(ReleaseObserver)
   if !ok{continue}
   observation,err:=observer.Observe(ctx)
   if err!=nil||observation.ID!=step.ReleaseID{continue}
   switch observation.Phase{
   case "active":step.State="active";step.Error=""
   case "rolled-back":step.State="reverted";step.Error=""
   }
   if err:=d.write(&tx);err!=nil{return err}
  }
  tx.Phase="rolling-back"
  if err:=d.write(&tx);err!=nil{return err}
  return d.rollback(ctx,&tx)
 })
 return tx,err
}

// Clear permits a trusted local operator to retire a resolved transaction.
func(d *DurableFleet)Clear(transactionID string)error{
 return d.locked(func()error{
  tx,err:=d.read();if err!=nil{return err}
  if tx.ID!=transactionID||(tx.Phase!="complete"&&tx.Phase!="reverted"){return errors.New("fleet transaction is not resolved")}
  if err:=os.Remove(d.StateFile);err!=nil{return err}
  directory,err:=os.Open(filepath.Dir(d.StateFile));if err!=nil{return err}
  defer directory.Close()
  return directory.Sync()
 })
}
