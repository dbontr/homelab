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
 cfg.Endpoint="http://127.0.0.1:1234"
 if _,err:=NewRemoteAuthorizationClient(cfg);err==nil{t.Fatal("insecure authorization transport accepted")}
}

func TestSecurityAuthorizationServerRejectsPublicListener(t *testing.T){
 _,serverCert,roots,closeServer:=remoteAuthFixture(t,http.HandlerFunc(func(http.ResponseWriter,*http.Request){}))
 defer closeServer()
 manager:=&AdminSessions{}
 if err:=manager.ServeAuthorizationTLS(context.Background(),"0.0.0.0:443",serverCert,roots,[]string{"jupiter.internal"});err==nil{
  t.Fatal("wildcard security listener accepted")
 }
}
