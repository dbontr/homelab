package agentflow

import (
 "context"
 "encoding/json"
 "errors"
 "testing"
 "time"
)

type recordingDispatcher struct {
 calls []string
 fail bool
}
func (d *recordingDispatcher) Dispatch(ctx context.Context,key,kind string,input json.RawMessage)(json.RawMessage,error){
 d.calls=append(d.calls,key+":"+kind)
 if d.fail{return nil,errors.New("worker rejected task")}
 return json.RawMessage(`{"confirmed":true}`),nil
}
func TestRunnerExecutesDependenciesAndDoesNotRepeatCompletedWork(t *testing.T){
 store:=testStore(t)
 if err:=store.Create(sample());err!=nil{t.Fatal(err)}
 worker:=&recordingDispatcher{}
 r:=Runner{Store:store,Worker:worker,Lease:time.Minute}
 for i:=0;i<2;i++{
  progressed,err:=r.RunOne(context.Background(),"engineering-1")
  if err!=nil||!progressed{t.Fatalf("step %d: %v, %v",i,progressed,err)}
 }
 progressed,err:=r.RunOne(context.Background(),"engineering-1")
 if err!=nil||progressed{t.Fatalf("expected completed workflow: %v, %v",progressed,err)}
 if len(worker.calls)!=2||worker.calls[0]!="engineering-1/build:workspace_command"||worker.calls[1]!="engineering-1/review:workspace_command"{t.Fatalf("unexpected worker dispatch: %v",worker.calls)}
}
func TestRunnerRetriesFailureWithStableIdempotencyKey(t *testing.T){
 store:=testStore(t)
 if err:=store.Create(sample());err!=nil{t.Fatal(err)}
 worker:=&recordingDispatcher{fail:true}
 r:=Runner{Store:store,Worker:worker,Lease:time.Minute}
 for i:=0;i<2;i++{
  progressed,err:=r.RunOne(context.Background(),"engineering-1")
  if err!=nil||!progressed{t.Fatalf("attempt %d: %v",i,err)}
 }
 if len(worker.calls)!=2||worker.calls[0]!=worker.calls[1]{t.Fatalf("idempotency key not stable: %v",worker.calls)}
 w,err:=store.Get("engineering-1");if err!=nil{t.Fatal(err)}
 if w.Steps[0].Status!=Failed{t.Fatalf("expected failure: %+v",w.Steps[0])}
}
