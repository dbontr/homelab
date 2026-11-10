package motivationhttp

import (
 "context"
 "encoding/json"
 "net/http"
 "net/http/httptest"
 "strings"
 "testing"
)

func TestRejectsInvalidEndpointsAndTokens(t *testing.T){
 for _,endpoint:=range []string{
  "http://example.com:8322","https://127.0.0.1:8322",
  "http://localhost:8322","http://127.0.0.1:8322/path",
  "http://127.0.0.1:8322/?token=secret",
  "http://127.0.0.1",
 } {
  if _,err:=New(endpoint,strings.Repeat("x",40));err==nil{t.Errorf("accepted endpoint %s",endpoint)}
 }
 if _,err:=New("http://127.0.0.1:8322","short");err==nil{t.Fatal("accepted short token")}
}

func TestOwnerTransportProxiesExactRoutesWithoutCallerHeaders(t *testing.T){
 secret:=strings.Repeat("z",40)
 var seen string
 upstream:=httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter,r *http.Request){
  seen=r.Header.Get("Authorization")
  if r.URL.Path!="/v1/motivation/state"{t.Errorf("unexpected path %s",r.URL.Path)}
  w.Header().Set("Content-Type","application/json")
  _,_=w.Write([]byte(`{"profile":{"enabled":false}}`))
 }))
 defer upstream.Close()
 c,err:=New(upstream.URL,secret)
 if err!=nil{t.Fatal(err)}
 var value map[string]any
 serverMux:=http.NewServeMux()
 Register(serverMux,c)
 request:=httptest.NewRequest("GET","/v1/motivation/state",nil)
 request.Header.Set("Authorization","Bearer attacker")
 response:=httptest.NewRecorder()
 serverMux.ServeHTTP(response,request)
 if response.Code!=200{t.Fatalf("status %d: %s",response.Code,response.Body.String())}
 if seen!="Bearer "+secret{t.Fatal("upstream service identity not replaced")}
 if err:=json.Unmarshal(response.Body.Bytes(),&value);err!=nil{t.Fatal(err)}
 if value["profile"]==nil{t.Fatal("missing service result")}
 if _,err:=c.Call(context.Background(),"GET","/v1/motivation/../state",nil);err==nil{
  t.Fatal("accepted unrestricted path")
 }
}

func TestMissingServiceIsUnavailable(t *testing.T){
 mux:=http.NewServeMux()
 Register(mux,nil)
 r:=httptest.NewRecorder()
 mux.ServeHTTP(r,httptest.NewRequest("GET","/v1/motivation/state",nil))
 if r.Code!=http.StatusServiceUnavailable{t.Fatalf("got %d",r.Code)}
}
