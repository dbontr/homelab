package privileged

import (
 "context"
 "encoding/json"
 "errors"
 "path/filepath"
 "strings"
 "sync"
 "sync/atomic"
 "testing"

 "github.com/dbontr/homelab-new/system/security"
)

type requestReplayAuthorizer struct {
 authorize atomic.Int32
 replay atomic.Int32
 denyReplay bool
}

func (a *requestReplayAuthorizer) Authorize(
 context.Context,string,string,json.RawMessage,
)(security.Authorization,error){
 a.authorize.Add(1)
 return security.Authorization{Allowed:true},nil
}

func (a *requestReplayAuthorizer) Replay(
 context.Context,string,string,json.RawMessage,
)(security.Authorization,error){
 a.replay.Add(1)
 if a.denyReplay{return security.Authorization{Reason:"owner session expired"},nil}
 return security.Authorization{Allowed:true},nil
}

func replayExecutor(t *testing.T,journal *AuditJournal,auth *requestReplayAuthorizer,
 run Run)(*Executor,json.RawMessage){
 t.Helper()
 executor,err:=New(Config{
  Host:"jupiter",Systemctl:"/usr/bin/systemctl",
  AllowedServices:[]string{"platform.service","secondary.service"},
  Auth:auth,Audit:journal,Releases:&fakeRelease{},Run:run,
 })
 if err!=nil{t.Fatal(err)}
 return executor,json.RawMessage(`{"host":"jupiter","service":"platform.service"}`)
}

func TestExecutionRequestIsDurableAcrossRestartAndCannotChangeScope(t *testing.T){
 directory:=privateAuditDir(t)
 path:=filepath.Join(directory,"executor-actions.jsonl")
 journal,err:=OpenAuditJournal(path)
 if err!=nil{t.Fatal(err)}
 auth:=&requestReplayAuthorizer{}
 var calls atomic.Int32
 runner:=func(context.Context,string,...string)error{calls.Add(1);return nil}
 executor,args:=replayExecutor(t,journal,auth,runner)
 id:=strings.Repeat("a",32)
 first,err:=executor.ExecuteWithID(context.Background(),id,
  "infrastructure__service_restart","jupiter",args)
 if err!=nil||!first.Allowed||first.Replayed||first.AuditID!=id||calls.Load()!=1{
  t.Fatalf("first request failed: %+v %v",first,err)
 }
 replay,err:=executor.ExecuteWithID(context.Background(),id,
  "infrastructure__service_restart","jupiter",args)
 if err!=nil||!replay.Allowed||!replay.Replayed||replay.AuditID!=id||calls.Load()!=1{
  t.Fatalf("request was repeated: %+v %v",replay,err)
 }
 wrong:=json.RawMessage(`{"host":"jupiter","service":"secondary.service"}`)
 if _,err:=executor.ExecuteWithID(context.Background(),id,
  "infrastructure__service_restart","jupiter",wrong);err==nil{
  t.Fatal("reused request ID changed its canonical arguments")
 }
 if auth.replay.Load()!=1{t.Fatal("recorded outcome bypassed independent replay authorization")}
 auth.denyReplay=true
 denied,err:=executor.ExecuteWithID(context.Background(),id,
  "infrastructure__service_restart","jupiter",args)
 if err!=nil||denied.Allowed||calls.Load()!=1{
  t.Fatal("revoked owner inherited recorded execution",denied,err)
 }
 auth.denyReplay=false
 if err:=journal.Close();err!=nil{t.Fatal(err)}
 reopened,err:=OpenAuditJournal(path)
 if err!=nil{t.Fatal(err)}
 defer reopened.Close()
 successor,_:=replayExecutor(t,reopened,auth,runner)
 recovered,err:=successor.ExecuteWithID(context.Background(),id,
  "infrastructure__service_restart","jupiter",args)
 if err!=nil||!recovered.Replayed||recovered.AuditID!=id||calls.Load()!=1{
  t.Fatalf("executor restart repeated a completed request: %+v %v",recovered,err)
 }
}

func TestIndeterminateRequestNeverRepeatsExternalSideEffect(t *testing.T){
 auth:=&requestReplayAuthorizer{}
 journal:=testExecutorAudit(t)
 var effects atomic.Int32
 executor,args:=replayExecutor(t,journal,auth,func(context.Context,string,...string)error{
  effects.Add(1)
  return errors.New("remote effect may already have happened")
 })
 id:=strings.Repeat("b",32)
 first,err:=executor.ExecuteWithID(context.Background(),id,
  "infrastructure__service_restart","jupiter",args)
 if err==nil||first.Allowed||effects.Load()!=1{t.Fatal("ambiguous side effect was treated as complete",first,err)}
 second,err:=executor.ExecuteWithID(context.Background(),id,
  "infrastructure__service_restart","jupiter",args)
 if err!=nil||second.Allowed||effects.Load()!=1{
  t.Fatal("indeterminate request was executed twice",second,err)
 }
 if second.Reason==""{t.Fatal("ambiguous outcome was not reported")}
}

func TestConcurrentIdenticalRequestsPerformAtMostOneMutation(t *testing.T){
 auth:=&requestReplayAuthorizer{}
 journal:=testExecutorAudit(t)
 var effects atomic.Int32
 executor,args:=replayExecutor(t,journal,auth,func(context.Context,string,...string)error{
  effects.Add(1)
  return nil
 })
 id:=strings.Repeat("c",32)
 var workers sync.WaitGroup
 results:=make(chan ExecutionResult,12)
 for i:=0;i<12;i++{
  workers.Add(1)
  go func(){
   defer workers.Done()
   output,_:=executor.ExecuteWithID(context.Background(),id,
    "infrastructure__service_restart","jupiter",args)
   results<-output
  }()
 }
 workers.Wait()
 close(results)
 if effects.Load()!=1{t.Fatalf("duplicate effects: %d",effects.Load())}
 accepted:=0
 for result:=range results{if result.Allowed{accepted++}}
 if accepted==0{t.Fatal("no execution admitted")}
}
