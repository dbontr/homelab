package agentflow

import (
 "context"
 "io"
 "net/http"
 "net/http/httptest"
 "testing"
)

func TestModelAuthorUsesCurrentCredentialOnly(t *testing.T){
 token:="model-authorized-token-identity-one"
 calls:=0
 upstream:=httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter,r *http.Request){
  calls++
  if r.Method!="POST"||r.Header.Get("Authorization")!="Bearer "+token{
   t.Error("unapproved model credential")
  }
  w.Header().Set("Content-Type","application/json")
  _,_=io.WriteString(w,`{"choices":[{"message":{"content":"{}"},"finish_reason":"stop"}]}`)
 }))
 defer upstream.Close()
 author,err:=NewModelAuthor(upstream.URL+"/v1/chat/completions","model","")
 if err!=nil{t.Fatal(err)}
 author.TokenSource=func()(string,error){return token,nil}
 for _,value:=range []string{"model-authorized-token-identity-one","model-authorized-token-identity-two"}{
  token=value
  text,err:=author.completeJSON(context.Background(),"system","payload")
  if err!=nil||text!="{}"{t.Fatalf("model token failed after rotation: %v",err)}
 }
 if calls!=2{t.Fatal("model did not retrieve credentials for both calls")}
 token="invalid token"
 if _,err:=author.completeJSON(context.Background(),"system","payload");err==nil{
  t.Fatal("malformed credential reached model provider")
 }
 if calls!=2{t.Fatal("invalid model key was transmitted")}
 author.Token="static-provider-token"
 if _,err:=author.completeJSON(context.Background(),"system","payload");err==nil{
  t.Fatal("conflicting model credential authorities accepted")
 }
}

func TestComputeSourceRequiresLiveNonAdminEditorGrant(t *testing.T){
 token:="editor-source-credential-current-value"
 identityChecks:=0
 projectChecks:=0
 upstream:=httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter,r *http.Request){
  if r.Header.Get("Authorization")!="Bearer "+token{t.Error("compute credential was stale")}
  w.Header().Set("Content-Type","application/json")
  switch r.URL.Path{
  case "/api/whoami":
   identityChecks++
   _,_=io.WriteString(w,`{"principal":"engineering-service","administrator":false}`)
  case "/api/projects/test/role":
   projectChecks++
   _,_=io.WriteString(w,`{"project_id":"test","role":"editor"}`)
  default: w.WriteHeader(404)
  }
 }))
 defer upstream.Close()
 compute,err:=NewComputeClientFromSource(upstream.URL+"/mcp",func()(string,error){return token,nil})
 if err!=nil{t.Fatal(err)}
 if err:=compute.VerifyProjectCredential(context.Background());err!=nil{t.Fatal(err)}
 if err:=compute.VerifyProjectEditor(context.Background(),"test");err!=nil{t.Fatal(err)}
 token="editor-source-credential-rotated-value"
 if err:=compute.VerifyProjectCredential(context.Background());err!=nil{t.Fatal(err)}
 if identityChecks!=2||projectChecks!=1{t.Fatal("project editor verification missing")}
 token="invalid token"
 if err:=compute.VerifyProjectCredential(context.Background());err==nil{
  t.Fatal("untrusted compute credential was sent")
 }
}

func TestModelRouteRejectsTwoCredentialSources(t *testing.T){
 _,err:=NewModelRouter([]ModelRoute{{Name:"planner",Purpose:"planning",
  Endpoint:"https://example.test/v1/chat/completions",Model:"bounded",
  Token:"plain-token",TokenSource:func()(string,error){return "second-token",nil}}})
 if err==nil{t.Fatal("duplicate model token sources accepted")}
}
