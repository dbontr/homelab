package transport

import (
 "net/http"
 "net/http/httptest"
 "path/filepath"
 "strings"
 "testing"

 "github.com/dbontr/homelab-new/system/security"
 "github.com/dbontr/homelab-new/system/store"
)

func TestSecuritySignInUIIsNonSecretAndDisabledWithoutService(t *testing.T) {
 db,err:=store.Open(filepath.Join(t.TempDir(),"store.json"),"UTC")
 if err!=nil{t.Fatal(err)}
 token:=strings.Repeat("x",40)
 server,err:=New(db,token,nil)
 if err!=nil{t.Fatal(err)}
 do:=func(payload string)string{
  r:=httptest.NewRequest(http.MethodPost,"/mcp",strings.NewReader(payload))
  r.Header.Set("Authorization","Bearer "+token)
  r.Header.Set("Content-Type","application/json")
  out:=httptest.NewRecorder()
  server.Handler().ServeHTTP(out,r)
  if out.Code!=200{t.Fatalf("unexpected HTTP %d: %s",out.Code,out.Body.String())}
  return out.Body.String()
 }
 getResources:=`{"jsonrpc":"2.0","id":1,"method":"resources/list"}`
 if got:=do(getResources);strings.Contains(got,"security-login.html"){t.Fatal("unconfigured sign-in resource advertised")}
 for _,raw:=range []string{"http://auth.example.test","https://user:password@auth.example.test","https://auth.example.test/path","https://auth.example.test/?next=untrusted"}{
  if err:=server.SetSecurityLoginURL(raw);err==nil{t.Fatalf("unsafe sign-in URL accepted: %s",raw)}
 }
 if err:=server.SetSecurityLoginURL("https://auth.example.test");err!=nil{t.Fatal(err)}
 if got:=do(getResources);strings.Contains(got,"security-login.html"){t.Fatal("resource advertised without independent security client")}
 client,err:=security.NewAdminClient("/run/homelab-securityd/client.sock")
 if err!=nil{t.Fatal(err)}
 server.SetPrivilegeClient(client)
 if got:=do(getResources);!strings.Contains(got,"security-login.html"){t.Fatal("configured resource not advertised")}
 read:=`{"jsonrpc":"2.0","id":2,"method":"resources/read","params":{"uri":"ui://homelab/security-login.html"}}`
 content:=do(read)
 if !strings.Contains(content,"openExternal")||!strings.Contains(content,"redirect_domains"){t.Fatal("ChatGPT authentication widget unavailable")}
 if strings.Contains(content,"name=\\\"password\\\"")||strings.Contains(content,"name=\\\"totp\\\""){t.Fatal("model-visible UI contains password or OTP fields")}
 bad:=`{"jsonrpc":"2.0","id":3,"method":"resources/read","params":{"uri":"ui://homelab/untrusted.html"}}`
 if !strings.Contains(do(bad),"unknown UI resource"){t.Fatal("unexpected resource accepted")}
}
