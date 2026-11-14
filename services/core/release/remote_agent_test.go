package release

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

func testCertificate(t *testing.T,ca *x509.Certificate,key *rsa.PrivateKey,dns string,server bool)tls.Certificate{
 t.Helper()
 leafKey,err:=rsa.GenerateKey(rand.Reader,2048);if err!=nil{t.Fatal(err)}
 usage:=x509.ExtKeyUsageClientAuth
 if server{usage=x509.ExtKeyUsageServerAuth}
 now:=time.Now()
 tmpl:=&x509.Certificate{
  SerialNumber:big.NewInt(now.UnixNano()),Subject:pkix.Name{CommonName:dns},DNSNames:[]string{dns},
  NotBefore:now.Add(-time.Hour),NotAfter:now.Add(time.Hour),KeyUsage:x509.KeyUsageDigitalSignature,ExtKeyUsage:[]x509.ExtKeyUsage{usage},
 }
 der,err:=x509.CreateCertificate(rand.Reader,tmpl,ca,&leafKey.PublicKey,key)
 if err!=nil{t.Fatal(err)}
 return tls.Certificate{Certificate:[][]byte{der,ca.Raw},PrivateKey:leafKey}
}

func remoteFixture(t *testing.T,handler http.Handler)(RemoteAgentConfig,func()){
 t.Helper()
 key,err:=rsa.GenerateKey(rand.Reader,2048);if err!=nil{t.Fatal(err)}
 now:=time.Now()
 tmpl:=&x509.Certificate{SerialNumber:big.NewInt(1),Subject:pkix.Name{CommonName:"release-test-ca"},NotBefore:now.Add(-time.Hour),NotAfter:now.Add(time.Hour),IsCA:true,BasicConstraintsValid:true,KeyUsage:x509.KeyUsageCertSign|x509.KeyUsageDigitalSignature}
 der,err:=x509.CreateCertificate(rand.Reader,tmpl,tmpl,&key.PublicKey,key)
 if err!=nil{t.Fatal(err)}
 ca,err:=x509.ParseCertificate(der);if err!=nil{t.Fatal(err)}
 pool:=x509.NewCertPool();pool.AddCert(ca)
 server:=httptest.NewUnstartedServer(handler)
 server.TLS=&tls.Config{MinVersion:tls.VersionTLS13,ClientAuth:tls.RequireAndVerifyClientCert,ClientCAs:pool,Certificates:[]tls.Certificate{testCertificate(t,ca,key,"release-jupiter.internal",true)}}
 server.StartTLS()
 return RemoteAgentConfig{Host:"jupiter",Endpoint:server.URL,ServerName:"release-jupiter.internal",TrustRoots:pool,
  Certificate:testCertificate(t,ca,key,"release-coordinator.internal",false),
  AccessAssertion:func(context.Context)string{return "signed-owner-assertion"}},server.Close
}

func TestRemoteAgentAuthenticatesTransportAndScopesArguments(t *testing.T){
 id:=strings.Repeat("a",48)
 calls:=[]string{}
 handler:=http.HandlerFunc(func(w http.ResponseWriter,r *http.Request){
  if r.Header.Get("Cf-Access-Jwt-Assertion")!="signed-owner-assertion"{t.Error("missing independently supplied assertion");w.WriteHeader(403);return}
  if len(r.TLS.VerifiedChains)==0{t.Error("client certificate not verified");w.WriteHeader(403);return}
  if r.URL.Path=="/v1/release/status"{_=json.NewEncoder(w).Encode(ReleaseObservation{ID:id,Phase:"active"});return}
  var request struct{
   RequestID string `json:"request_id"`
   Operation string `json:"operation"`
   Target string `json:"target"`
   Arguments struct{
    Host string `json:"host"`
    ReleaseID string `json:"release_id"`
   } `json:"arguments"`
  }
  if err:=json.NewDecoder(r.Body).Decode(&request);err!=nil{t.Error(err);w.WriteHeader(400);return}
  if !validRemoteExecutionID(request.RequestID){t.Error("invalid caller request identity")}
  if request.Target!="jupiter"||request.Arguments.Host!="jupiter"||request.Arguments.ReleaseID!=id{
   t.Error("unscoped request",request);w.WriteHeader(403);return
  }
  calls=append(calls,request.Operation)
  _=json.NewEncoder(w).Encode(map[string]any{
   "allowed":true,"state":"completed","audit_id":request.RequestID,
  })
 })
 cfg,closeServer:=remoteFixture(t,handler);defer closeServer()
 agent,err:=NewRemoteReleaseAgent(cfg);if err!=nil{t.Fatal(err)}
 if err:=agent.Activate(context.Background(),id);err!=nil{t.Fatal(err)}
 if err:=agent.Revert(context.Background(),id);err!=nil{t.Fatal(err)}
 status,err:=agent.Observe(context.Background())
 if err!=nil||status.ID!=id||status.Phase!="active"{t.Fatal(status,err)}
 if len(calls)!=2||calls[0]!="infrastructure__release_activate"||calls[1]!="infrastructure__release_revert"{t.Fatal(calls)}
 if err:=agent.Activate(context.Background(),"../../etc/passwd");err==nil{t.Fatal("unscoped release ID accepted")}
}

func TestRemoteReleaseAgentPreservesCallerIdentityAndChecksReceipt(t *testing.T){
 releaseID:=strings.Repeat("a",48)
 requestID:=strings.Repeat("b",32)
 calls:=0
 mismatch:=false
 handler:=http.HandlerFunc(func(w http.ResponseWriter,r *http.Request){
  calls++
  var input struct{RequestID string `json:"request_id"`}
  if err:=json.NewDecoder(r.Body).Decode(&input);err!=nil||input.RequestID!=requestID{
   t.Error("remote agent changed caller request identity",err)
  }
  receipt:=requestID
  if mismatch{receipt=strings.Repeat("c",32)}
  w.Header().Set("Content-Type","application/json")
  _=json.NewEncoder(w).Encode(map[string]any{
   "allowed":true,"state":"completed","audit_id":receipt,
  })
 })
 cfg,closeServer:=remoteFixture(t,handler)
 defer closeServer()
 agent,err:=NewRemoteReleaseAgent(cfg)
 if err!=nil{t.Fatal(err)}
 for i:=0;i<2;i++{
  if err:=agent.ActivateWithID(context.Background(),requestID,releaseID);err!=nil{t.Fatal(err)}
 }
 if calls!=2{t.Fatal("explicit request ID was not propagated")}
 mismatch=true
 if err:=agent.RevertWithID(context.Background(),requestID,releaseID);err==nil{
  t.Fatal("unrelated audit receipt was accepted")
 }
 if err:=agent.ActivateWithID(context.Background(),"bad-id",releaseID);err==nil{
  t.Fatal("invalid request ID reached host")
 }
 if calls!=3{t.Fatal("bad request identity contacted host")}
}

func TestRemoteAgentFailsClosedWithoutIdentityOrTrust(t *testing.T){
 cfg,closeServer:=remoteFixture(t,http.HandlerFunc(func(w http.ResponseWriter,r *http.Request){_,_=w.Write([]byte(`{"allowed":true}`))}))
 defer closeServer()
 cfg.AccessAssertion=func(context.Context)string{return ""}
 agent,err:=NewRemoteReleaseAgent(cfg);if err!=nil{t.Fatal(err)}
 if err:=agent.Activate(context.Background(),strings.Repeat("a",48));err==nil{t.Fatal("identity-free request sent")}
 cfg.AccessAssertion=func(context.Context)string{return "signed-owner-assertion"}
 cfg.TrustRoots=x509.NewCertPool()
 agent,err=NewRemoteReleaseAgent(cfg);if err!=nil{t.Fatal(err)}
 if err:=agent.Activate(context.Background(),strings.Repeat("a",48));err==nil{t.Fatal("untrusted server accepted")}
 cfg.Endpoint="http://127.0.0.1:9999"
 if _,err:=NewRemoteReleaseAgent(cfg);err==nil{t.Fatal("unencrypted transport accepted")}
}
