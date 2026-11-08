package release

import (
 "context"
 "errors"
 "fmt"
 "regexp"
 "sync"
 "time"
)

// HostReleaseAgent is implemented by an independently authenticated host
// executor. The coordinator never receives host administrator credentials.
type HostReleaseAgent interface {
 Activate(context.Context,string)error
 Revert(context.Context,string)error
}

var fleetHostPattern = regexp.MustCompile("^[a-z][a-z0-9-]{0,62}$")

type FleetTarget struct {
 Host string `json:"host"`
 Role string `json:"role"`
 ReleaseID string `json:"release_id"`
}
type FleetStep struct {
 Host string `json:"host"`
 Role string `json:"role"`
 ReleaseID string `json:"release_id"`
 State string `json:"state"`
 Error string `json:"error,omitempty"`
}
type FleetResult struct {
 Steps []FleetStep `json:"steps"`
 Complete bool `json:"complete"`
 RolledBack bool `json:"rolled_back"`
 AttentionRequired bool `json:"attention_required"`
}

type FleetCoordinator struct {
 mu sync.Mutex
 Agents map[string]HostReleaseAgent
}

func validateFleet(targets []FleetTarget,agents map[string]HostReleaseAgent)error{
 if len(targets)==0||len(targets)>16{return errors.New("fleet must contain 1..16 hosts")}
 seen:=map[string]bool{}
 controlSeen:=false
 for i,target:=range targets{
  if !fleetHostPattern.MatchString(target.Host)||seen[target.Host]||!idPattern.MatchString(target.ReleaseID)||
   (target.Role!="compute"&&target.Role!="control"){
   return errors.New("invalid host, role, or release identity")
  }
  if target.Role=="control"{
   if controlSeen||i!=len(targets)-1{return errors.New("control plane must be a single final rollout target")}
   controlSeen=true
  }
  if agents[target.Host]==nil{return fmt.Errorf("host %s has no authorized release agent",target.Host)}
  seen[target.Host]=true
 }
 return nil
}

// Activate runs a serial rolling release. Each host agent is responsible for
// exact-generation authorization, local health checks, and rollback on failure.
// If any host fails, completed hosts are reverted in reverse order.
func(c *FleetCoordinator) Activate(ctx context.Context,targets []FleetTarget)(FleetResult,error){
 var result FleetResult
 if c==nil{return result,errors.New("fleet coordinator not configured")}
 c.mu.Lock()
 defer c.mu.Unlock()
 if err:=validateFleet(targets,c.Agents);err!=nil{return result,err}
 result.Steps=make([]FleetStep,len(targets))
 for i,target:=range targets{
  result.Steps[i]=FleetStep{Host:target.Host,Role:target.Role,ReleaseID:target.ReleaseID,State:"pending"}
 }
 completed:=0
 var activationErr error
 for i,target:=range targets{
  if err:=ctx.Err();err!=nil{activationErr=err;break}
  if err:=c.Agents[target.Host].Activate(ctx,target.ReleaseID);err!=nil{
   result.Steps[i].State="unknown"
   result.Steps[i].Error="host activation failed; inspect local release journal"
   result.AttentionRequired=true
   activationErr=fmt.Errorf("host %s activation failed: %w",target.Host,err)
   break
  }
  result.Steps[i].State="active"
  completed++
 }
 if activationErr==nil{
  result.Complete=true
  return result,nil
 }
 // Owner cancellation does not interrupt recovery on hosts already activated.
 cleanup,cancel:=context.WithTimeout(context.Background(),3*time.Minute)
 defer cancel()
 restored:=true
 for i:=completed-1;i>=0;i--{
  target:=targets[i]
  if err:=c.Agents[target.Host].Revert(cleanup,target.ReleaseID);err!=nil{
   restored=false
   result.Steps[i].State="revert-failed"
   result.Steps[i].Error="host rollback failed; inspect local release journal"
   result.AttentionRequired=true
  }else{result.Steps[i].State="reverted"}
 }
 result.RolledBack=restored
 if !restored{return result,fmt.Errorf("%w; fleet rollback incomplete",activationErr)}
 return result,activationErr
}
