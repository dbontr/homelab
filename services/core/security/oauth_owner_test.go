package security

import (
 "bytes"
 "context"
 "crypto/sha256"
 "encoding/base64"
 "encoding/json"
 "io"
 "net/http"
 "net/http/httptest"
 "net/url"
 "path/filepath"
 "strings"
 "testing"
)

type oauthTransport func(*http.Request)(*http.Response,error)
func(fn oauthTransport)RoundTrip(r *http.Request)(*http.Response,error){return fn(r)}

func TestOwnerOAuthFlowRequiresFactorsAndOneTimeLocalPKCEState(t *testing.T){
 manager,key,now:=testSessionManager(t)
 vault,err:=OpenVault(filepath.Join(privateDir(t),"oauth-vault.json"),[]byte(strings.Repeat("k",32)))
 if err!=nil{t.Fatal(err)}
 if err:=manager.SetVault(vault);err!=nil{t.Fatal(err)}
 provider:=Provider{
  Name:"provider",AuthorizeURL:"https://provider.example/oauth/authorize",
  TokenURL:"https://provider.example/oauth/token",
  ClientID:"public-app",RedirectURL:"http://127.0.0.1:8765/oauth/callback",
  Scopes:[]string{"read"},
 }
 broker,err:=NewOAuthBroker([]Provider{provider})
 if err!=nil{t.Fatal(err)}
 exchanged:=0
 client:=&http.Client{Transport:oauthTransport(func(r *http.Request)(*http.Response,error){
  exchanged++
  if r.URL.String()!=provider.TokenURL||r.Method!="POST"{
   t.Fatalf("unexpected OAuth token recipient: %s",r.URL.String())
  }
  if err:=r.ParseForm();err!=nil{t.Fatal(err)}
  if r.Form.Get("grant_type")!="authorization_code"||
   r.Form.Get("client_id")!=provider.ClientID||
   r.Form.Get("redirect_uri")!=provider.RedirectURL||
   r.Form.Get("code")!="vendor-code"||
   r.Form.Get("code_verifier")==""{
   t.Fatalf("token exchange omitted PKCE or changed provider scope")
  }
  return &http.Response{
   StatusCode:200,
   Header:http.Header{"Content-Type":[]string{"application/json"}},
   Body:io.NopCloser(strings.NewReader(
    `{"access_token":"CREDENTIAL-MUST-NOT-LEAK","token_type":"Bearer","refresh_token":"private-refresh"}`)),
  },nil
  }),
 }
 if err:=manager.SetOAuthBroker(broker,client);err!=nil{t.Fatal(err)}
 begin:=func(password,code string)*httptest.ResponseRecorder{
  raw,_:=json.Marshal(map[string]string{"password":password,"totp":code})
  req:=httptest.NewRequest("POST","/v1/oauth/provider/begin",bytes.NewReader(raw))
  req.Header.Set("Content-Type","application/json")
  response:=httptest.NewRecorder()
  manager.OwnerHandler().ServeHTTP(response,req)
  return response
 }
 code:=TOTPCode(key,now.Unix()/30)
 if got:=begin("incorrect",code);got.Code==200{
  t.Fatal("invalid owner factors initiated provider authorization")
 }
 started:=begin(testOwnerPassword,code)
 if started.Code!=200{t.Fatalf("verified owner could not start OAuth: %d %s",started.Code,started.Body.String())}
 var authorization OAuthAuthorization
 if err:=json.Unmarshal(started.Body.Bytes(),&authorization);err!=nil{t.Fatal(err)}
 parsed,err:=url.Parse(authorization.URL)
 if err!=nil{t.Fatal(err)}
 query:=parsed.Query()
 state:=query.Get("state")
 if parsed.Scheme!="https"||parsed.Host!="provider.example"||
  query.Get("response_type")!="code"||query.Get("code_challenge_method")!="S256"||
  query.Get("redirect_uri")!=provider.RedirectURL||len(state)<32{
  t.Fatalf("OAuth owner authorization URL changed scope: %v",parsed)
 }
 gateway:=httptest.NewRecorder()
 manager.ClientHandler().ServeHTTP(gateway,httptest.NewRequest("POST","/v1/oauth/provider/begin",nil))
 if gateway.Code==200{t.Fatal("gateway initiated provider authorization")}
 after:=begin(testOwnerPassword,code)
 if after.Code==200{t.Fatal("reused TOTP initiated a second OAuth authorization")}
 complete:=func(token string)*httptest.ResponseRecorder{
  target:="http://127.0.0.1:8765/oauth/callback?code=vendor-code&state="+url.QueryEscape(token)
  req:=httptest.NewRequest("GET",target,nil)
  result:=httptest.NewRecorder()
  manager.OAuthCallbackHandler().ServeHTTP(result,req)
  return result
 }
 if denied:=complete(state+"-wrong");denied.Code==200{
  t.Fatal("tampered OAuth state accepted")
 }
 approved:=complete(state)
 if approved.Code!=200||strings.Contains(approved.Body.String(),"CREDENTIAL-MUST-NOT-LEAK")||
  strings.Contains(approved.Body.String(),state) {
  t.Fatalf("owner OAuth callback exposed credential or denied enrollment: %d %s",approved.Code,approved.Body.String())
 }
 if exchanged!=1{t.Fatalf("unexpected token exchanges: %d",exchanged)}
 if duplicate:=complete(state);duplicate.Code==200{
  t.Fatal("OAuth state was replayed")
 }
 handles:=vault.List()
 if len(handles)!=1||handles[0].Provider!=provider.Name{
  t.Fatalf("OAuth credential not retained by independent vault: %+v",handles)
 }
}

func TestOAuthOwnerRejectsCrossOriginAndUnsafeProviderConfiguration(t *testing.T){
 manager,_,_:=testSessionManager(t)
 vault,err:=OpenVault(filepath.Join(privateDir(t),"oauth-vault.json"),[]byte(strings.Repeat("y",32)))
 if err!=nil{t.Fatal(err)}
 if err:=manager.SetVault(vault);err!=nil{t.Fatal(err)}
 base:=Provider{
  Name:"provider",AuthorizeURL:"https://provider.example/oauth/authorize",
  TokenURL:"https://provider.example/oauth/token",ClientID:"public-app",
  RedirectURL:"http://127.0.0.1:8765/oauth/callback",
 }
 for _,modified:=range []Provider{
  {Name:base.Name,AuthorizeURL:base.AuthorizeURL,TokenURL:"http://provider.example/token",ClientID:base.ClientID,RedirectURL:base.RedirectURL},
  {Name:base.Name,AuthorizeURL:base.AuthorizeURL,TokenURL:base.TokenURL,ClientID:base.ClientID,RedirectURL:"http://localhost:8765/oauth/callback"},
  {Name:base.Name,AuthorizeURL:base.AuthorizeURL,TokenURL:base.TokenURL,ClientID:base.ClientID,RedirectURL:"http://127.0.0.1:8765/other"},
  {Name:"invalid/provider",AuthorizeURL:base.AuthorizeURL,TokenURL:base.TokenURL,ClientID:base.ClientID,RedirectURL:base.RedirectURL},
 }{
  broker,err:=NewOAuthBroker([]Provider{modified})
  if err!=nil{continue}
  if err:=manager.SetOAuthBroker(broker,nil);err==nil{
   t.Fatalf("unsafe OAuth owner policy accepted: %+v",modified)
  }
 }
 good,err:=NewOAuthBroker([]Provider{base})
 if err!=nil{t.Fatal(err)}
 if err:=manager.SetOAuthBroker(good,nil);err!=nil{t.Fatal(err)}
 query:=url.Values{"state":{"pending-token"},"code":{"provider-code"}}
 req:=httptest.NewRequest("GET","http://attacker.invalid/oauth/callback?"+query.Encode(),nil)
 recorder:=httptest.NewRecorder()
 manager.OAuthCallbackHandler().ServeHTTP(recorder,req)
 if recorder.Code==200{t.Fatal("cross-origin OAuth callback accepted")}
}

func TestPKCEVerifierIsBoundToChallenge(t *testing.T){
 broker,err:=NewOAuthBroker([]Provider{{
  Name:"provider",AuthorizeURL:"https://provider.example/authorize",
  TokenURL:"https://provider.example/token",ClientID:"public-app",
  RedirectURL:"http://127.0.0.1:8765/oauth/callback",
 }})
 if err!=nil{t.Fatal(err)}
 authorization,err:=broker.Begin("provider")
 if err!=nil{t.Fatal(err)}
 u,err:=url.Parse(authorization.URL)
 if err!=nil{t.Fatal(err)}
 state:=u.Query().Get("state")
 err=broker.Finish(context.Background(),state,"proof-code",func(_ context.Context,_ Provider,code,verifier string)error{
  if code!="proof-code"{t.Fatal("authorization code substituted")}
  digest:=sha256.Sum256([]byte(verifier))
  calculated:=base64.RawURLEncoding.EncodeToString(digest[:])
  if calculated!=u.Query().Get("code_challenge"){t.Fatal("PKCE verifier did not match the issued challenge")}
  return nil
 })
 if err!=nil{t.Fatal(err)}
 if err=broker.Finish(context.Background(),state,"proof-code",func(context.Context,Provider,string,string)error{return nil});err==nil{
  t.Fatal("consumed OAuth state was replayed")
 }
}

func TestOwnerOAuthRefreshRotatesSecretWithFreshFactors(t *testing.T){
 manager,key,now:=testSessionManager(t)
 vault,err:=OpenVault(filepath.Join(privateDir(t),"oauth-vault.json"),[]byte(strings.Repeat("v",32)))
 if err!=nil{t.Fatal(err)}
 if err:=manager.SetVault(vault);err!=nil{t.Fatal(err)}
 provider:=Provider{
  Name:"vendor",AuthorizeURL:"https://provider.example/authorize",
  TokenURL:"https://provider.example/token",ClientID:"public-app",
  RedirectURL:"http://127.0.0.1:8765/oauth/callback",
 }
 broker,err:=NewOAuthBroker([]Provider{provider})
 if err!=nil{t.Fatal(err)}
 exchanged:=0
 client:=&http.Client{Transport:oauthTransport(func(r *http.Request)(*http.Response,error){
  exchanged++
  if r.Method!="POST"||r.URL.String()!=provider.TokenURL{
   t.Fatalf("refresh sent credential to wrong host: %s",r.URL.String())
  }
  if err:=r.ParseForm();err!=nil{t.Fatal(err)}
  if r.Form.Get("grant_type")!="refresh_token"||
   r.Form.Get("refresh_token")!="initial-refresh"||
   r.Form.Get("client_id")!=provider.ClientID{
   t.Fatal("refresh failed to preserve original source scope")
  }
  return &http.Response{StatusCode:200,
   Header:http.Header{"Content-Type":[]string{"application/json"}},
   Body:io.NopCloser(strings.NewReader(
    `{"access_token":"ROTATED-PRIVATE-ACCESS","token_type":"Bearer","expires_in":3600}`)),
  },nil
 })}
 if err:=manager.SetOAuthBroker(broker,client);err!=nil{t.Fatal(err)}
 handle,err:=vault.Store("vendor",[]byte(
  `{"access_token":"initial-access","token_type":"Bearer","refresh_token":"initial-refresh"}`))
 if err!=nil{t.Fatal(err)}
 before,err:=vault.Version(handle.ID,"vendor")
 if err!=nil{t.Fatal(err)}
 perform:=func(confirm bool, password,code string)*httptest.ResponseRecorder{
  payload,_:=json.Marshal(map[string]any{
   "handle":handle.ID,"confirm":confirm,"password":password,"totp":code,
  })
  req:=httptest.NewRequest("POST","/v1/oauth/vendor/refresh",bytes.NewReader(payload))
  req.Header.Set("Content-Type","application/json")
  result:=httptest.NewRecorder()
  manager.OwnerHandler().ServeHTTP(result,req)
  return result
 }
 code:=TOTPCode(key,now.Unix()/30)
 if response:=perform(false,testOwnerPassword,code);response.Code==200{
  t.Fatal("unconfirmed token refresh was allowed")
 }
 if response:=perform(true,"incorrect",code);response.Code==200{
  t.Fatal("unauthenticated token refresh was allowed")
 }
 response:=perform(true,testOwnerPassword,code)
 if response.Code!=200||strings.Contains(response.Body.String(),"ROTATED-PRIVATE-ACCESS"){
  t.Fatalf("confirmed refresh failed or leaked access token: %d %s",
   response.Code,response.Body.String())
 }
 if exchanged!=1{t.Fatalf("wrong number of provider refresh exchanges: %d",exchanged)}
 current,err:=vault.Version(handle.ID,"vendor")
 if err!=nil||current==before{
  t.Fatal("refresh did not change protected credential revision")
 }
 if err:=vault.WithCredentialVersion(handle.ID,"vendor",before,func(_ []byte)error{
  t.Fatal("stale scoped credential remained usable after refresh")
  return nil
 });err==nil{t.Fatal("old scoped credential version remained valid")}
 if err:=vault.WithCredentialVersion(handle.ID,"vendor",current,func(raw []byte)error{
  if !bytes.Contains(raw,[]byte("ROTATED-PRIVATE-ACCESS"))||
   !bytes.Contains(raw,[]byte("initial-refresh")){
   t.Fatal("refresh failed to preserve token rotation and existing refresh grant")
  }
  return nil
 });err!=nil{t.Fatal(err)}
 if again:=perform(true,testOwnerPassword,code);again.Code==200{
  t.Fatal("reused TOTP authorized second refresh")
 }
 if exchanged!=1{t.Fatal("replayed refresh reached upstream provider")}
}
