package security

import (
 "context"
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
 srv:=httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter,r *http.Request){
  calls++
  if r.Method!=http.MethodGet||r.URL.Path!="/v1/service-credentials/gitlab"{
   t.Errorf("unexpected vault route %s %s",r.Method,r.URL.Path)
  }
  w.Header().Set("Content-Type",contentType)
  w.WriteHeader(status)
  _,_=io.WriteString(w,token)
 }))
 defer srv.Close()
 client:=&RemoteAuthorizationClient{client:srv.Client(),endpoint:srv.URL}
 source,err:=client.TokenSource("gitlab")
 if err!=nil{t.Fatal(err)}
 first,err:=source()
 if err!=nil||first!=token||calls!=1{t.Fatalf("first vault credential unavailable: %v",err)}
 token="glpat-second-owner-vault-credential"
 second,err:=source()
 if err!=nil||second!=token||calls!=2{t.Fatalf("rotated vault credential unavailable: %v",err)}
 status=http.StatusForbidden
 if _,err:=source();err==nil{t.Fatal("revoked vault access was silently accepted")}
 status=http.StatusOK
 contentType="text/plain"
 if _,err:=source();err==nil{t.Fatal("non-credential response accepted")}
 contentType="application/octet-stream"
 token=strings.Repeat("x",4097)
 if _,err:=source();err==nil{t.Fatal("oversized vault response accepted")}
 for _,alias:=range []string{"../root","", "gitlab/other"}{
  if _,err:=client.TokenSource(alias);err==nil{t.Fatalf("unsafe alias accepted: %q",alias)}
 }
 if calls!=5{t.Fatalf("invalid alias contacted vault: %d calls",calls)}
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
 if _,err:=client.ServiceCredential(context.Background(),"gitlab");err==nil{
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
