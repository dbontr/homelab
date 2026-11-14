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

func TestMotivationAdmitsOnlyDomainOperations(t *testing.T){
 if !admitted("GET","/v1/motivation/metrics") ||
  !admitted("POST","/v1/motivation/checkins") ||
  !admitted("POST","/v1/motivation/dismiss") ||
  !admitted("POST","/v1/motivation/focus") ||
  !admitted("POST","/v1/motivation/focus/end") {
  t.Fatal("expected motivation operations are not admitted")
 }
 for _,path:=range []string{
   "/v1/motivation/../state", "/v1/tools", "/v1/recipes",
   "/v1/motivation/profile/../../v1/security",
 }{
  if admitted("POST",path){t.Fatalf("unexpected cross-domain route: %s",path)}
 }
}

func TestAbsentSuggestionArgumentsBecomeDefaultContext(t *testing.T) {
 token:=strings.Repeat("x",40)
 upstream:=httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter,r *http.Request){
  if r.Header.Get("Content-Type")!="application/json"{t.Error("missing content type")}
  var payload map[string]any
  if err:=json.NewDecoder(r.Body).Decode(&payload);err!=nil{t.Errorf("invalid default context: %v",err)}
  w.Header().Set("Content-Type","application/json")
  _,_=w.Write([]byte(`{"id":1}`))
 }))
 defer upstream.Close()
 client,err:=New(upstream.URL,token)
 if err!=nil{t.Fatal(err)}
 if _,err:=client.Call(context.Background(),"POST","/v1/motivation/interventions",nil);err!=nil{t.Fatal(err)}
}

func TestMissingServiceIsUnavailable(t *testing.T){
 mux:=http.NewServeMux()
 Register(mux,nil)
 r:=httptest.NewRecorder()
 mux.ServeHTTP(r,httptest.NewRequest("GET","/v1/motivation/state",nil))
 if r.Code!=http.StatusServiceUnavailable{t.Fatalf("got %d",r.Code)}
}
