package mediahttp

import (
 "io"
 "net/http"
 "net/http/httptest"
 "strings"
 "testing"

 "github.com/dbontr/homelab-new/system/media"
)

func mediaCall(h http.Handler,method,path,body string)*httptest.ResponseRecorder{
 w:=httptest.NewRecorder()
 h.ServeHTTP(w,httptest.NewRequest(method,path,strings.NewReader(body)))
 return w
}

func TestMediaRoutesForwardSearchAndRequestsWithoutOwningSeerrState(t *testing.T){
 created:=0
 upstream:=httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter,r *http.Request){
  if r.Header.Get("X-Api-Key")!="seerr-private-service-credential"{
   t.Error("media backend did not use scoped service identity")
  }
  if r.URL.Path=="/api/v1/request"&&r.Method==http.MethodPost{created++}
  w.Header().Set("Content-Type","application/json")
  _,_=io.WriteString(w,`{"results":[{"title":"Example"}]}`)
 }))
 defer upstream.Close()
 client,err:=media.NewSeerr(upstream.URL,"seerr-private-service-credential")
 if err!=nil{t.Fatal(err)}
 mux:=http.NewServeMux()
 Register(mux,client)
 if w:=mediaCall(mux,"GET","/v1/media/search?query=Example","");w.Code!=200||
  !strings.Contains(w.Body.String(),"Example"){t.Fatalf("media search failed: %d",w.Code)}
 if w:=mediaCall(mux,"GET","/v1/media/requests","");w.Code!=200{
  t.Fatalf("media request listing failed: %d",w.Code)
 }
 if w:=mediaCall(mux,"POST","/v1/media/requests",`{"mediaType":"movie","mediaId":5}`);w.Code!=201{
  t.Fatalf("media request failed: %d %s",w.Code,w.Body.String())
 }
 if created!=1{t.Fatal("media request was not sent to Seerr")}
 if w:=mediaCall(mux,"POST","/v1/media/requests",`{"mediaType":"other","mediaId":5}`);w.Code!=502{
  t.Fatalf("unapproved media type returned %d",w.Code)
 }
 if w:=mediaCall(mux,"GET","/mcp","");w.Code!=404{
  t.Fatal("media adapter exposed an MCP endpoint")
 }
}

func TestUnconfiguredMediaIsExplicitlyUnavailable(t *testing.T){
 mux:=http.NewServeMux()
 Register(mux,nil)
 if w:=mediaCall(mux,"GET","/v1/media/requests","");w.Code!=503{
  t.Fatalf("unconfigured Seerr returned %d",w.Code)
 }
}
