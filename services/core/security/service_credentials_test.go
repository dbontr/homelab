package security

import (
 "bytes"
 "crypto/tls"
 "crypto/x509"
 "net"
 "net/http"
 "net/http/httptest"
 "path/filepath"
 "strings"
 "testing"
 "time"
)

func TestServiceCredentialGrantsBindOneVerifiedIdentityAndProvider(t *testing.T){
 manager,_,_:=testSessionManager(t)
 vault,err:=OpenVault(filepath.Join(t.TempDir(),"vault.json"),bytes.Repeat([]byte("v"),32))
 if err!=nil{t.Fatal(err)}
 handle,err:=vault.Store("gitlab",[]byte("glpat-private-secret-example"))
 if err!=nil{t.Fatal(err)}
 if err:=manager.SetVault(vault);err!=nil{t.Fatal(err)}
 grant:=ServiceCredentialGrant{
  ClientDNS:"compute.internal",Alias:"gitlab",Provider:"gitlab",Handle:handle.ID,
 }
 if err:=manager.SetServiceCredentialGrants([]ServiceCredentialGrant{grant});err!=nil{t.Fatal(err)}
 if err:=manager.SetServiceCredentialGrants([]ServiceCredentialGrant{grant});err==nil{
  t.Fatal("service credential policy was overwritten")
 }
 request:=func(alias,clientDNS string,verified bool,extraSAN bool)*httptest.ResponseRecorder{
  r:=httptest.NewRequest(http.MethodGet,"https://security.internal/v1/service-credentials/"+alias,nil)
  r.SetPathValue("alias",alias)
  peer:=&x509.Certificate{DNSNames:[]string{clientDNS}}
  if extraSAN{peer.DNSNames=append(peer.DNSNames,"another.internal")}
  r.TLS=&tls.ConnectionState{PeerCertificates:[]*x509.Certificate{peer}}
  if verified{r.TLS.VerifiedChains=[][]*x509.Certificate{{peer}}}
  result:=httptest.NewRecorder()
  manager.ServiceCredentialHandler().ServeHTTP(result,r)
  return result
 }
 for _,tc:=range []struct{
  alias,identity string
  verified,extraSAN bool
 }{
  {"gitlab","compute.internal",false,false},
  {"gitlab","unapproved.internal",true,false},
  {"github","compute.internal",true,false},
  {"gitlab","compute.internal",true,true},
 }{
  denied:=request(tc.alias,tc.identity,tc.verified,tc.extraSAN)
  if denied.Code==200||strings.Contains(denied.Body.String(),"glpat-private-secret-example"){
   t.Fatalf("unauthorized service credential accessed: %+v",tc)
  }
 }
 allowed:=request("gitlab","compute.internal",true,false)
 if allowed.Code!=200||allowed.Body.String()!="glpat-private-secret-example"||
  allowed.Header().Get("Cache-Control")!="no-store"{
  t.Fatalf("authorized service secret was not securely delivered: %d",allowed.Code)
 }
 // Rotation keeps the handle but invalidates the revision-pinned service grant.
 if _,err:=vault.Rotate(handle.ID,"gitlab",[]byte("glpat-rotated-secret-example"));err!=nil{t.Fatal(err)}
 rotated:=request("gitlab","compute.internal",true,false)
 if rotated.Code==200||strings.Contains(rotated.Body.String(),"glpat-rotated-secret-example"){
  t.Fatal("service credential grant survived rotation under the same handle")
 }
 if err:=vault.Delete(handle.ID);err!=nil{t.Fatal(err)}
 revoked:=request("gitlab","compute.internal",true,false)
 if revoked.Code==200{
  t.Fatal("revoked vault credential was still retrievable")
 }
 for _,handler:=range []http.Handler{manager.ClientHandler(),manager.OwnerHandler()}{
  r:=httptest.NewRequest(http.MethodGet,"/v1/service-credentials/gitlab",nil)
  out:=httptest.NewRecorder()
  handler.ServeHTTP(out,r)
  if out.Code==200{
   t.Fatal("service secret endpoint leaked into gateway or owner handler")
  }
 }
}

func TestServiceCredentialPolicyRejectsUnknownAndDuplicateGrants(t *testing.T){
 create:=func()*AdminSessions{
  t.Helper()
  manager,_,_:=testSessionManager(t)
  vault,err:=OpenVault(filepath.Join(t.TempDir(),"vault.json"),bytes.Repeat([]byte("k"),32))
  if err!=nil{t.Fatal(err)}
  handle,err:=vault.Store("gitlab",[]byte("secret-value"))
  if err!=nil{t.Fatal(err)}
  if err:=manager.SetVault(vault);err!=nil{t.Fatal(err)}
  _=handle
  return manager
 }
 manager:=create()
 grant:=ServiceCredentialGrant{
  ClientDNS:"compute.internal",Alias:"gitlab",Provider:"gitlab",
  Handle:"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
 }
 if err:=manager.SetServiceCredentialGrants([]ServiceCredentialGrant{grant});err==nil{
  t.Fatal("unknown credential handle was permitted")
 }
 manager=create()
 vault:=manager.vault
 handles:=vault.List()
 grant.Handle=handles[0].ID
 if err:=manager.SetServiceCredentialGrants([]ServiceCredentialGrant{grant,grant});err==nil{
  t.Fatal("duplicate service grant was accepted")
 }
 manager=create()
 grant.Handle=manager.vault.List()[0].ID
 grant.ClientDNS="compute.internal/../../private"
 if err:=manager.SetServiceCredentialGrants([]ServiceCredentialGrant{grant});err==nil{
  t.Fatal("invalid service certificate identity was accepted")
 }
 manager=create()
 grant.ClientDNS="compute.internal"
 grant.Handle=manager.vault.List()[0].ID
 grant.Revision=strings.Repeat("0",64)
 if err:=manager.SetServiceCredentialGrants([]ServiceCredentialGrant{grant});err==nil{
  t.Fatal("stale credential revision was accepted")
 }
 manager=create()
 grant.Handle=manager.vault.List()[0].ID
 grant.Revision=""
 expired:=time.Now().UTC().Add(-time.Minute)
 grant.ExpiresAt=&expired
 if err:=manager.SetServiceCredentialGrants([]ServiceCredentialGrant{grant});err==nil{
  t.Fatal("expired service credential grant was accepted")
 }
}

func TestServiceCredentialDNSIdentityMustBeCanonical(t *testing.T) {
 for _,name:=range []string{"jupiter","worker.internal","executor-01.saturn.internal"} {
  if !ValidServiceDNS(name){t.Fatalf("valid service identity rejected: %q",name)}
 }
 for _,name:=range []string{"", ".", "worker.", ".worker", "worker..internal",
  "-worker.internal", "worker-.internal", "*.internal", "worker.*",
  "Worker.internal", "worker_internal", "127.0.0.1", "worker/internal", "worker.internal:443",
  "https://worker.internal", "worker internal", "worker\n.internal",
  strings.Repeat("a",64)+".internal", strings.Repeat("a",251)+".internal"} {
  if ValidServiceDNS(name){t.Fatalf("invalid service identity accepted: %q",name)}
 }
 peer:=&x509.Certificate{DNSNames:[]string{"jupiter.internal"}}
 state:=&tls.ConnectionState{
  PeerCertificates:[]*x509.Certificate{peer},
  VerifiedChains:[][]*x509.Certificate{{peer}},
 }
 if dns,ok:=verifiedServiceDNS(state);!ok||dns!="jupiter.internal"{
  t.Fatalf("valid certificate identity rejected: %q %v",dns,ok)
 }
 for _,names:=range [][]string{
  {"*.internal"}, {"jupiter.internal","other.internal"}, {"JUPITER.internal"},
  {"jupiter.internal."}, {""},
 } {
  state.PeerCertificates[0]=&x509.Certificate{DNSNames:names}
  if dns,ok:=verifiedServiceDNS(state);ok{
   t.Fatalf("malformed certificate DNS admitted: %q",dns)
  }
 }
 state.PeerCertificates[0]=&x509.Certificate{
  DNSNames:[]string{"jupiter.internal"},
  IPAddresses:[]net.IP{net.ParseIP("10.77.30.1")},
 }
 if dns,ok:=verifiedServiceDNS(state);ok{
  t.Fatalf("mixed DNS/IP certificate identity accepted: %q",dns)
 }
 state.PeerCertificates[0]=&x509.Certificate{
  DNSNames:[]string{"jupiter.internal"},
  EmailAddresses:[]string{"operator@example.test"},
 }
 if dns,ok:=verifiedServiceDNS(state);ok{
  t.Fatalf("mixed DNS/email certificate identity accepted: %q",dns)
 }
 state.PeerCertificates[0]=peer
 state.VerifiedChains=nil
 if dns,ok:=verifiedServiceDNS(state);ok{
  t.Fatalf("unverified certificate accepted: %q",dns)
 }
}
