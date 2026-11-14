package security

import (
 "context"
 "crypto/rand"
 "crypto/rsa"
 "crypto/tls"
 "crypto/x509"
 "crypto/x509/pkix"
 "encoding/json"
 "math/big"
 "net/http"
 "net/http/httptest"
 "strings"
 "sync/atomic"
 "testing"
 "time"
)

func testRemoteCert(t *testing.T,ca *x509.Certificate,caKey *rsa.PrivateKey,dns string,server bool)tls.Certificate{
 t.Helper()
 key,err:=rsa.GenerateKey(rand.Reader,2048);if err!=nil{t.Fatal(err)}
 usage:=x509.ExtKeyUsageClientAuth;if server{usage=x509.ExtKeyUsageServerAuth}
 now:=time.Now()
 tmpl:=&x509.Certificate{SerialNumber:big.NewInt(now.UnixNano()),DNSNames:[]string{dns},
  NotBefore:now.Add(-time.Hour),NotAfter:now.Add(time.Hour),KeyUsage:x509.KeyUsageDigitalSignature,
  ExtKeyUsage:[]x509.ExtKeyUsage{usage}}
 der,err:=x509.CreateCertificate(rand.Reader,tmpl,ca,&key.PublicKey,caKey)
 if err!=nil{t.Fatal(err)}
 return tls.Certificate{Certificate:[][]byte{der,ca.Raw},PrivateKey:key}
}

func remoteAuthFixture(t *testing.T,handler http.Handler)(RemoteAuthorizationConfig,tls.Certificate,*x509.CertPool,func()){
 t.Helper()
 key,err:=rsa.GenerateKey(rand.Reader,2048);if err!=nil{t.Fatal(err)}
 now:=time.Now()
 caTmpl:=&x509.Certificate{SerialNumber:big.NewInt(1),Subject:pkix.Name{CommonName:"test-ca"},NotBefore:now.Add(-time.Hour),
  NotAfter:now.Add(time.Hour),IsCA:true,BasicConstraintsValid:true,KeyUsage:x509.KeyUsageCertSign|x509.KeyUsageDigitalSignature}
 der,err:=x509.CreateCertificate(rand.Reader,caTmpl,caTmpl,&key.PublicKey,key)
 if err!=nil{t.Fatal(err)}
 ca,err:=x509.ParseCertificate(der);if err!=nil{t.Fatal(err)}
 roots:=x509.NewCertPool();roots.AddCert(ca)
 serverCert:=testRemoteCert(t,ca,key,"security.internal",true)
 clientCert:=testRemoteCert(t,ca,key,"jupiter.internal",false)
 srv:=httptest.NewUnstartedServer(handler)
 srv.TLS=&tls.Config{MinVersion:tls.VersionTLS13,ClientAuth:tls.RequireAndVerifyClientCert,ClientCAs:roots,
  Certificates:[]tls.Certificate{serverCert}}
 srv.StartTLS()
 return RemoteAuthorizationConfig{Endpoint:srv.URL,ServerName:"security.internal",TrustRoots:roots,
  Certificate:clientCert},serverCert,roots,srv.Close
}

func TestRemoteAuthorizerRequiresVerifiedIdentityAndExactArguments(t *testing.T){
 operation:="infrastructure__release_activate"
 id:=strings.Repeat("a",48)
 calls:=0
 cfg,_,_,closeServer:=remoteAuthFixture(t,http.HandlerFunc(func(w http.ResponseWriter,r *http.Request){
  calls++
  if r.TLS==nil||len(r.TLS.VerifiedChains)==0||r.Header.Get("Cf-Access-Jwt-Assertion")!="signed-owner"{
   t.Error("unverified request");w.WriteHeader(403);return
  }
  var request struct{
   Operation string `json:"operation"`
   Target string `json:"target"`
   Arguments json.RawMessage `json:"arguments"`
  }
  if err:=json.NewDecoder(r.Body).Decode(&request);err!=nil{t.Error(err);w.WriteHeader(400);return}
  if request.Operation!=operation||request.Target!="jupiter"||
   string(request.Arguments)!=`{"host":"jupiter","release_id":"`+id+`"}`{
   t.Error("authorization scope changed",request);w.WriteHeader(403);return
  }
  w.Header().Set("Content-Type","application/json")
  _=json.NewEncoder(w).Encode(Authorization{Allowed:true})
 }))
 defer closeServer()
 client,err:=NewRemoteAuthorizationClient(cfg);if err!=nil{t.Fatal(err)}
 args:=json.RawMessage(`{"host":"jupiter","release_id":"`+id+`"}`)
 if _,err:=client.Authorize(context.Background(),operation,"jupiter",args);err==nil||calls!=0{
  t.Fatal("missing owner assertion accepted")
 }
 decision,err:=client.Authorize(WithAccessAssertion(context.Background(),"signed-owner"),operation,"jupiter",args)
 if err!=nil||!decision.Allowed||calls!=1{t.Fatal("authorized client failed",decision,err)}
 cfg.TrustRoots=x509.NewCertPool()
 client,err=NewRemoteAuthorizationClient(cfg);if err!=nil{t.Fatal(err)}
 if decision,err:=client.Authorize(WithAccessAssertion(context.Background(),"signed-owner"),operation,"jupiter",args);
  err==nil||decision.Allowed{t.Fatal("untrusted security service accepted")}
 cfg.ServerName="*.internal"
 if _,err:=NewRemoteAuthorizationClient(cfg);err==nil{t.Fatal("wildcard authorization server identity accepted")}
 cfg.ServerName="127.0.0.1"
 if _,err:=NewRemoteAuthorizationClient(cfg);err==nil{t.Fatal("IP-literal authorization server identity accepted")}
 cfg.ServerName="security.internal"
 cfg.Endpoint="http://127.0.0.1:1234"
 if _,err:=NewRemoteAuthorizationClient(cfg);err==nil{t.Fatal("insecure authorization transport accepted")}
}

func TestSecurityAuthorizationServerRejectsPublicListener(t *testing.T){
 _,serverCert,roots,closeServer:=remoteAuthFixture(t,http.HandlerFunc(func(http.ResponseWriter,*http.Request){}))
 defer closeServer()
 manager:=&AdminSessions{}
 if err:=manager.ServeAuthorizationTLS(context.Background(),"0.0.0.0:443",serverCert,roots,[]string{"jupiter.internal"},nil);err==nil{
  t.Fatal("wildcard security listener accepted")
 }
}

func TestRemoteAuthorizationEnforcesEntireResponseBoundary(t *testing.T){
 var mode atomic.Int32
 const valid=`{"allowed":true}`
 cfg,_,_,closeServer:=remoteAuthFixture(t,http.HandlerFunc(func(w http.ResponseWriter,r *http.Request){
  w.Header().Set("Content-Type","application/json")
  switch mode.Load(){
  case 0:
   _,_=w.Write([]byte(valid+strings.Repeat(" ",8192-len(valid))))
  case 1:
   _,_=w.Write([]byte(valid+strings.Repeat(" ",8193-len(valid))))
  case 2:
   w.Header().Set("X-Oversized-Authorization-Header",strings.Repeat("x",17<<10))
   _,_=w.Write([]byte(valid))
  case 3:
   _,_=w.Write([]byte(valid+valid))
  }
 }))
 defer closeServer()
 client,err:=NewRemoteAuthorizationClient(cfg)
 if err!=nil{t.Fatal(err)}
 ctx:=WithAccessAssertion(context.Background(),"signed-owner")
 for _,tc:=range []struct{name string;mode int32;accepted bool}{
  {"exact bound",0,true},{"oversized valid prefix",1,false},
  {"oversized upstream header",2,false},{"multiple JSON objects",3,false},
 }{
  t.Run(tc.name,func(t *testing.T){
   mode.Store(tc.mode)
   decision,err:=client.Authorize(ctx,"infrastructure__service_restart","jupiter",json.RawMessage(`{"host":"jupiter"}`))
   if tc.accepted{
    if err!=nil||!decision.Allowed{t.Fatalf("valid bounded response rejected: %+v %v",decision,err)}
   }else if err==nil||decision.Allowed{
    t.Fatalf("malformed or oversized response admitted: %+v %v",decision,err)
   }
  })
 }
}

func TestRemoteCredentialCapabilitiesHaveExactResponseShape(t *testing.T){
 var response string
 cfg,_,_,stop:=remoteAuthFixture(t,http.HandlerFunc(func(w http.ResponseWriter,r *http.Request){
  w.Header().Set("Content-Type","application/json")
  _,_=w.Write([]byte(response))
 }))
 defer stop()
 client,err:=NewRemoteAuthorizationClient(cfg)
 if err!=nil{t.Fatal(err)}
 ctx:=WithAccessAssertion(context.Background(),"signed-owner")
 operation:="workspace__workspace_create"
 args:=json.RawMessage(`{"host":"jupiter"}`)
 ticket:=strings.Repeat("a",48)
 response=`{"allowed":true,"credential_tickets":[{"alias":"workspace","ticket":"`+ticket+`"}]}`
 allowed,err:=client.Authorize(ctx,operation,"jupiter",args)
 if err!=nil||!allowed.Allowed||len(allowed.CredentialTickets)!=1{
  t.Fatal("valid scoped capability was refused",err)
 }
 for _,invalid:=range []string{
  `{"allowed":false,"reason":"denied","credential_tickets":[{"alias":"workspace","ticket":"`+ticket+`"}]}`,
  `{"allowed":true,"credential_tickets":[{"alias":"workspace","ticket":"malformed"}]}`,
  `{"allowed":true,"credential_tickets":[{"alias":"workspace","ticket":"`+ticket+`"},{"alias":"workspace","ticket":"`+ticket+`"}]}`,
  `{"allowed":true,"reason":"denied","credential_tickets":[{"alias":"workspace","ticket":"`+ticket+`"}]}`,
 }{
  response=invalid
  if _,err:=client.Authorize(ctx,operation,"jupiter",args);err==nil{
   t.Fatal("malformed credential capability was accepted")
  }
 }
 response=`{"allowed":true,"credential_tickets":[{"alias":"workspace","ticket":"`+ticket+`"}]}`
 if _,err:=client.Preview(ctx,operation,"jupiter",args);err==nil{
  t.Fatal("credential capability was issued for a nonconsuming preview")
 }
}

func TestRemoteTLSClientRolesAreDisjoint(t *testing.T){
 allowed,authorizers,err:=remotePeerRoles(
  []string{"executor.internal"},
  []string{"media.internal","compute.internal"},
 )
 if err!=nil{t.Fatal(err)}
 if len(allowed)!=3||len(authorizers)!=1{
  t.Fatal("remote TLS role sets have incorrect membership")
 }
 if _,ok:=allowed["media.internal"];!ok{t.Fatal("credential-only client denied TLS access")}
 if _,ok:=authorizers["media.internal"];ok{t.Fatal("credential-only client admitted as an authorizer")}
 if _,ok:=authorizers["executor.internal"];!ok{t.Fatal("executor lost operation authorization")}
 for _,tc:=range []struct{name string;auth,credentials []string}{
  {"missing authorizer",nil,[]string{"media.internal"}},
  {"overlapping role",[]string{"executor.internal"},[]string{"executor.internal"}},
  {"duplicate authorizer",[]string{"executor.internal","executor.internal"},nil},
  {"duplicate credential",[]string{"executor.internal"},[]string{"media.internal","media.internal"}},
  {"wildcard credential",[]string{"executor.internal"},[]string{"*.internal"}},
  {"unbounded peers",[]string{"executor.internal"},[]string{"a","b","c","d","e","f","g","h","i","j","k","l","m","n","o","p"}},
 }{
  t.Run(tc.name,func(t *testing.T){
   if _,_,err:=remotePeerRoles(tc.auth,tc.credentials);err==nil{
    t.Fatal("invalid remote TLS role policy accepted")
   }
  })
 }
}
