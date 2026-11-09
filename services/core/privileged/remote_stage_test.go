package privileged

import (
 "context"
 "crypto/tls"
 "crypto/x509"
 "encoding/json"
 "net/http"
 "net/http/httptest"
 "os"
 "path/filepath"
 "strings"
 "testing"

 "github.com/dbontr/homelab-new/system/release"
 "github.com/dbontr/homelab-new/system/security"
)

type testStageActuator struct{calls int}
func(*testStageActuator)Activate(context.Context,string)error{return nil}
func(*testStageActuator)Revert(context.Context,string)error{return nil}
func(f *testStageActuator)Stage(_ context.Context,revision string)(release.Manifest,error){
 f.calls++;return release.Manifest{Host:"jupiter",Revision:revision,ID:strings.Repeat("a",48),System:"/nix/store/abc-system"},nil
}
type testStageAuth struct{allowed bool;calls int;args json.RawMessage}
func(f *testStageAuth)Authorize(_ context.Context,op,target string,args json.RawMessage)(security.Authorization,error){
 f.calls++;f.args=args;return security.Authorization{Allowed:f.allowed},nil
}
func TestStageHandlerRejectsUnverifiedTLSAndUnauthorizedStage(t *testing.T){
 revision:=strings.Repeat("b",40)
 auth:=&testStageAuth{}
 stage:=&testStageActuator{}
 auditPath:=filepath.Join(t.TempDir(),"audit.jsonl")
 audit,err:=OpenAuditJournal(auditPath)
 if err!=nil{t.Fatal(err)}
 executor:=&Executor{cfg:Config{Host:"jupiter",AllowRelease:true,Auth:auth,Releases:stage,Audit:audit}}
 body:=`{"host":"jupiter","revision":"`+revision+`"}`
 request:=httptest.NewRequest(http.MethodPost,"/v1/release/stage",strings.NewReader(body))
 request.Header.Set("Cf-Access-Jwt-Assertion","signed-owner")
 response:=httptest.NewRecorder()
 executor.RemoteHandler().ServeHTTP(response,request)
 if response.Code!=403||auth.calls!=0||stage.calls!=0{t.Fatal("unverified TLS bypassed")}
 request=httptest.NewRequest(http.MethodPost,"/v1/release/stage",strings.NewReader(body))
 request.Header.Set("Cf-Access-Jwt-Assertion","signed-owner")
 request.TLS=&tls.ConnectionState{VerifiedChains:[][]*x509.Certificate{{&x509.Certificate{}}},PeerCertificates:[]*x509.Certificate{{}}}
 response=httptest.NewRecorder()
 executor.RemoteHandler().ServeHTTP(response,request)
 if response.Code!=200||stage.calls!=0||auth.calls!=1{t.Fatal("policy denial bypassed",response.Code)}
 auth.allowed=true
 request=httptest.NewRequest(http.MethodPost,"/v1/release/stage",strings.NewReader(body))
 request.Header.Set("Cf-Access-Jwt-Assertion","signed-owner")
 request.TLS=&tls.ConnectionState{VerifiedChains:[][]*x509.Certificate{{&x509.Certificate{}}},PeerCertificates:[]*x509.Certificate{{}}}
 response=httptest.NewRecorder()
 executor.RemoteHandler().ServeHTTP(response,request)
 if response.Code!=200||stage.calls!=1||auth.calls!=2{t.Fatal("approved stage failed",response.Code,response.Body.String())}
 var out struct{Allowed bool;Manifest release.Manifest}
 if err:=json.Unmarshal(response.Body.Bytes(),&out);err!=nil||!out.Allowed||out.Manifest.Revision!=revision{t.Fatal("bad manifest",out,err)}
 journal,err:=os.ReadFile(auditPath)
 if err!=nil{t.Fatal(err)}
 if strings.Count(string(journal),"\n")!=2||
  !strings.Contains(string(journal),`"outcome":"started"`)||
  !strings.Contains(string(journal),`"outcome":"succeeded"`)||
  strings.Contains(string(journal),revision){
  t.Fatalf("remote stage must record bounded results, not raw source arguments: %s",journal)
 }
 // An administrative stage may not run when durable audit custody is absent.
 executor.cfg.Audit=nil
 request=httptest.NewRequest(http.MethodPost,"/v1/release/stage",strings.NewReader(body))
 request.Header.Set("Cf-Access-Jwt-Assertion","signed-owner")
 request.TLS=&tls.ConnectionState{VerifiedChains:[][]*x509.Certificate{{&x509.Certificate{}}},PeerCertificates:[]*x509.Certificate{{}}}
 response=httptest.NewRecorder()
 executor.RemoteHandler().ServeHTTP(response,request)
 if response.Code!=http.StatusServiceUnavailable||stage.calls!=1{
  t.Fatalf("release staging bypassed audit storage: %d",response.Code)
 }
}
