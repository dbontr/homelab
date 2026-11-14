package controller

import (
 "context"
 "encoding/json"
 "io"
 "net/http"
 "net/http/httptest"
 "os"
 "path/filepath"
 "strings"
 "testing"
)

func TestVaultClientRejectsIncompleteMutualTLSConfiguration(t *testing.T){
 for _,tc:=range []struct{
  endpoint,hostname,ca,cert,key string
 }{
  {"http://security.internal:8443","security.internal","ca","cert","key"},
  {"https://user:password@security.internal:8443","security.internal","ca","cert","key"},
  {"https://security.internal:8443/owner","security.internal","ca","cert","key"},
  {"https://security.internal:8443","", "ca","cert","key"},
  {"https://security.internal:8443","security.internal","","cert","key"},
 } {
  if _,err:=NewVaultSecretClient(tc.endpoint,tc.hostname,tc.ca,tc.cert,tc.key);err==nil{
   t.Fatalf("unsafe vault TLS configuration accepted: %+v",tc)
  }
 }
}

func TestVaultClientPinsCanonicalDNSAndProtectedFiles(t *testing.T){
 for _,name:=range []string{"","*.internal","SECURITY.internal","127.0.0.1",
  "security.",".internal","security..internal","security_internal","https://security.internal"}{
  if validVaultServerDNS(name){t.Fatalf("invalid vault TLS name accepted: %q",name)}
 }
 if !validVaultServerDNS("security.internal"){t.Fatal("canonical identity refused")}
 dir:=t.TempDir()
 path:=filepath.Join(dir,"client-key")
 if err:=os.WriteFile(path,[]byte("test-private-material"),0600);err!=nil{t.Fatal(err)}
 if _,err:=protectedVaultTLSFile(path,true);err!=nil{t.Fatal(err)}
 link:=filepath.Join(dir,"client-key-link")
 if err:=os.Symlink(path,link);err!=nil{t.Fatal(err)}
 if _,err:=protectedVaultTLSFile(link,true);err==nil{
  t.Fatal("symlinked private client key accepted")
 }
 if err:=os.Chmod(path,0644);err!=nil{t.Fatal(err)}
 if _,err:=protectedVaultTLSFile(path,true);err==nil{
  t.Fatal("world-readable vault private key accepted")
 }
 if err:=os.Chmod(path,0600);err!=nil{t.Fatal(err)}
 hard:=filepath.Join(dir,"client-key-hardlink")
 if err:=os.Link(path,hard);err!=nil{t.Fatal(err)}
 if _,err:=protectedVaultTLSFile(path,true);err==nil{
  t.Fatal("shared-inode vault private key accepted")
 }
}

func TestVaultClientReceivesOnlyExactScopedCredential(t *testing.T){
 response:=struct{status int;kind,secret string}{
  status:200,kind:"application/octet-stream",
  secret:"glpat-owner-vault-scoped-secret",
 }
 count:=0
 operation,target:="workspace__workspace_create","jupiter"
 arguments:=json.RawMessage(`{"host":"jupiter"}`)
 provider:=httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter,r *http.Request){
  count++
  if r.Method!=http.MethodPost||r.URL.EscapedPath()!="/v1/service-credentials/gitlab"{
   t.Errorf("vault client requested unapproved method or path: %s %s",r.Method,r.URL.EscapedPath())
  }
  if r.Header.Get("Content-Type")!="application/json"{
   t.Error("credential redemption did not use JSON request")
  }
  var payload struct{
   Ticket string `json:"ticket"`
   Operation string `json:"operation"`
   Target string `json:"target"`
   Arguments json.RawMessage `json:"arguments"`
  }
  if err:=json.NewDecoder(r.Body).Decode(&payload);err!=nil ||
   payload.Ticket!=strings.Repeat("a",48) ||payload.Operation!=operation||
   payload.Target!=target||string(payload.Arguments)!=string(arguments){
   t.Error("credential redemption changed approved scope",err,payload)
  }
  w.Header().Set("Content-Type",response.kind)
  w.WriteHeader(response.status)
  _,_=io.WriteString(w,response.secret)
 }))
 defer provider.Close()
 client:=&VaultSecretClient{client:provider.Client(),endpoint:provider.URL}
 capability:=VaultCredentialTicket{Alias:"gitlab",Ticket:strings.Repeat("a",48)}
 token,err:=client.CredentialForOperation(context.Background(),capability,operation,target,arguments)
 if err!=nil||token!=response.secret||count!=1{
  t.Fatalf("valid vault credential missing: %q %v",token,err)
 }
 if _,err:=client.Credential("gitlab");err==nil||count!=1{
  t.Fatal("ambient credential source contacted the vault")
 }
 invalid:=capability;invalid.Alias="../../private"
 if _,err:=client.CredentialForOperation(context.Background(),invalid,operation,target,arguments);err==nil||count!=1{
  t.Fatal("invalid alias reached secret provider")
 }
 invalid=capability;invalid.Ticket="not-authorized"
 if _,err:=client.CredentialForOperation(context.Background(),invalid,operation,target,arguments);err==nil||count!=1{
  t.Fatal("invalid capability reached secret provider")
 }
 response.status=403
 if _,err:=client.CredentialForOperation(context.Background(),capability,operation,target,arguments);err==nil{
  t.Fatal("denied credential accepted")
 }
 response.status=200
 response.kind="text/plain"
 if _,err:=client.CredentialForOperation(context.Background(),capability,operation,target,arguments);err==nil{
  t.Fatal("non-secret content type accepted")
 }
 response.kind="application/octet-stream"
 response.secret=strings.Repeat("x",8193)
 if _,err:=client.CredentialForOperation(context.Background(),capability,operation,target,arguments);err==nil{
  t.Fatal("oversized credential accepted")
 }
 response.secret="unverified value with spaces"
 if _,err:=client.CredentialForOperation(context.Background(),capability,operation,target,arguments);err==nil{
  t.Fatal("malformed provider credential accepted")
 }
}

func TestVaultClientRequiresProtectedCertificates(t *testing.T){
 dir:=t.TempDir()
 ca,cert,key:=filepath.Join(dir,"ca.crt"),filepath.Join(dir,"cert.pem"),filepath.Join(dir,"key.pem")
 for _,path:=range []string{ca,cert,key}{
  if err:=os.WriteFile(path,[]byte("not-a-certificate"),0600);err!=nil{t.Fatal(err)}
 }
 if _,err:=NewVaultSecretClient("https://127.0.0.1:9443","security.internal",ca,cert,key);err==nil{
  t.Fatal("invalid vault TLS certificate identity accepted")
 }
}
