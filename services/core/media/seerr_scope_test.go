package media

import (
 "context"
 "crypto/sha256"
 "encoding/hex"
 "encoding/json"
 "errors"
 "io"
 "net/http"
 "net/http/httptest"
 "strings"
 "testing"
)

type mediaOwnerKey struct{}

func TestSeerrCredentialIsBoundToOneOwnerApprovedMediaAction(t *testing.T){
 const secret="seerr-scoped-api-key-example"
 requests:=0
 upstream:=httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter,r *http.Request){
  requests++
  if r.Header.Get("X-Api-Key")!=secret{
   t.Error("Seerr provider received no scoped credential")
  }
  w.Header().Set("Content-Type","application/json")
  _,_=io.WriteString(w,`{"results":[]}`)
 }))
 defer upstream.Close()
 client,err:=NewSeerrWithScopedSource(upstream.URL,func(
  ctx context.Context,operation,target string,args json.RawMessage,
 )(string,error){
  if ctx.Value(mediaOwnerKey{})==nil{
   return "",errors.New("owner is not authenticated")
  }
  if operation!="media__seerr_request"||target!="seerr"{
   return "",errors.New("invalid media authorization scope")
  }
  var binding map[string]string
  if err:=json.Unmarshal(args,&binding);err!=nil{return "",err}
  digest:=sha256.Sum256(nil)
  if binding["method"]!="GET"||binding["path"]!="/api/v1/search?query=Dune"||
   binding["body_sha256"]!=hex.EncodeToString(digest[:]){
   return "",errors.New("Seerr authorization did not bind exact method, path and body digest")
  }
  return secret,nil
 })
 if err!=nil{t.Fatal(err)}
 ctx:=context.WithValue(context.Background(),mediaOwnerKey{},true)
 if _,err:=client.Search(ctx,"Dune");err!=nil{t.Fatal(err)}
 if _,err:=client.Search(context.Background(),"Dune");err==nil{
  t.Fatal("Seerr provider accepted missing owner authorization")
 }
 if requests!=1{t.Fatal("unauthorized Seerr call reached provider")}
}

func TestSeerrRejectsMultipleCredentialAuthorities(t *testing.T){
 endpoint:="https://seerr.example:443"
 source:=func()(string,error){return strings.Repeat("a",32),nil}
 scoped:=func(context.Context,string,string,json.RawMessage)(string,error){
  return strings.Repeat("b",32),nil
 }
 if _,err:=newSeerr(endpoint,"static",source,nil);err==nil{
  t.Fatal("Seerr accepted static and ambient provider identities")
 }
 if _,err:=newSeerr(endpoint,"",source,scoped);err==nil{
  t.Fatal("Seerr accepted ambient and owner-scoped provider identities")
 }
 if _,err:=NewSeerrWithScopedSource(endpoint,nil);err==nil{
  t.Fatal("Seerr accepted a missing owner credential source")
 }
}
