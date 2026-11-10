package privileged

import (
 "context"
 "encoding/base32"
 "encoding/json"
 "os"
 "path/filepath"
 "strings"
 "sync"
 "sync/atomic"
 "testing"
 "time"

 "github.com/dbontr/homelab-new/system/security"
)

const ownerAcceptancePassphrase = "verified-administrator-passphrase"

type ownerSessionAuthorizer struct { sessions *security.AdminSessions }

func(a ownerSessionAuthorizer)Authorize(_ context.Context,operation,target string,args json.RawMessage)(security.Authorization,error){
 return a.sessions.Authorize("platform-gateway",operation,target,args),nil
}

func newOwnerApprovedExecutor(t *testing.T)(*Executor,*security.AdminSessions,[]byte,*atomic.Int32){
 t.Helper()
 password,err:=security.HashPassphrase([]byte(ownerAcceptancePassphrase))
 if err!=nil{t.Fatal(err)}
 key:=[]byte("12345678901234567890")
 manager,err:=security.NewAdminSessions(security.SessionConfig{
  Owner:"operator",Client:"platform-gateway",Verifier:password,
  TOTPSecret:base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(key),
  StateFile:filepath.Join(privateAuditDir(t),"authenticator.json"),
  Policies:[]security.OperationPolicy{{
   Operation:"infrastructure__service_restart",Targets:[]string{"jupiter"},Destructive:true,
  }},
 })
 if err!=nil{t.Fatal(err)}
 calls:=new(atomic.Int32)
 executor,err:=New(Config{
  Host:"jupiter",Systemctl:"/usr/bin/systemctl",
  AllowedServices:[]string{"homelab-platform.service","homelab-bridge.service"},
  Auth:ownerSessionAuthorizer{sessions:manager},Releases:&fakeRelease{},
  Audit:testExecutorAudit(t),
  Run:func(_ context.Context,path string,args ...string)error{
   if path!="/usr/bin/systemctl"||len(args)!=2||args[0]!="restart"||args[1]!="homelab-platform.service"{
    t.Errorf("unauthorized command reached system runner: %q %v",path,args)
   }
   calls.Add(1)
   return nil
  },
 })
 if err!=nil{t.Fatal(err)}
 return executor,manager,key,calls
}

func ownerLogin(t *testing.T,sessions *security.AdminSessions,key []byte)string{
 t.Helper()
 // The next counter is valid under RFC 6238 skew rules but cannot replay the
 // counter used during login. The confirmation remains a distinct owner action.
 step:=time.Now().Unix()/30
 if _,err:=sessions.Authenticate(ownerAcceptancePassphrase,security.TOTPCode(key,step));err!=nil{
  t.Fatalf("owner login rejected: %v",err)
 }
 return security.TOTPCode(key,step+1)
}

func TestPrivilegedOperationRequiresBoundConfirmationAndRevocation(t *testing.T){
 executor,sessions,key,calls:=newOwnerApprovedExecutor(t)
 original:=json.RawMessage(`{"host":"jupiter","service":"homelab-platform.service"}`)
 other:=json.RawMessage(`{"host":"jupiter","service":"homelab-bridge.service"}`)
 operation:="infrastructure__service_restart"
 requireDenied:=func(args json.RawMessage)ExecutionResult{
  t.Helper()
  result,err:=executor.Execute(context.Background(),operation,"jupiter",args)
  if err!=nil||result.Allowed||calls.Load()!=0{
   t.Fatalf("operation executed before owner approval: %+v err=%v calls=%d",result,err,calls.Load())
  }
  return result
 }
 requireDenied(original)
 confirmCode:=ownerLogin(t,sessions,key)
 challenge:=requireDenied(original)
 if challenge.ConfirmationID==""{t.Fatal("unconfirmed action has no owner challenge")}
 if err:=sessions.Confirm(challenge.ConfirmationID,ownerAcceptancePassphrase,confirmCode);err!=nil{
  t.Fatalf("separate owner confirmation failed: %v",err)
 }
 requireDenied(other)
 // Exact action, target, and canonical argument digest identify the consent.
 result,err:=executor.Execute(context.Background(),operation,"jupiter",original)
 if err!=nil||!result.Allowed||result.State!=ExecutionAccepted||len(result.AuditID)!=32||calls.Load()!=1{
  t.Fatalf("confirmed action was not executed exactly once: %+v err=%v calls=%d",result,err,calls.Load())
 }
 replay,err:=executor.Execute(context.Background(),operation,"jupiter",original)
 if err!=nil||replay.Allowed||calls.Load()!=1{
  t.Fatalf("confirmed challenge was reusable: %+v err=%v",replay,err)
 }
 sessions.Revoke()
 revoked,err:=executor.Execute(context.Background(),operation,"jupiter",original)
 if err!=nil||revoked.Allowed||calls.Load()!=1{
  t.Fatalf("revoked owner session retained execution authority: %+v err=%v",revoked,err)
 }
 records,err:=os.ReadFile(executor.cfg.Audit.path)
 if err!=nil{t.Fatal(err)}
 if strings.Count(string(records),"\n")!=2||strings.Contains(string(records),ownerAcceptancePassphrase)||
  !strings.Contains(string(records),`"outcome":"started"`)||
  !strings.Contains(string(records),`"outcome":"accepted"`){
  t.Fatal("audit trail was incomplete or contained an owner secret")
 }
}

func TestConcurrentPrivilegedRequestsConsumeOneConfirmation(t *testing.T){
 executor,sessions,key,calls:=newOwnerApprovedExecutor(t)
 confirmation:=ownerLogin(t,sessions,key)
 args:=json.RawMessage(`{"host":"jupiter","service":"homelab-platform.service"}`)
 const operation="infrastructure__service_restart"
 challenge,err:=executor.Execute(context.Background(),operation,"jupiter",args)
 if err!=nil||challenge.ConfirmationID==""{t.Fatal("missing owner confirmation",err)}
 if err:=sessions.Confirm(challenge.ConfirmationID,ownerAcceptancePassphrase,confirmation);err!=nil{t.Fatal(err)}
 var wg sync.WaitGroup
 var succeeded atomic.Int32
 for n:=0;n<24;n++{
  wg.Add(1)
  go func(){
   defer wg.Done()
   response,err:=executor.Execute(context.Background(),operation,"jupiter",args)
   if err!=nil{t.Errorf("executor returned an internal error: %v",err);return}
   if response.Allowed{succeeded.Add(1)}
  }()
 }
 wg.Wait()
 if succeeded.Load()!=1||calls.Load()!=1{
  t.Fatalf("one owner confirmation permitted %d executions and %d side effects",succeeded.Load(),calls.Load())
 }
}
