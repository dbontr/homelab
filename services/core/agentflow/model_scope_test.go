package agentflow

import (
 "context"
 "encoding/json"
 "errors"
 "io"
 "net/http"
 "net/http/httptest"
 "testing"
)

func TestModelProviderTokenIsBoundToExactRequestDigest(t *testing.T){
 const token="model-provider-scope-token-example"
 requests:=0
 endpoint:=httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter,r *http.Request){
  requests++
  if r.Method!=http.MethodPost||r.URL.Path!="/v1/chat/completions"||
   r.Header.Get("Authorization")!="Bearer "+token{
   t.Error("provider received unexpected credential or request")
  }
  w.Header().Set("Content-Type","application/json")
  _,_=io.WriteString(w,`{"choices":[{"message":{"content":"{}"},"finish_reason":"stop"}]}`)
 }))
 defer endpoint.Close()
 model,err:=NewModelAuthor(endpoint.URL+"/v1/chat/completions","exact-model","")
 if err!=nil{t.Fatal(err)}
 expectedCalls:=0
 model.ScopedTokenSource=func(
  ctx context.Context,operation,target string,args json.RawMessage,
 )(string,error){
  expectedCalls++
  if ctx.Value("owner")==nil{return "",errors.New("owner assertion unavailable")}
  if operation!="model__completion"||target!="exact-model"{
   t.Error("model authorization scope changed")
  }
  var payload map[string]string
  if err:=json.Unmarshal(args,&payload);err!=nil{return "",err}
  if payload["model"]!=target{return "",errors.New("wrong model target")}
  // The callback receives no system instructions, user prompt or API key.
  if len(payload)!=2||len(payload["request_sha256"])!=64{
   return "",errors.New("model prompt was not digest-bound")
  }
  return token,nil
 }
 ctx:=context.WithValue(context.Background(),"owner",true)
 output,err:=model.completeJSON(ctx,"system instructions","user content")
 if err!=nil||output!="{}"||requests!=1||expectedCalls!=1{
  t.Fatalf("authorized model request failed: %q %v",output,err)
 }
 // The token source has no ambient fallback when an owner context is absent.
 if _,err:=model.completeJSON(context.Background(),"system","user");err==nil{
  t.Fatal("model accepted a scoped credential without the owner context")
 }
 if requests!=1||expectedCalls!=2{
  t.Fatal("denied model call reached the provider")
 }
 // Two credential authorities must never be silently combined.
 model.Token="static-token"
 if _,err:=model.completeJSON(ctx,"system","user");err==nil{
  t.Fatal("model accepted duplicate credential authorities")
 }
 if requests!=1{t.Fatal("conflicting model authority was sent to provider")}
}
