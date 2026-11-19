package agentflow

import (
 "encoding/json"
 "net/http"
 "net/http/httptest"
 "os"
 "path/filepath"
 "strings"
 "sync/atomic"
 "testing"
 "time"
)

func machineTestSecret(t *testing.T)string{
 t.Helper()
 file:=filepath.Join(t.TempDir(),"engineering-machine-secret")
 if err:=os.WriteFile(file,[]byte(strings.Repeat("s",48)),0600);err!=nil{t.Fatal(err)}
 return file
}
func machineJWT(letter string)string{
 return strings.Repeat(letter,18)+"."+strings.Repeat(letter,18)+"."+strings.Repeat(letter,18)
}

func TestMachineCredentialRenewsOnlyWhenApproachingExpiry(t *testing.T){
 secret:=machineTestSecret(t)
 var requests atomic.Int32
 now:=time.Date(2026,11,19,0,0,0,0,time.UTC)
 server:=httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter,r *http.Request){
  requests.Add(1)
  user,password,ok:=r.BasicAuth()
  if !ok||user!="engineering-project-editor"||password!=strings.Repeat("s",48){
   t.Errorf("machine credentials were not sent with protected Basic auth")
  }
  if r.Method!="POST"||r.FormValue("grant_type")!="client_credentials"||
   r.FormValue("scope")!="compute:editor"||
   r.FormValue("resource")!="https://compute.example.test"{
    t.Errorf("unexpected token request parameters")
  }
  w.Header().Set("Content-Type","application/json")
  json.NewEncoder(w).Encode(map[string]any{
   "access_token":machineJWT("a"),"token_type":"Bearer","expires_in":3600,
  })
 }))
 defer server.Close()
 source:=&machineTokenSource{
  endpoint:server.URL+"/oauth2/token",
  clientID:"engineering-project-editor",
  secretFile:secret,
  scope:"compute:editor",
  resource:"https://compute.example.test",
  client:server.Client(),
  now:func()time.Time{return now},
 }
 first,err:=source.get()
 if err!=nil||first!=machineJWT("a"){t.Fatalf("first machine access token: %q %v",first,err)}
 second,err:=source.get()
 if err!=nil||first!=second||requests.Load()!=1{
  t.Fatalf("token was not cached: %d %v",requests.Load(),err)
 }
 now=now.Add(61*time.Minute)
 if _,err:=source.get();err!=nil||requests.Load()!=2{
  t.Fatalf("short-lived token was not refreshed: %d %v",requests.Load(),err)
 }
}

func TestMachineTokenDeniesRedirectsAndMalformedResponses(t *testing.T){
 secret:=machineTestSecret(t)
 for _,invalid:=range []struct{name string; status int; contentType string; body string}{
  {"refused",403,"application/json",`{"error":"denied"}`},
  {"wrong type",200,"text/plain",`{"access_token":"not valid"}`},
  {"opaque",200,"application/json",`{"access_token":"verylongbutopaqueopaquenotjwt","token_type":"Bearer","expires_in":3600}`},
  {"long lifetime",200,"application/json",`{"access_token":"aaaaaaaaaaaaaaaa.bbbbbbbbbbbbbbbb.cccccccccccccccc","token_type":"Bearer","expires_in":999999}`},
  {"short lifetime",200,"application/json",`{"access_token":"aaaaaaaaaaaaaaaa.bbbbbbbbbbbbbbbb.cccccccccccccccc","token_type":"Bearer","expires_in":1}`},
 }{
  t.Run(invalid.name,func(t *testing.T){
   srv:=httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter,r *http.Request){
    w.Header().Set("Content-Type",invalid.contentType)
    w.WriteHeader(invalid.status)
    w.Write([]byte(invalid.body))
   }))
   defer srv.Close()
   source:=&machineTokenSource{
    endpoint:srv.URL+"/token",clientID:"engineering",
    secretFile:secret,client:srv.Client(),now:time.Now,
   }
   if token,err:=source.get();err==nil||token!=""{
    t.Fatalf("untrusted OAuth response was accepted: %q %v",token,err)
   }
  })
 }
}

func TestMachineClientSecretRequiresPrivateRegularFile(t *testing.T){
 secret:=machineTestSecret(t)
 if _,err:=readMachineSecret(secret);err!=nil{t.Fatal(err)}
 if err:=os.Chmod(secret,0644);err!=nil{t.Fatal(err)}
 if _,err:=readMachineSecret(secret);err==nil{t.Fatal("world-readable client secret accepted")}
 if err:=os.Chmod(secret,0600);err!=nil{t.Fatal(err)}
 if err:=os.Symlink(secret,filepath.Join(filepath.Dir(secret),"link"));err!=nil{t.Fatal(err)}
 if _,err:=readMachineSecret(filepath.Join(filepath.Dir(secret),"link"));err==nil{
  t.Fatal("symlink client secret accepted")
 }
 if _,err:=NewMachineTokenSource("http://localhost:3000/token","id",secret,"","");err==nil{
  t.Fatal("insecure OAuth issuer accepted")
 }
 if _,err:=NewMachineTokenSource("https://id.example.test/token","id",secret,"","http://compute.test");err==nil{
  t.Fatal("insecure OAuth resource accepted")
 }
}
