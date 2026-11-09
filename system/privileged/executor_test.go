package privileged

import (
 "context"
 "encoding/json"
 "errors"
 "net/http"
 "net/http/httptest"
 "strings"
 "testing"

 "github.com/dbontr/homelab-new/system/security"
)

type fakeAuth struct{calls int;decision security.Authorization;err error}
func(f *fakeAuth) Authorize(_ context.Context,_,_ string,_ json.RawMessage)(security.Authorization,error){
 f.calls++
 return f.decision,f.err
}
type fakeRelease struct{calls int;id string}
func(f *fakeRelease) Activate(_ context.Context,id string)error{f.calls++;f.id=id;return nil}
func(f *fakeRelease) Revert(_ context.Context,id string)error{f.calls++;f.id=id;return nil}

func TestExecutorDoesNotAcceptArbitraryCommandsOrTargets(t *testing.T){
 auth:=&fakeAuth{decision:security.Authorization{Allowed:true}}
 rel:=&fakeRelease{}
 calls:=0
 executor,err:=New(Config{Audit:testExecutorAudit(t),
  Host:"jupiter",Systemctl:"/run/current-system/sw/bin/systemctl",
  AllowedServices:[]string{"homelab-platform.service"},Auth:auth,Releases:rel,
  Run:func(_ context.Context,_ string,args ...string)error{
   calls++
   if len(args)!=2||args[0]!="restart"||args[1]!="homelab-platform.service"{t.Fatal("wrong fixed argv",args)}
   return nil
  },
 })
 if err!=nil{t.Fatal(err)}
 for _,tc:=range []struct{op,target,args string}{
  {"shell.run","jupiter",`{"command":"rm -rf /"}`},
  {"infrastructure__service_restart","saturn",`{"host":"saturn","service":"homelab-platform.service"}`},
  {"infrastructure__service_restart","jupiter",`{"host":"jupiter","service":"sshd.service"}`},
  {"infrastructure__service_restart","jupiter",`{"host":"jupiter","service":"homelab-platform.service","command":"sh"}`},
  {"infrastructure__service_restart","jupiter",`{"host":"jupiter","service":"homelab-platform.service","host":"saturn"}`},
 }{
  v,e:=executor.Execute(context.Background(),tc.op,tc.target,json.RawMessage(tc.args))
  if e!=nil||v.Allowed{t.Fatal("forbidden action accepted",tc,v,e)}
 }
 if calls!=0||auth.calls!=0{t.Fatal("unauthorized call reached executor")}
 valid:=json.RawMessage(`{"host":"jupiter","service":"homelab-platform.service"}`)
 decision,err:=executor.Execute(context.Background(),"infrastructure__service_restart","jupiter",valid)
 if err!=nil||!decision.Allowed||calls!=1||auth.calls!=1{t.Fatal("approved typed restart failed",decision,err)}
}

func TestExecutorRequiresIndependentApprovalBeforeRelease(t *testing.T){
 auth:=&fakeAuth{decision:security.Authorization{Reason:"confirmation required",ConfirmationID:"pending"}}
 rel:=&fakeRelease{}
 executor,err:=New(Config{Audit:testExecutorAudit(t),
  Host:"jupiter",Systemctl:"/usr/bin/systemctl",AllowRelease:true,Auth:auth,Releases:rel,
  Run:func(context.Context,string,...string)error{t.Fatal("unexpected system command");return nil},
 })
 if err!=nil{t.Fatal(err)}
 id:=strings.Repeat("a",48)
 args:=json.RawMessage(`{"host":"jupiter","release_id":"`+id+`"}`)
 decision,err:=executor.Execute(context.Background(),"infrastructure__release_activate","jupiter",args)
 if err!=nil||decision.Allowed||rel.calls!=0{t.Fatal("unconfirmed release executed")}
 auth.decision=security.Authorization{Allowed:true}
 decision,err=executor.Execute(context.Background(),"infrastructure__release_activate","jupiter",args)
 if err!=nil||!decision.Allowed||rel.calls!=1||rel.id!=id{t.Fatal("confirmed release not activated",decision,err)}
 auth.err=errors.New("securityd offline")
 decision,err=executor.Execute(context.Background(),"infrastructure__release_activate","jupiter",args)
 if err==nil||decision.Allowed||rel.calls!=1{t.Fatal("authorization outage did not fail closed")}
}

func TestExecutorSocketRequiresOwnerAssertion(t *testing.T){
 auth:=&fakeAuth{decision:security.Authorization{Allowed:true}}
 executor,err:=New(Config{Audit:testExecutorAudit(t),
  Host:"jupiter",Systemctl:"/usr/bin/systemctl",Auth:auth,Releases:&fakeRelease{},
  Run:func(context.Context,string,...string)error{return nil},
 })
 if err!=nil{t.Fatal(err)}
 r:=httptest.NewRequest("POST","/v1/execute",strings.NewReader(`{"operation":"infrastructure__release_activate","target":"jupiter","arguments":{"host":"jupiter","release_id":"`+strings.Repeat("a",48)+`"}}`))
 w:=httptest.NewRecorder()
 executor.Handler().ServeHTTP(w,r)
 if w.Code!=http.StatusForbidden||auth.calls!=0{t.Fatal("unsigned owner call accepted")}
}
