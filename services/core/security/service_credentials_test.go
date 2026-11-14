package security

import (
 "bytes"
 "crypto/tls"
 "crypto/x509"
 "encoding/json"
 "net"
 "net/http"
 "net/http/httptest"
 "path/filepath"
 "strings"
 "testing"
 "time"
)

func TestServiceCredentialGrantsBindOneVerifiedIdentityAndProvider(t *testing.T){
 manager,key,now:=testSessionManager(t)
 loginAt(t,manager,key,now)
 manager.rules["service__status"]=OperationPolicy{
  Operation:"service__status",Targets:[]string{"jupiter"},
 }
 vault,err:=OpenVault(filepath.Join(t.TempDir(),"vault.json"),bytes.Repeat([]byte("v"),32))
 if err!=nil{t.Fatal(err)}
 handle,err:=vault.Store("gitlab",[]byte("glpat-private-secret-example"))
 if err!=nil{t.Fatal(err)}
 if err:=manager.SetVault(vault);err!=nil{t.Fatal(err)}
 grant:=ServiceCredentialGrant{
  ClientDNS:"compute.internal",Alias:"gitlab",Provider:"gitlab",Handle:handle.ID,
  Operation:"service__status",Target:"jupiter",
 }
 if err:=manager.SetServiceCredentialGrants([]ServiceCredentialGrant{grant});err!=nil{t.Fatal(err)}
 if err:=manager.SetServiceCredentialGrants([]ServiceCredentialGrant{grant});err==nil{
  t.Fatal("credential policy was overwritten")
 }
 args:=json.RawMessage(`{"host":"jupiter","image":"debian"}`)
 mint:=func()CredentialTicket{
  t.Helper()
  decision:=manager.authorizeForServicePeer(
   "local-operator",manager.client,"compute.internal",grant.Operation,grant.Target,args)
  if !decision.Allowed||len(decision.CredentialTickets)!=1 {
   t.Fatalf("scoped capability unavailable: %+v",decision)
  }
  return decision.CredentialTickets[0]
 }
 request:=func(cap CredentialTicket,alias,clientDNS,operation,target string,arguments json.RawMessage,verified,extraSAN bool)*httptest.ResponseRecorder{
  t.Helper()
  payload,_:=json.Marshal(map[string]any{
   "ticket":cap.Ticket,"operation":operation,"target":target,"arguments":arguments,
  })
  r:=httptest.NewRequest(http.MethodPost,"https://security.internal/v1/service-credentials/"+alias,bytes.NewReader(payload))
  r.Header.Set("Content-Type","application/json")
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
  alias,identity,operation,target string
  args json.RawMessage
  verified,extraSAN bool
 }{
  {"gitlab","compute.internal",grant.Operation,grant.Target,args,false,false},
  {"gitlab","unapproved.internal",grant.Operation,grant.Target,args,true,false},
  {"github","compute.internal",grant.Operation,grant.Target,args,true,false},
  {"gitlab","compute.internal",grant.Operation,grant.Target,args,true,true},
  {"gitlab","compute.internal","workspace__pool_register_image",grant.Target,args,true,false},
  {"gitlab","compute.internal",grant.Operation,"venus",args,true,false},
  {"gitlab","compute.internal",grant.Operation,grant.Target,json.RawMessage(`{"host":"venus"}`),true,false},
 }{
  cap:=mint()
  denied:=request(cap,tc.alias,tc.identity,tc.operation,tc.target,tc.args,tc.verified,tc.extraSAN)
  if denied.Code==200||strings.Contains(denied.Body.String(),"glpat-private-secret-example"){
   t.Fatalf("unauthorized service credential accessed: %+v",tc)
  }
  replay:=request(cap,"gitlab","compute.internal",grant.Operation,grant.Target,args,true,false)
  // A malformed TLS identity is rejected before ticket redemption. A
  // verified substitution consumes the capability irrevocably.
  if tc.verified&&!tc.extraSAN&&replay.Code==200{t.Fatal("substituted capability remained redeemable")}
 }
 allowedCap:=mint()
 allowed:=request(allowedCap,"gitlab","compute.internal",grant.Operation,grant.Target,args,true,false)
 if allowed.Code!=200||allowed.Body.String()!="glpat-private-secret-example"||
  allowed.Header().Get("Cache-Control")!="no-store"{
  t.Fatalf("authorized service secret was not securely delivered: %d",allowed.Code)
 }
 if request(allowedCap,"gitlab","compute.internal",grant.Operation,grant.Target,args,true,false).Code==200{
  t.Fatal("one-use capability was replayed")
 }
 if _,err:=vault.Rotate(handle.ID,"gitlab",[]byte("glpat-rotated-secret-example"));err!=nil{t.Fatal(err)}
 if request(mint(),"gitlab","compute.internal",grant.Operation,grant.Target,args,true,false).Code==200{
  t.Fatal("credential rotation did not invalidate a pinned capability")
 }
 if err:=vault.Delete(handle.ID);err!=nil{t.Fatal(err)}
 if request(mint(),"gitlab","compute.internal",grant.Operation,grant.Target,args,true,false).Code==200{
  t.Fatal("revoked vault credential was still available")
 }
 manager.Revoke()
 if result:=manager.authorizeForServicePeer(
  "local-operator",manager.client,"compute.internal",grant.Operation,grant.Target,args);result.Allowed {
  t.Fatal("revoked owner session issued a credential capability")
 }
 for _,handler:=range []http.Handler{manager.ClientHandler(),manager.OwnerHandler()}{
  r:=httptest.NewRequest(http.MethodPost,"/v1/service-credentials/gitlab",nil)
  out:=httptest.NewRecorder()
  handler.ServeHTTP(out,r)
  if out.Code==200{t.Fatal("vault route leaked into the gateway or owner socket")}
 }
}

func TestServiceCredentialPolicyRejectsUnknownAndDuplicateGrants(t *testing.T){
 create:=func()*AdminSessions{
  t.Helper()
  manager,_,_:=testSessionManager(t)
  vault,err:=OpenVault(filepath.Join(t.TempDir(),"vault.json"),bytes.Repeat([]byte("k"),32))
  if err!=nil{t.Fatal(err)}
  if _,err:=vault.Store("gitlab",[]byte("secret-value"));err!=nil{t.Fatal(err)}
  if err:=manager.SetVault(vault);err!=nil{t.Fatal(err)}
  return manager
 }
 manager:=create()
 grant:=ServiceCredentialGrant{
  ClientDNS:"compute.internal",Alias:"gitlab",Provider:"gitlab",
  Handle:"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
  Operation:"workspace__workspace_create",Target:"jupiter",
 }
 if err:=manager.SetServiceCredentialGrants([]ServiceCredentialGrant{grant});err==nil{
  t.Fatal("unknown credential handle was permitted")
 }
 manager=create()
 grant.Handle=manager.vault.List()[0].ID
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
 grant.Target="unapproved"
 if err:=manager.SetServiceCredentialGrants([]ServiceCredentialGrant{grant});err==nil{
  t.Fatal("unapproved resource target accepted")
 }
 manager=create()
 grant.Target="jupiter"
 grant.Operation="unapproved__operation"
 if err:=manager.SetServiceCredentialGrants([]ServiceCredentialGrant{grant});err==nil{
  t.Fatal("unapproved operation accepted")
 }
 manager=create()
 grant.Operation="workspace__workspace_create"
 grant.ArgumentsSHA256="not-a-sha256"
 if err:=manager.SetServiceCredentialGrants([]ServiceCredentialGrant{grant});err==nil{
  t.Fatal("invalid canonical argument digest accepted")
 }
 manager=create()
 grant.ArgumentsSHA256=""
 grant.Handle=manager.vault.List()[0].ID
 expired:=time.Unix(1_700_000_000,0).Add(-time.Minute)
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
