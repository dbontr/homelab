package controller

import (
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

func TestVaultClientReceivesOnlyExactScopedCredential(t *testing.T){
 response:=struct{status int;kind,secret string}{
  status:200,kind:"application/octet-stream",
  secret:"glpat-owner-vault-scoped-secret",
 }
 count:=0
 provider:=httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter,r *http.Request){
  count++
  if r.Method!="GET"||r.URL.EscapedPath()!="/v1/service-credentials/gitlab"{
   t.Errorf("vault client requested unapproved path: %s",r.URL.EscapedPath())
  }
  w.Header().Set("Content-Type",response.kind)
  w.WriteHeader(response.status)
  _,_=io.WriteString(w,response.secret)
 }))
 defer provider.Close()
 // The production constructor supplies mTLS, pinned server identity and an
 // HTTPS endpoint. This local handler checks the bounded response contract.
 client:=&VaultSecretClient{client:provider.Client(),endpoint:provider.URL}
 token,err:=client.Credential("gitlab")
 if err!=nil||token!=response.secret||count!=1{
  t.Fatalf("valid vault credential missing: %q %v",token,err)
 }
 if _,err:=client.Credential("../../private");err==nil||count!=1{
  t.Fatal("invalid alias reached secret provider")
 }
 response.status=403
 if _,err:=client.Credential("gitlab");err==nil{
  t.Fatal("denied credential accepted")
 }
 response.status=200
 response.kind="text/plain"
 if _,err:=client.Credential("gitlab");err==nil{
  t.Fatal("non-secret content type accepted")
 }
 response.kind="application/octet-stream"
 response.secret=strings.Repeat("x",4097)
 if _,err:=client.Credential("gitlab");err==nil{
  t.Fatal("oversized credential accepted")
 }
 response.secret="unverified value with spaces"
 if _,err:=client.Credential("gitlab");err==nil{
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
