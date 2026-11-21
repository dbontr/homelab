package transport

import (
 "encoding/json"
 "net/http"
 "net/http/httptest"
 "path/filepath"
 "strings"
 "testing"

 "github.com/dbontr/homelab-new/system/store"
)

func TestPersonalRulesOwnerCSRFAndCanonicalMCPParity(t *testing.T){
 db,err:=store.Open(filepath.Join(t.TempDir(),"state.json"),"UTC");if err!=nil{t.Fatal(err)}
 svc,err:=New(db,strings.Repeat("z",40),nil);if err!=nil{t.Fatal(err)}
 manager,assertion:=accessTransportFixture(t)
 svc.SetSessions(manager)
 if err:=svc.SetAllowedOrigins([]string{"https://console.example"});err!=nil{t.Fatal(err)}
 csrf,valid:=manager.Check(assertion);if !valid{t.Fatal("signed owner fixture invalid")}
 router:=svc.Handler()
 send:=func(method,path,body,auth,token string)*httptest.ResponseRecorder{
  req:=httptest.NewRequest(method,path,strings.NewReader(body))
  req.Host="console.example"
  req.Header.Set("Origin","https://console.example")
  req.Header.Set("Content-Type","application/json")
  if auth!=""{req.Header.Set("Cf-Access-Jwt-Assertion",auth)}
  if token!=""{req.Header.Set("X-CSRF-Token",token)}
  w:=httptest.NewRecorder();router.ServeHTTP(w,req);return w
 }
 path:="/v1/rules/study/policy"
 body:=`{"expected_revision":0,"text":"Study for bounded intervals",
 "kind":"constraint","enabled":true,"paused":false,"priority":5,
 "scope":"global","effect":"max_minutes","value":"30"}`
 if got:=send("PUT",path,body,"","");got.Code!=http.StatusUnauthorized{
  t.Fatalf("unauthenticated rule write admitted: %d",got.Code)
 }
 if got:=send("PUT",path,body,assertion,"");got.Code!=http.StatusUnauthorized{
  t.Fatalf("missing CSRF admitted: %d",got.Code)
 }
 if got:=send("PUT",path,body,assertion,csrf);got.Code!=http.StatusOK{
  t.Fatalf("owner rule write rejected: %d %s",got.Code,got.Body.String())
 }
 if got:=send("PUT",path,body,assertion,csrf);got.Code!=http.StatusConflict{
  t.Fatalf("stale owner rule revision accepted: %d",got.Code)
 }
 if got:=send("PUT","/v1/rules/study/policy/extraneous",body,assertion,csrf);got.Code!=http.StatusUnauthorized{
  t.Fatalf("unbounded owner rule path admitted: %d",got.Code)
 }
 if got:=send("GET","/v1/rules","","","");got.Code!=http.StatusUnauthorized{
  t.Fatalf("anonymous rule read admitted: %d",got.Code)
 }
 if got:=send("GET","/v1/rules","",assertion,"");got.Code!=http.StatusOK{
  t.Fatalf("owner rule read rejected: %d",got.Code)
 }
 value,err:=svc.localTool(httptest.NewRequest("GET","/",nil).Context(),"rule_list",nil)
 if err!=nil{t.Fatal(err)}
 rows,ok:=value.([]store.Rule)
 if !ok||len(rows)!=1||rows[0].Revision!=1{t.Fatalf("MCP store differs: %#v",value)}
 call:=map[string]any{"minutes":120,"limit":10}
 raw,err:=json.Marshal(call);if err!=nil{t.Fatal(err)}
 advice,err:=svc.localTool(httptest.NewRequest("GET","/",nil).Context(),"task_suggest",raw)
 if err!=nil{t.Fatal(err)}
 if _,ok:=advice.(store.RecommendationResult);!ok{t.Fatalf("MCP advice uses different model: %T",advice)}
}
