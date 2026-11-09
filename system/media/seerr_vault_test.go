package media

import (
 "context"
 "io"
 "net/http"
 "net/http/httptest"
 "testing"
)

func TestSeerrVaultCredentialSourceHonorsRotationAndRevocation(t *testing.T){
 key:="seerr-owner-first-credential"
 requests:=0
 upstream:=httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter,r *http.Request){
  requests++
  if got:=r.Header.Get("X-Api-Key");got!=key{
   t.Errorf("media credential changed unexpectedly: %q",got)
  }
  w.Header().Set("Content-Type","application/json")
  _,_=io.WriteString(w,`{"results":[]}`)
 }))
 defer upstream.Close()
 source:=func()(string,error){return key,nil}
 client,err:=NewSeerrWithSource(upstream.URL,source)
 if err!=nil{t.Fatal(err)}
 if _,err:=client.Search(context.Background(),"test");err!=nil{t.Fatal(err)}
 key="seerr-owner-second-credential"
 if _,err:=client.Search(context.Background(),"test");err!=nil{t.Fatal(err)}
 if requests!=2{t.Fatal("vault token was cached rather than fetched on demand")}
 key="bad key"
 if _,err:=client.Search(context.Background(),"test");err==nil{
  t.Fatal("invalid rotated key was sent to media provider")
 }
 if requests!=2{t.Fatal("invalid token reached media service")}
 if _,err:=NewSeerrWithSource(upstream.URL,nil);err==nil{
  t.Fatal("missing source was accepted")
 }
}
