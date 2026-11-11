package controller

import (
 "context"
 "encoding/json"
 "errors"
 "io"
 "net/http"
 "net/http/httptest"
 "strings"
 "testing"
)

func TestProviderScopeRedeemsOneTicketPerRequestAndNeverSharesSecrets(t *testing.T){
 const token="ghp_private-scoped-token-example"
 const alias="github_token"
 const operation="git_repository_head"
 const assertion="signed-owner"
 args:=json.RawMessage(`{"project_id":"project-a","repository":"dbontr/research","ref":"main"}`)
 authorized,redeemed,providerCalls:=0,0,0
 owner:=httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter,r *http.Request){
  switch r.URL.Path{
  case "/v1/authorize":
   authorized++
   if r.Method!="POST"||r.Header.Get("Cf-Access-Jwt-Assertion")!=assertion{
    http.Error(w,"identity denied",http.StatusForbidden)
    return
   }
   var input struct{
    Operation string `json:"operation"`
    Target string `json:"target"`
    Arguments json.RawMessage `json:"arguments"`
   }
   if err:=json.NewDecoder(r.Body).Decode(&input);err!=nil||
    input.Operation!=operation||input.Target!="controller"||
    string(input.Arguments)!=string(args){
    t.Error("owner authorization scope changed",err,input)
   }
   w.Header().Set("Content-Type","application/json")
   _,_=io.WriteString(w,`{"allowed":true,"credential_tickets":[{"alias":"github_token","ticket":"`+
    strings.Repeat("a",48)+`"}]}`)
  case "/v1/service-credentials/github_token":
   redeemed++
   if r.Method!="POST"{
    t.Error("credential redeemed using an unsafe method")
   }
   var input struct{
    Ticket string `json:"ticket"`
    Operation string `json:"operation"`
    Target string `json:"target"`
    Arguments json.RawMessage `json:"arguments"`
   }
   if err:=json.NewDecoder(r.Body).Decode(&input);err!=nil||
    input.Ticket!=strings.Repeat("a",48)||input.Operation!=operation||
    input.Target!="controller"||string(input.Arguments)!=string(args){
    t.Error("credential redemption scope changed",err,input)
   }
   w.Header().Set("Content-Type","application/octet-stream")
   _,_=io.WriteString(w,token)
  default:
   t.Error("unexpected owner vault endpoint",r.URL.Path)
   http.NotFound(w,r)
  }
 }))
 defer owner.Close()
 provider:=httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter,r *http.Request){
  providerCalls++
  if r.Header.Get("Authorization")!="Bearer "+token{
   t.Error("provider did not receive the authorized token")
  }
  w.Header().Set("Content-Type","application/json")
  _,_=io.WriteString(w,`{"full_name":"dbontr/research"}`)
 }))
 defer provider.Close()
 github:=NewGitHub("")
 github.API=provider.URL
 github.TokenSource=func()(string,error){return "",errors.New("ambient access denied")}
 original:=&Server{
  GitHub:github,VaultBroker:&VaultSecretClient{client:owner.Client(),endpoint:owner.URL},
  VaultGitHubAlias:alias,
 }
 scoped,clear:=original.forProviderOperation(context.Background(),assertion,operation,args)
 for i:=0;i<2;i++{
  if _,err:=scoped.GitHub.Repo("dbontr/research");err!=nil{t.Fatal(err)}
 }
 clear()
 if authorized!=1||redeemed!=1||providerCalls!=2{
  t.Fatalf("credential scope reused incorrectly: authorize %d redeem %d provider %d",
   authorized,redeemed,providerCalls)
 }
 if _,err:=original.GitHub.Repo("dbontr/research");err==nil{
  t.Fatal("persistent provider gained access to request credential")
 }
 noIdentity,release:=original.forProviderOperation(context.Background(),"",operation,args)
 defer release()
 if _,err:=noIdentity.GitHub.Repo("dbontr/research");err==nil{
  t.Fatal("provider credential accepted without signed owner identity")
 }
 if redeemed!=1||providerCalls!=2{
  t.Fatal("denied operation leaked credential to provider")
 }
}

func TestProviderHTTPContextBindsMethodPathQueryAndJSONBody(t *testing.T){
 original:=&Server{
  GitHub:NewGitHub(""),
  VaultBroker:&VaultSecretClient{},
  VaultGitHubAlias:"github_token",
 }
 r:=httptest.NewRequest("POST","https://controller.internal/api/projects/a/github/issues?repository=dbontr%2Fresearch",
  strings.NewReader(`{"title":"Issue","body":"Text"}`))
 scoped,clear,err:=original.forProviderHTTP(r)
 if err!=nil{t.Fatal(err)}
 defer clear()
 if scoped==original||scoped.GitHub==original.GitHub{
  t.Fatal("provider clients were shared between request scopes")
 }
 restored,err:=io.ReadAll(r.Body)
 if err!=nil||string(restored)!=`{"title":"Issue","body":"Text"}`{
  t.Fatal("REST handler body was consumed by owner authorization",err)
 }
 tooLarge:=httptest.NewRequest("POST","https://controller.internal/api/projects/a/github/issues",
  strings.NewReader(strings.Repeat("x",256<<10+1)))
 if _,_,err:=original.forProviderHTTP(tooLarge);err==nil{
  t.Fatal("oversized provider request was accepted")
 }
}

func TestProviderAuthorizationRejectsMalformedAndUnscopedDecisions(t *testing.T){
 reply:=`{"allowed":false,"reason":"owner authentication required"}`
 calls:=0
 server:=httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter,r *http.Request){
  calls++
  w.Header().Set("Content-Type","application/json")
  _,_=io.WriteString(w,reply)
 }))
 defer server.Close()
 broker:=&VaultSecretClient{client:server.Client(),endpoint:server.URL}
 args:=json.RawMessage(`{"repository":"dbontr/research"}`)
 if _,err:=broker.AuthorizeOperation(context.Background(),"","git_repository_head","controller",args);err==nil||calls!=0{
  t.Fatal("unsigned request reached the authority")
 }
 for _,invalid:=range []string{
  `{"allowed":false,"credential_tickets":[{"alias":"github_token","ticket":"`+strings.Repeat("a",48)+`"}]}`,
  `{"allowed":true,"reason":"denied"}`,
  `{"allowed":true,"credential_tickets":[{"alias":"github_token","ticket":"invalid"}]}`,
  `{"allowed":true,"credential_tickets":[{"alias":"github_token","ticket":"`+strings.Repeat("a",48)+
   `"},{"alias":"github_token","ticket":"`+strings.Repeat("b",48)+`"}]}`,
 }{
  reply=invalid
  if _,err:=broker.AuthorizeOperation(context.Background(),"signed-owner",
   "git_repository_head","controller",args);err==nil{
   t.Fatal("malformed owner capability accepted")
  }
 }
}
