package agentflow

import (
 "context"
 "encoding/json"
 "errors"
 "fmt"
 "time"
)

// Dispatcher performs a named, pre-authorized operation in an isolated worker.
// Implementations must deduplicate calls by the stable idempotency key, because
// an expired lease can be reclaimed after the worker has performed its effect.
type Dispatcher interface {
 Dispatch(context.Context, string, string, json.RawMessage) (json.RawMessage, error)
}
type Runner struct {
 Store *Store
 Worker Dispatcher
 Lease time.Duration
}
// RunOne claims at most one ready step and records its confirmed outcome.
// A cancelled context is propagated, but the lease remains recoverable if
// the worker cannot confirm the result before the deadline.
func (r Runner) RunOne(ctx context.Context, workflowID string)(bool,error){
 if r.Store==nil||r.Worker==nil{return false,errors.New("runner requires store and dispatcher")}
 if err:=ctx.Err();err!=nil{return false,err}
 claim,err:=r.Store.ClaimReady(workflowID,r.Lease)
 if errors.Is(err,errNoReady){return false,nil}
 if err!=nil{return false,err}
 duration:=time.Until(claim.Deadline)
 if duration<=0{return true,errors.New("lease expired before dispatch")}
 active,cancel:=context.WithTimeout(ctx,duration)
 defer cancel()
 idempotencyKey:=fmt.Sprintf("%s/%s",claim.WorkflowID,claim.StepID)
 result,runErr:=r.Worker.Dispatch(active,idempotencyKey,claim.Kind,claim.Input)
 if active.Err()!=nil{return true,active.Err()}
 if runErr!=nil{
  detail:=runErr.Error()
  if len(detail)>1024{detail=detail[:1024]}
  if err:=r.Store.Complete(claim,nil,detail);err!=nil{return true,err}
  return true,nil
 }
 if err:=r.Store.Complete(claim,result,"");err!=nil{return true,err}
 return true,nil
}
