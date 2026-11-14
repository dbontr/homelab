package security

import (
 "context"
 "encoding/json"
 "io"
 "os"
 "path/filepath"
 "net/http"
 "net/http/httptest"
 "strings"
 "testing"
)

func TestScopedVaultCredentialReadReflectsRotationAndRevocation(t *testing.T){
 token:="glpat-first-owner-vault-credential"
 status:=http.StatusOK
 contentType:="application/octet-stream"
 calls:=0
 operation,target:="workspace__workspace_create","jupiter"
 arguments:=json.RawMessage(`{"host":"jupiter"}`)
 srv:=httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter,r *http.Request){
  calls++
  if r.Method!=http.MethodPost||r.URL.Path!="/v1/service-credentials/gitlab"{
   t.Errorf("unexpected vault route %s %s",r.Method,r.URL.Path)
  }
  if r.Header.Get("Content-Type")!="application/json"{t.Error("missing JSON credential request")}
  var body struct{
   Ticket string `json:"ticket"`
   Operation string `json:"operation"`
   Target string `json:"target"`
   Arguments json.RawMessage `json:"arguments"`
  }
  if err:=json.NewDecoder(r.Body).Decode(&body);err!=nil||body.Operation!=operation||
   body.Target!=target||string(body.Arguments)!=string(arguments)||
   !serviceCredentialTicket.MatchString(body.Ticket){
   t.Error("capability authorization context changed",err,body)
  }
  w.Header().Set("Content-Type",contentType)
  w.WriteHeader(status)
  _,_=io.WriteString(w,token)
 }))
 defer srv.Close()
 client:=&RemoteAuthorizationClient{client:srv.Client(),endpoint:srv.URL}
 source,err:=client.ScopedTokenSource("gitlab")
 if err!=nil{t.Fatal(err)}
 cap:=func(suffix string)CredentialTicket{return CredentialTicket{Alias:"gitlab",Ticket:strings.Repeat(suffix,48)}}
 first,err:=source(context.Background(),cap("a"),operation,target,arguments)
 if err!=nil||first!=token||calls!=1{t.Fatalf("scoped vault credential unavailable: %v",err)}
 token="glpat-second-owner-vault-credential"
 second,err:=source(context.Background(),cap("b"),operation,target,arguments)
 if err!=nil||second!=token||calls!=2{t.Fatalf("rotated vault credential unavailable: %v",err)}
 status=http.StatusForbidden
 if _,err:=source(context.Background(),cap("c"),operation,target,arguments);err==nil{
  t.Fatal("revoked vault access was silently accepted")
 }
 status=http.StatusOK
 contentType="text/plain"
 if _,err:=source(context.Background(),cap("d"),operation,target,arguments);err==nil{
  t.Fatal("non-credential response accepted")
 }
 contentType="application/octet-stream"
 token=strings.Repeat("x",8193)
 if _,err:=source(context.Background(),cap("e"),operation,target,arguments);err==nil{
  t.Fatal("oversized vault response accepted")
 }
 for _,alias:=range []string{"../root","", "gitlab/other"}{
  if _,err:=client.ScopedTokenSource(alias);err==nil{t.Fatalf("unsafe alias accepted: %q",alias)}
 }
 if _,err:=client.TokenSource("gitlab");err==nil{t.Fatal("ambient credential source remained enabled")}
 if _,err:=source(context.Background(),CredentialTicket{Alias:"other",Ticket:strings.Repeat("a",48)},operation,target,arguments);err==nil{
  t.Fatal("ticket for another alias accepted")
 }
 if calls!=5{t.Fatalf("invalid credential scope contacted vault: %d calls",calls)}
}

func TestVaultClientRequiresCompleteTrustedTLSConfiguration(t *testing.T){
 if _,err:=NewVaultClientFromFiles("https://security.internal","security.internal","","key","cert");err==nil{
  t.Fatal("untrusted vault connection accepted")
 }
 if _,err:=NewVaultClientFromEnv("homelab");err==nil{
  t.Fatal("invalid environment prefix accepted")
 }
 t.Setenv("TEST_VAULT_URL","https://security.internal")
 if _,err:=NewVaultClientFromEnv("TEST");err==nil{
  t.Fatal("partially configured vault trust accepted")
 }
 client:=(*RemoteAuthorizationClient)(nil)
 if _,err:=client.ServiceCredential(context.Background(),"gitlab",CredentialTicket{Alias:"gitlab",Ticket:strings.Repeat("a",48)},"workspace__workspace_create","jupiter",json.RawMessage(`{"host":"jupiter"}`));err==nil{
  t.Fatal("nil vault client accepted")
 }
}

func TestVaultClientRejectsUnsafeCredentialAndTrustPaths(t *testing.T){
 cert,key:=certificateMaterialFixture(t)
 create:=func(caPath,certPath,keyPath string)error{
  _,err:=NewVaultClientFromFiles("https://security.internal","security.internal",caPath,certPath,keyPath)
  return err
 }
 if err:=create(cert,cert,key);err!=nil{t.Fatalf("valid vault TLS materials refused: %v",err)}
 if err:=os.Chmod(key,0644);err!=nil{t.Fatal(err)}
 if err:=create(cert,cert,key);err==nil{t.Fatal("world-readable vault client key accepted")}
 if err:=os.Chmod(key,0600);err!=nil{t.Fatal(err)}
 link:=filepath.Join(filepath.Dir(key),"vault-key-link")
 if err:=os.Symlink(key,link);err!=nil{t.Fatal(err)}
 if err:=create(cert,cert,link);err==nil{t.Fatal("symlinked vault client key accepted")}
 if err:=os.Remove(link);err!=nil{t.Fatal(err)}
 if err:=os.Symlink(cert,link);err!=nil{t.Fatal(err)}
 if err:=create(link,cert,key);err==nil{t.Fatal("symlinked trust roots accepted")}
 if err:=create(cert,link,key);err==nil{t.Fatal("symlinked vault client certificate accepted")}
}
