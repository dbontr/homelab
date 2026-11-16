package transport

import (
 "context"
 "encoding/json"
 "net"
 "net/http"
 "path/filepath"
 "strings"
 "testing"

 "github.com/dbontr/homelab-new/system/security"
)

func TestOwnerLinkIsRequestScopedAndNeverExposesCredential(t *testing.T) {
 socket:=filepath.Join(t.TempDir(),"security.sock")
 listener,err:=net.Listen("unix",socket)
 if err!=nil{t.Fatal(err)}
 defer listener.Close()
 calls:=0
 srv:=&http.Server{Handler:http.HandlerFunc(func(w http.ResponseWriter,r *http.Request){
  if r.Header.Get("Cf-Access-Jwt-Assertion")!="signed-owner" {
   http.Error(w,"denied",http.StatusForbidden);return
  }
  w.Header().Set("Content-Type","application/json")
  switch r.URL.Path {
  case "/v1/login-requests":
   if r.Method!=http.MethodPost {http.Error(w,"wrong method",405);return}
   calls++
   _,_=w.Write([]byte(`{"request_id":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","reference_code":"aaaaaaaaaaaa","expires_in_seconds":300}`))
  case "/v1/session":
   _,_=w.Write([]byte(`{"active":true}`))
  default:
   http.Error(w,"not found",http.StatusNotFound)
  }
 })}
 defer srv.Close()
 go func(){_ = srv.Serve(listener)}()
 client,err:=security.NewAdminClient(socket)
 if err!=nil{t.Fatal(err)}
 gateway:=&Server{privilegeClient:client,securityLoginURL:"https://auth.example.invalid"}
 ctx:=security.WithAccessAssertion(context.Background(),"signed-owner")
 value,err:=gateway.localTool(ctx,"security_session_open",json.RawMessage("{}"))
 if err!=nil{t.Fatal(err)}
 body,ok:=value.(map[string]any)
 if !ok{t.Fatalf("unexpected response type %T",value)}
 if body["sign_in_url"]!="https://auth.example.invalid/request/"+strings.Repeat("a",48) {
  t.Fatalf("approval link does not bind the request: %v",body["sign_in_url"])
 }
 if body["reference_code"]!="aaaaaaaaaaaa"||body["expires_in_seconds"]!=300{
  t.Fatal("invalid non-bearer reference response")
 }
 if calls!=1{t.Fatalf("request was not issued by authority: %d",calls)}
 raw,_:=json.Marshal(body)
 for _,secret:=range []string{"password","totp","bearer_token","admin_token"} {
  if strings.Contains(string(raw),"\""+secret+"\""){
   t.Fatalf("owner secret field exposed: %s",secret)
  }
 }
 _,err=gateway.localTool(context.Background(),"security_session_open",json.RawMessage("{}"))
 if err==nil{t.Fatal("anonymous owner request accepted")}
}
