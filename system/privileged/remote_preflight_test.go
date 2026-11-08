package privileged

import (
 "context"
 "crypto/tls"
 "crypto/x509"
 "encoding/json"
 "net/http"
 "net/http/httptest"
 "strings"
 "testing"

 "github.com/dbontr/homelab-new/system/security"
)

type previewReleaseFake struct{verified bool;executions int}
func(f *previewReleaseFake)Activate(context.Context,string)error{f.executions++;return nil}
func(f *previewReleaseFake)Revert(context.Context,string)error{f.executions++;return nil}
func(f *previewReleaseFake)VerifyStaged(id string)error{
 if !f.verified{return context.Canceled}
 return nil
}
type previewAuthFake struct{calls int;approved bool;operation string;target string;args json.RawMessage}
func(f *previewAuthFake)Authorize(context.Context,string,string,json.RawMessage)(security.Authorization,error){
 tbd:=security.Authorization{Reason:"execution authorization must not be invoked during preview"}
 return tbd,nil
}
func(f *previewAuthFake)Preview(_ context.Context,op,target string,args json.RawMessage)(security.Authorization,error){
 f.calls++;f.operation=op;f.target=target;f.args=args
 return security.Authorization{Allowed:f.approved,Reason:"confirmation required",ConfirmationID:"challenge"},nil
}
func TestHostReleasePreflightIsReadOnlyAndScopeBound(t *testing.T){
 id:=strings.Repeat("a",48)
 auth:=&previewAuthFake{}
 releases:=&previewReleaseFake{verified:true}
 e:=&Executor{cfg:Config{Host:"jupiter",AllowRelease:true,Auth:auth,Releases:releases}}
 request:=func(body string,verifiedTLS bool)*httptest.ResponseRecorder{
  req:=httptest.NewRequest(http.MethodPost,"/v1/release/preview",strings.NewReader(body))
  req.Header.Set("Cf-Access-Jwt-Assertion","signed-owner")
  if verifiedTLS{
   req.TLS=&tls.ConnectionState{PeerCertificates:[]*x509.Certificate{{}},
    VerifiedChains:[][]*x509.Certificate{{&x509.Certificate{}}}}
  }
  w:=httptest.NewRecorder();e.RemoteHandler().ServeHTTP(w,req);return w
 }
 valid:=`{"host":"jupiter","release_id":"`+id+`","action":"activate"}`
 if r:=request(valid,false);r.Code!=403||auth.calls!=0{t.Fatal("unverified TLS was accepted")}
 if r:=request(strings.Replace(valid,"jupiter","saturn",1),true);r.Code!=403||auth.calls!=0{t.Fatal("wrong host accepted")}
 if r:=request(valid,true);r.Code!=200||auth.calls!=1||releases.executions!=0{
  t.Fatal("preflight executed a host mutation",r.Code,r.Body.String())
 }
 if auth.operation!="infrastructure__release_activate"||auth.target!="jupiter"||
  string(auth.args)!=`{"host":"jupiter","release_id":"`+id+`"}`{
  t.Fatal("preflight altered authorization scope",auth)
 }
 releases.verified=false
 if r:=request(valid,true);r.Code!=403||auth.calls!=1{t.Fatal("unstaged release created a confirmation")}
}
