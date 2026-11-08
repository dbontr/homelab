package security

import (
 "context"
 "encoding/json"
 "path/filepath"
 "strings"
 "sync"
 "sync/atomic"
 "testing"
 "time"
)

func TestScopedCredentialBrokerNeverReturnsSecretAndConsumesOnce(t *testing.T) {
 sessions,key,now:=testSessionManager(t)
 loginAt(t,sessions,key,now)
 vault,err:=OpenVault(filepath.Join(privateDir(t),"vault.json"),[]byte(strings.Repeat("k",32)))
 if err!=nil{t.Fatal(err)}
 handle,err:=vault.Store("vendor",[]byte("private-license-key"))
 if err!=nil{t.Fatal(err)}
 broker,err:=NewScopedCredentialBroker(vault,sessions)
 if err!=nil{t.Fatal(err)}
 args:=json.RawMessage(`{"project_id":"jupiter","operation":"install"}`)
 capability,err:=broker.Prepare("local-operator",handle.ID,"vendor","workspace__workspace_create","jupiter",args,90*time.Second)
 if err!=nil{t.Fatal(err)}
 if strings.Contains(string(mustMarshal(t,capability)),"private-license-key")||strings.Contains(string(mustMarshal(t,capability)),handle.ID){
  t.Fatal("secret or internal credential handle leaked")
 }
 if decision,err:=broker.Consume(context.Background(),"other",capability.ID,capability.Operation,capability.Target,args,func([]byte)error{t.Fatal("cross-principal callback");return nil});err!=nil||decision.Allowed{
  t.Fatal("cross-principal credential use permitted")
 }
 var calls atomic.Int32
 callback:=func(secret []byte)error{
  if string(secret)!="private-license-key"{t.Fatal("wrong credential")}
  calls.Add(1)
  return nil
 }
 decision,err:=broker.Consume(context.Background(),"local-operator",capability.ID,capability.Operation,capability.Target,args,callback)
 if err!=nil||!decision.Allowed||calls.Load()!=1{t.Fatal("authorized credential use failed",err)}
 decision,err=broker.Consume(context.Background(),"local-operator",capability.ID,capability.Operation,capability.Target,args,callback)
 if err!=nil||decision.Allowed||calls.Load()!=1{t.Fatal("replayed credential use succeeded")}
}

func TestScopedCredentialBrokerScopeRevocationAndConcurrency(t *testing.T) {
 sessions,key,now:=testSessionManager(t)
 loginAt(t,sessions,key,now)
 vault,_:=OpenVault(filepath.Join(privateDir(t),"vault.json"),[]byte(strings.Repeat("k",32)))
 handle,err:=vault.Store("vendor",[]byte("private"))
 if err!=nil{t.Fatal(err)}
 broker,_:=NewScopedCredentialBroker(vault,sessions)
 args:=json.RawMessage(`{"project_id":"jupiter"}`)
 c,err:=broker.Prepare("local-operator",handle.ID,"vendor","workspace__workspace_create","jupiter",args,time.Minute)
 if err!=nil{t.Fatal(err)}
 if d,_:=broker.Consume(context.Background(),"local-operator",c.ID,c.Operation,"venus",args,func([]byte)error{return nil});d.Allowed{
  t.Fatal("resource substitution accepted")
 }
 if d,_:=broker.Consume(context.Background(),"local-operator",c.ID,c.Operation,c.Target,json.RawMessage(`{"project_id":"venus"}`),func([]byte)error{return nil});d.Allowed{
  t.Fatal("argument substitution accepted")
 }
 var used atomic.Int32
 var wg sync.WaitGroup
 for i:=0;i<16;i++{
  wg.Add(1)
  go func(){defer wg.Done()
   _,_ = broker.Consume(context.Background(),"local-operator",c.ID,c.Operation,c.Target,args,func([]byte)error{used.Add(1);return nil})
  }()
 }
 wg.Wait()
 if used.Load()!=1{t.Fatalf("used credential %d times",used.Load())}
 c,err=broker.Prepare("local-operator",handle.ID,"vendor","workspace__workspace_create","jupiter",args,time.Minute)
 if err!=nil{t.Fatal(err)}
 sessions.Revoke()
 if d,_:=broker.Consume(context.Background(),"local-operator",c.ID,c.Operation,c.Target,args,func([]byte)error{t.Fatal("revoked session executed");return nil});d.Allowed{
  t.Fatal("revoked owner session retained access")
 }
 if !broker.Revoke(c.ID){t.Fatal("lease could not be revoked")}
}

func mustMarshal(t *testing.T,value any)[]byte{
 t.Helper();result,err:=json.Marshal(value);if err!=nil{t.Fatal(err)};return result
}
