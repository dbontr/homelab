package privileged

import (
 "context"
 "crypto/tls"
 "crypto/x509"
 "encoding/json"
 "errors"
 "net/http"
 "net/http/httptest"
 "strings"
 "testing"

 "github.com/dbontr/homelab-new/system/release"
 "github.com/dbontr/homelab-new/system/security"
)

type remoteFakeAuth struct{
 allowed bool
 calls int
 operation string
 target string
 arguments json.RawMessage
}
func(f *remoteFakeAuth)Authorize(_ context.Context,operation,target string,arguments json.RawMessage)(security.Authorization,error){
 f.calls++;f.operation=operation;f.target=target;f.arguments=arguments
 return security.Authorization{Allowed:f.allowed},nil
}
type remoteFakeRelease struct{journal release.Journal;err error}
func(f *remoteFakeRelease)Activate(context.Context,string)error{return nil}
func(f *remoteFakeRelease)Revert(context.Context,string)error{return nil}
func(f *remoteFakeRelease)Status()(release.Journal,error){return f.journal,f.err}

func TestReleaseStatusRequiresSignedOwnerAndIndependentAuthorization(t *testing.T){
 id:=strings.Repeat("a",48)
 auth:=&remoteFakeAuth{}
 releases:=&remoteFakeRelease{journal:release.Journal{ID:id,Phase:"active"}}
 e:=&Executor{cfg:Config{Host:"jupiter",AllowRelease:true,Auth:auth,Releases:releases}}
 request:=httptest.NewRequest(http.MethodGet,"/v1/release/status",nil)
 response:=httptest.NewRecorder()
 e.RemoteHandler().ServeHTTP(response,request)
 if response.Code!=403||auth.calls!=0{t.Fatal("unsigned read authorized")}
 request.Header.Set("Cf-Access-Jwt-Assertion","signed-owner")
 response=httptest.NewRecorder()
 e.RemoteHandler().ServeHTTP(response,request)
 if response.Code!=403||auth.calls!=0{t.Fatal("unverified TLS client reached authorization")}
 request.TLS=&tls.ConnectionState{
  VerifiedChains:[][]*x509.Certificate{{&x509.Certificate{}}},
  PeerCertificates:[]*x509.Certificate{{}},
 }
 response=httptest.NewRecorder()
 e.RemoteHandler().ServeHTTP(response,request)
 if response.Code!=403||auth.calls!=1{t.Fatal("policy denial bypassed")}
 auth.allowed=true
 response=httptest.NewRecorder()
 e.RemoteHandler().ServeHTTP(response,request)
 if response.Code!=200||auth.operation!="infrastructure__release_status"||auth.target!="jupiter"{t.Fatal(response.Code,auth)}
 var result release.ReleaseObservation
 if err:=json.Unmarshal(response.Body.Bytes(),&result);err!=nil||result.ID!=id||result.Phase!="active"{t.Fatal(result,err)}
 if string(auth.arguments)!=`{"host":"jupiter"}`{t.Fatal("wrong authorization scope",string(auth.arguments))}
 releases.err=errors.New("journal unavailable")
 response=httptest.NewRecorder()
 e.RemoteHandler().ServeHTTP(response,request)
 if response.Code!=503{t.Fatal("unavailable journal reported healthy")}
}

func TestRemoteExecutorCannotBindPublicInterfaces(t *testing.T){
 e:=&Executor{}
 cert:=tls.Certificate{Certificate:[][]byte{[]byte("unused")},PrivateKey:struct{}{}}
 roots:=x509.NewCertPool()
 for _,address:=range []string{"0.0.0.0:9443","[::]:9443","public.example:9443"}{
  if err:=e.ServeMutualTLS(context.Background(),address,cert,roots,"coordinator.internal");err==nil{
   t.Fatal("unsafe remote binding permitted",address)
  }
 }
}
