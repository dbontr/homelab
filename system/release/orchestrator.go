package release

import (
 "bytes"
 "context"
 "encoding/json"
 "errors"
 "io"
 "os"
 "path/filepath"
)

// FleetOrchestrator holds a privately staged, immutable fleet plan. The
// gateway cannot replace one host's release with another staged generation.
type FleetOrchestrator struct {
 Fleet *DurableFleet
 StageTargets []FleetStageTarget
 StageAgents map[string]FleetStageAgent
 PlanFile string
}

func(o *FleetOrchestrator)check()error{
 if o==nil||o.Fleet==nil||o.PlanFile==""||filepath.Dir(o.PlanFile)!=filepath.Dir(o.Fleet.StateFile)||o.PlanFile==o.Fleet.StateFile{
  return errors.New("invalid fleet orchestrator state")
 }
 return nil
}

func(o *FleetOrchestrator)writePlan(plan FleetStagePlan)error{
 payload,err:=json.Marshal(plan)
 if err!=nil{return err}
 if len(payload)>32768{return errors.New("fleet plan exceeds limit")}
 dir:=filepath.Dir(o.PlanFile)
 f,err:=os.CreateTemp(dir,".stage-plan-*")
 if err!=nil{return err}
 defer os.Remove(f.Name())
 if err:=f.Chmod(0600);err!=nil{f.Close();return err}
 if _,err:=f.Write(append(payload,'\n'));err!=nil{f.Close();return err}
 if err:=f.Sync();err!=nil{f.Close();return err}
 if err:=f.Close();err!=nil{return err}
 if err:=os.Rename(f.Name(),o.PlanFile);err!=nil{return err}
 d,err:=os.Open(dir);if err!=nil{return err};defer d.Close()
 return d.Sync()
}

func(o *FleetOrchestrator)readPlan()(FleetStagePlan,error){
 var plan FleetStagePlan
 info,err:=os.Lstat(o.PlanFile)
 if err!=nil{return plan,err}
 if !info.Mode().IsRegular()||info.Mode().Perm()&0077!=0{return plan,errors.New("unsafe fleet plan record")}
 f,err:=os.Open(o.PlanFile);if err!=nil{return plan,err};defer f.Close()
 raw,err:=io.ReadAll(io.LimitReader(f,32769));if err!=nil{return plan,err}
 if len(raw)>32768{return plan,errors.New("fleet plan too large")}
 decoder:=json.NewDecoder(bytes.NewReader(raw));decoder.DisallowUnknownFields()
 if err:=decoder.Decode(&plan);err!=nil{return plan,errors.New("invalid fleet plan")}
 var extra any
 if err:=decoder.Decode(&extra);err!=io.EOF{return plan,errors.New("fleet plan has trailing content")}
 if !idPattern.MatchString(plan.ID){return plan,errors.New("invalid fleet plan identity")}
 if err:=plan.Validate(o.Fleet.Agents);err!=nil{return plan,err}
 if len(plan.Targets)!=len(o.StageTargets){return plan,errors.New("stage plan differs from configured fleet")}
 for i,target:=range o.StageTargets{
  if target.Host!=plan.Targets[i].Host||target.Role!=plan.Targets[i].Role{return plan,errors.New("stage plan differs from configured fleet")}
 }
 return plan,nil
}

// Stage checks the configured fleet, stages every exact source revision and
// persists the resulting plan before it can be activated.
func(o *FleetOrchestrator)Stage(ctx context.Context,revision string)(FleetStagePlan,error){
 var plan FleetStagePlan
 if err:=o.check();err!=nil{return plan,err}
 err:=o.Fleet.locked(func()error{
  if _,err:=os.Lstat(o.PlanFile);err==nil{return errors.New("existing fleet plan must be resolved first")}else if !errors.Is(err,os.ErrNotExist){return err}
  if _,err:=os.Lstat(o.Fleet.StateFile);err==nil{return errors.New("existing fleet transaction must be resolved first")}else if !errors.Is(err,os.ErrNotExist){return err}
  var err error
  plan,err=StageFleet(ctx,revision,o.StageTargets,o.StageAgents)
  if err!=nil{return err}
  plan.ID,err=newID()
  if err!=nil{return err}
  if err:=plan.Validate(o.Fleet.Agents);err!=nil{return err}
  return o.writePlan(plan)
 })
 return plan,err
}

func(o *FleetOrchestrator)Plan()(FleetStagePlan,error){
 var plan FleetStagePlan
 if err:=o.check();err!=nil{return plan,err}
 err:=o.Fleet.locked(func()error{var err error;plan,err=o.readPlan();return err})
 return plan,err
}

// Activate takes an opaque plan ID, not caller-supplied target release IDs.
// Local activation remains separately authorized on every host executor.
func(o *FleetOrchestrator)Activate(ctx context.Context,planID string)(FleetTransaction,error){
 var tx FleetTransaction
 if err:=o.check();err!=nil{return tx,err}
 err:=o.Fleet.locked(func()error{
  plan,err:=o.readPlan();if err!=nil{return err}
  if plan.ID!=planID{return errors.New("fleet plan identity mismatch")}
  tx,err=o.Fleet.startLocked(ctx,plan.Targets)
  return err
 })
 return tx,err
}

func(o *FleetOrchestrator)Recover(ctx context.Context,transactionID string)(FleetTransaction,error){
 if err:=o.check();err!=nil{return FleetTransaction{},err}
 return o.Fleet.Recover(ctx,transactionID)
}

// Clear removes a fully resolved transaction and its immutable plan. A plan
// without a transaction can also be discarded by the trusted operator.
func(o *FleetOrchestrator)Clear(planID string)error{
 if err:=o.check();err!=nil{return err}
 return o.Fleet.locked(func()error{
  plan,err:=o.readPlan();if err!=nil{return err}
  if plan.ID!=planID{return errors.New("fleet plan identity mismatch")}
  tx,err:=o.Fleet.read()
  if err==nil&&tx.Phase!="complete"&&tx.Phase!="reverted"{return errors.New("fleet transaction requires recovery before clearing")}
  if err!=nil&&!errors.Is(err,os.ErrNotExist){return err}
  if err==nil{if err:=os.Remove(o.Fleet.StateFile);err!=nil{return err}}
  if err:=os.Remove(o.PlanFile);err!=nil{return err}
  d,err:=os.Open(filepath.Dir(o.PlanFile));if err!=nil{return err};defer d.Close()
  return d.Sync()
 })
}
