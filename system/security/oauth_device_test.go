package security

import (
 "bytes"
 "context"
 "encoding/json"
 "io"
 "net/http"
 "net/http/httptest"
 "path/filepath"
 "strings"
 "testing"
 "time"
)

func deviceOwnerCall(manager *AdminSessions,route string,body map[string]string)*httptest.ResponseRecorder{
 raw,_:=json.Marshal(body)
 req:=httptest.NewRequest(http.MethodPost,route,bytes.NewReader(raw))
 req.Header.Set("Content-Type","application/json")
 out:=httptest.NewRecorder()
 manager.OwnerHandler().ServeHTTP(out,req)
 return out
}

func TestOwnerDeviceAuthorizationRequiresFactorsAndStoresOnlyEncryptedTokens(t *testing.T){
 manager,key,authNow:=testSessionManager(t)
 vault,err:=OpenVault(filepath.Join(privateDir(t),"device-vault.json"),bytes.Repeat([]byte("v"),32))
 if err!=nil{t.Fatal(err)}
 if err:=manager.SetVault(vault);err!=nil{t.Fatal(err)}
 polls:=0
 server:=httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter,r *http.Request){
  w.Header().Set("Content-Type","application/json")
  switch r.URL.Path{
  case "/device":
   if r.Method!=http.MethodPost||r.PostFormValue("client_id")!="device-client"||
    r.PostFormValue("scope")!="read"{
    t.Error("device enrollment request incorrect")
    w.WriteHeader(400);return
   }
   _,_=io.WriteString(w,`{"device_code":"SECRET-DEVICE-CODE","user_code":"F6X9-KA2","verification_uri":"https://verify.example/device","expires_in":180,"interval":5}`)
  case "/token":
   if r.Method!=http.MethodPost||
    r.PostFormValue("grant_type")!="urn:ietf:params:oauth:grant-type:device_code"||
    r.PostFormValue("device_code")!="SECRET-DEVICE-CODE"||
    r.PostFormValue("client_id")!="device-client"{
    t.Error("device token exchange scope changed")
    w.WriteHeader(400);return
   }
   polls++
   switch polls{
   case 1:
    w.WriteHeader(http.StatusBadRequest)
    _,_=io.WriteString(w,`{"error":"authorization_pending"}`)
   case 2:
    w.WriteHeader(http.StatusBadRequest)
    _,_=io.WriteString(w,`{"error":"slow_down"}`)
   default:
    _,_=io.WriteString(w,`{"access_token":"PRIVATE-ACCESS","refresh_token":"PRIVATE-REFRESH","token_type":"Bearer"}`)
   }
  default:
   t.Error("unexpected provider route")
   w.WriteHeader(404)
  }
 }))
 defer server.Close()
 provider:=Provider{
  Name:"device-vendor",ClientID:"device-client",TokenURL:server.URL+"/token",
  DeviceAuthorizationURL:server.URL+"/device",
  DeviceVerificationHost:"verify.example",Scopes:[]string{"read"},
 }
 broker,err:=NewOAuthBroker([]Provider{provider})
 if err!=nil{t.Fatal(err)}
 address,err:=broker.CallbackListenAddress()
 if err!=nil||address!=""{t.Fatalf("device-only provider required callback: %s %v",address,err)}
 now:=time.Now()
 broker.now=func()time.Time{return now}
 if err:=manager.SetOAuthBroker(broker,server.Client());err!=nil{t.Fatal(err)}
 startPath:="/v1/oauth/device-vendor/device/begin"
 factors:=map[string]string{"password":testOwnerPassword,"totp":TOTPCode(key,authNow.Unix()/30)}
 invalid:=deviceOwnerCall(manager,startPath,map[string]string{"password":"wrong","totp":factors["totp"]})
 if invalid.Code==200{t.Fatal("invalid owner password began device authorization")}
 start:=deviceOwnerCall(manager,startPath,factors)
 if start.Code!=200{t.Fatalf("owner device enrollment denied: %d %s",start.Code,start.Body.String())}
 if strings.Contains(start.Body.String(),"SECRET-DEVICE-CODE")||strings.Contains(start.Body.String(),"PRIVATE-ACCESS"){
  t.Fatal("owner device response exposed provider secrets")
 }
 var instructions OAuthDeviceAuthorization
 if err:=json.Unmarshal(start.Body.Bytes(),&instructions);err!=nil{t.Fatal(err)}
 if len(instructions.ID)!=32||instructions.UserCode!="F6X9-KA2"||
  instructions.VerificationURL!="https://verify.example/device"{
  t.Fatalf("invalid device instructions: %+v",instructions)
 }
 reused:=deviceOwnerCall(manager,startPath,factors)
 if reused.Code==200{t.Fatal("replayed owner factors began a second device flow")}
 gateway:=httptest.NewRecorder()
 manager.ClientHandler().ServeHTTP(gateway,httptest.NewRequest("POST",startPath,nil))
 if gateway.Code==200{t.Fatal("MCP gateway accessed owner-only device enrollment")}
 pollPath:="/v1/oauth/device-vendor/device/poll"
 poll:=func(id string)*httptest.ResponseRecorder{
  return deviceOwnerCall(manager,pollPath,map[string]string{"id":id})
 }
 early:=poll(instructions.ID)
 var response OAuthDeviceStatus
 if early.Code!=200||json.Unmarshal(early.Body.Bytes(),&response)!=nil||
  response.State!="pending"||response.RetryAfterSeconds<1||polls!=0{
  t.Fatalf("initial polling limit not enforced: %d %s",early.Code,early.Body.String())
 }
 now=now.Add(6*time.Second)
 awaiting:=poll(instructions.ID)
 if awaiting.Code!=200||polls!=1{
  t.Fatalf("authorization_pending incorrectly failed: %d %s",awaiting.Code,awaiting.Body.String())
 }
 now=now.Add(6*time.Second)
 slow:=poll(instructions.ID)
 if slow.Code!=200||polls!=2||
  json.Unmarshal(slow.Body.Bytes(),&response)!=nil||response.RetryAfterSeconds<10{
  t.Fatalf("provider slow_down not enforced: %d %s",slow.Code,slow.Body.String())
 }
 now=now.Add(11*time.Second)
 complete:=poll(instructions.ID)
 if complete.Code!=200||polls!=3||
  json.Unmarshal(complete.Body.Bytes(),&response)!=nil||
  response.State!="authorized"||response.Credential==nil{
  t.Fatalf("device credential not stored: %d %s",complete.Code,complete.Body.String())
 }
 if strings.Contains(complete.Body.String(),"PRIVATE-ACCESS"){
  t.Fatal("provider token leaked through owner socket")
 }
 if len(vault.List())!=1{t.Fatal("device token was not persisted to owner vault")}
 if err:=vault.WithCredential(response.Credential.ID,"device-vendor",func(raw []byte)error{
  if !bytes.Contains(raw,[]byte("PRIVATE-ACCESS"))||!bytes.Contains(raw,[]byte("PRIVATE-REFRESH")){
   t.Fatal("expected encrypted vault payload unavailable")
  }
  return nil
 });err!=nil{t.Fatal(err)}
 if again:=poll(instructions.ID);again.Code==200{
  t.Fatal("consumed device authorization was replayed")
 }
}

func TestDeviceAuthorizationRejectsUntrustedProviderInstructions(t *testing.T){
 raw:=`{"device_code":"private-secret","user_code":"ABCD-EFGH","verification_uri":"https://attacker.example/verify","expires_in":120,"interval":5}`
 server:=httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter,_ *http.Request){
  w.Header().Set("Content-Type","application/json")
  _,_=io.WriteString(w,raw)
 }))
 defer server.Close()
 broker,err:=NewOAuthBroker([]Provider{{
  Name:"vendor",ClientID:"public-client",TokenURL:server.URL+"/token",
  DeviceAuthorizationURL:server.URL+"/device",DeviceVerificationHost:"provider.example",
 }})
 if err!=nil{t.Fatal(err)}
 if _,err:=broker.BeginDevice(context.Background(),"vendor",server.Client());err==nil{
  t.Fatal("untrusted verification website was presented to owner")
 }
 if len(broker.deviceSessions)!=0{t.Fatal("untrusted provider instruction left device code pending")}
}

func TestDeviceAuthorizationRejectsMixedFlowAndExpiredCode(t *testing.T){
 if _,err:=NewOAuthBroker([]Provider{{
  Name:"vendor",ClientID:"id",TokenURL:"https://provider.example/token",
  AuthorizeURL:"https://provider.example/authorize",
  DeviceAuthorizationURL:"https://provider.example/device",
  DeviceVerificationHost:"provider.example",
 }});err==nil{t.Fatal("mixed browser and device provider configuration accepted")}
 server:=httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter,_ *http.Request){
  w.Header().Set("Content-Type","application/json")
  _,_=io.WriteString(w,`{"device_code":"secret-device","user_code":"123-456","verification_uri":"https://provider.example/device","expires_in":30,"interval":5}`)
 }))
 defer server.Close()
 broker,err:=NewOAuthBroker([]Provider{{
  Name:"vendor",ClientID:"client",TokenURL:server.URL+"/token",
  DeviceAuthorizationURL:server.URL+"/device",DeviceVerificationHost:"provider.example",
 }})
 if err!=nil{t.Fatal(err)}
 now:=time.Now()
 broker.now=func()time.Time{return now}
 flow,err:=broker.BeginDevice(context.Background(),"vendor",server.Client())
 if err!=nil{t.Fatal(err)}
 vault,err:=OpenVault(filepath.Join(t.TempDir(),"vault.json"),bytes.Repeat([]byte("e"),32))
 if err!=nil{t.Fatal(err)}
 now=now.Add(31*time.Second)
 if _,err:=broker.PollDevice(context.Background(),"vendor",flow.ID,server.Client(),vault);err==nil{
  t.Fatal("expired device code remained redeemable")
 }
 if len(vault.List())!=0{t.Fatal("expired device code created a credential")}
}
