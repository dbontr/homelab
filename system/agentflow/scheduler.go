package agentflow

import (
 "context"
 "errors"
 "time"
)

// RunAvailable advances one eligible step from each active engineering run.
// The scheduler has no cluster-administrator credentials. A failed or
// uncertain external mutation is never automatically replayed.
func (e *Engine) RunAvailable(ctx context.Context)(int,error){
 if e==nil||e.Store==nil||e.Compute==nil{return 0,errors.New("engineering service not configured")}
 runs,err:=e.List();if err!=nil{return 0,err}
 processed:=0
 for _,run:=range runs{
  if err:=ctx.Err();err!=nil{return processed,err}
  if run.Status==Succeeded||run.Status==Failed||run.Status==Cancelled{continue}
  runner:=Runner{Store:e.Store,Worker:e,Lease:30*time.Minute}
  advanced,err:=runner.RunOne(ctx,run.ID)
  if err!=nil{return processed,err}
  if advanced{processed++}
 }
 return processed,nil
}

// Serve runs unattended after explicit service configuration. Worker
// operations are serialized; individual jobs cannot claim executor authority.
func (e *Engine) Serve(ctx context.Context){
 ticker:=time.NewTicker(3*time.Second)
 defer ticker.Stop()
 for{
  select {case <-ctx.Done():return;case <-ticker.C:}
  // An error affects only the current pass. Failed steps remain durable
  // for owner inspection; the next pass continues other eligible runs.
  _,_=e.RunAvailable(ctx)
 }
}
