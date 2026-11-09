package controller

import (
 "context"
 "encoding/json"
 "errors"
 "net/http"
 "net/http/httptest"
 "testing"
)

func TestEmbeddingProviderReadsCurrentVaultCredential(t *testing.T){
 token:="embedding-provider-scoped-token-one"
 calls:=0
 server:=httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter,r *http.Request){
  calls++
  if r.Method!=http.MethodPost||r.Header.Get("Authorization")!="Bearer "+token{
   t.Error("embedding request did not use the current scoped token")
  }
  w.Header().Set("Content-Type","application/json")
  _=json.NewEncoder(w).Encode(map[string]any{
   "model":"embedding-test",
   "data":[]any{map[string]any{"index":0,"embedding":[]float64{1,0,0,0,0,0,0,0}}},
  })
 }))
 defer server.Close()
 provider,err:=NewEmbeddingProvider(server.URL+"/v1/embeddings","embedding-test","")
 if err!=nil{t.Fatal(err)}
 provider.TokenSource=func()(string,error){return token,nil}
 for _,next:=range []string{
  "embedding-provider-scoped-token-one","embedding-provider-scoped-token-two",
 }{
  token=next
  if _,err:=provider.Embed(context.Background(),"approved passage");err!=nil{t.Fatal(err)}
 }
 if calls!=2{t.Fatal("embedding credential source was not called for each request")}
 provider.TokenSource=func()(string,error){return "",errors.New("vault revoked")}
 if _,err:=provider.Embed(context.Background(),"approved passage");err==nil{
  t.Fatal("revoked embedding credential accepted")
 }
 if calls!=2{t.Fatal("embedding request escaped after vault revocation")}
 provider.Token="conflicting-secret"
 if _,err:=provider.Embed(context.Background(),"approved passage");err==nil{
  t.Fatal("two embedding credential authorities accepted")
 }
}
