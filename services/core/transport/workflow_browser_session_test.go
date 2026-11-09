package transport

import (
 "encoding/json"
 "net/http"
 "net/http/httptest"
 "strings"
 "testing"

 "github.com/dbontr/homelab-new/system/security"
 "github.com/dbontr/homelab-new/system/session"
)

func TestOwnerBrowserCanManageOnlyAuthorizedWorkflowRoutes(t *testing.T){
 s,_,_:=flowFixture(t)
 password:="private-console-passphrase-for-workflows"
 record,err:=security.HashPassphrase([]byte(password))
 if err!=nil{t.Fatal(err)}
 manager,err:=session.New(record)
 if err!=nil{t.Fatal(err)}
 s.SetSessions(manager)
 if err:=s.SetAllowedOrigins([]string{"https://console.example"});err!=nil{t.Fatal(err)}
 handler:=s.Handler()
 call:=func(method,path,body string,cookie *http.Cookie,csrf string)*httptest.ResponseRecorder{
  req:=httptest.NewRequest(method,path,strings.NewReader(body))
  req.Host="console.example"
  req.Header.Set("Origin","https://console.example")
  if body!=""{req.Header.Set("Content-Type","application/json")}
  if cookie!=nil{req.AddCookie(cookie)}
  if csrf!=""{req.Header.Set("X-CSRF-Token",csrf)}
  response:=httptest.NewRecorder()
  handler.ServeHTTP(response,req)
  return response
 }
 login:=call(http.MethodPost,"/ui/session",`{"passphrase":"private-console-passphrase-for-workflows"}`,nil,"")
 if login.Code!=http.StatusOK||len(login.Result().Cookies())!=1{
  t.Fatalf("owner login failed: %d %s",login.Code,login.Body.String())
 }
 cookie:=login.Result().Cookies()[0]
 status:=call(http.MethodGet,"/ui/session","",cookie,"")
 var identity struct{CSRF string `json:"csrf_token"`}
 if err:=json.Unmarshal(status.Body.Bytes(),&identity);err!=nil||identity.CSRF==""{
  t.Fatalf("authenticated browser CSRF unavailable: %v",err)
 }
 program:=`{"project":"study","objective":"Review project evidence","steps":[{"id":"context","action":"project_context"}]}`
 if got:=call(http.MethodPost,"/v1/workflows",program,cookie,"");got.Code!=http.StatusUnauthorized{
  t.Fatalf("browser workflow bypassed CSRF: %d",got.Code)
 }
 workflow:=call(http.MethodPost,"/v1/workflows",program,cookie,identity.CSRF)
 if workflow.Code!=http.StatusAccepted{
  t.Fatalf("browser workflow submission unavailable: %d %s",workflow.Code,workflow.Body.String())
 }
 var created struct{ID string `json:"id"`;Revision uint64 `json:"revision"`}
 if err:=json.Unmarshal(workflow.Body.Bytes(),&created);err!=nil||created.ID==""||created.Revision==0{
  t.Fatalf("invalid native workflow identity: %+v %v",created,err)
 }
 cancel:=call(http.MethodPost,"/v1/workflows/"+created.ID+"/cancel",
  `{"revision":`+jsonNumber(created.Revision)+`}`,cookie,identity.CSRF)
 if cancel.Code!=http.StatusOK{
  t.Fatalf("browser workflow cancellation unavailable: %d %s",cancel.Code,cancel.Body.String())
 }
 scheduleBody:=`{"program":{"project":"study","objective":"Weekly progress",
  "personal_read":true,"steps":[{"id":"report","action":"weekly_report"}]},
  "every_minutes":10080,"enabled":true,"notify_on_every_run":true}`
 if got:=call(http.MethodPost,"/v1/automations",scheduleBody,cookie,"");got.Code!=http.StatusUnauthorized{
  t.Fatalf("weekly schedule bypassed CSRF: %d",got.Code)
 }
 schedule:=call(http.MethodPost,"/v1/automations",scheduleBody,cookie,identity.CSRF)
 if schedule.Code!=http.StatusCreated{
  t.Fatalf("browser weekly schedule unavailable: %d %s",schedule.Code,schedule.Body.String())
 }
 var automation struct{ID string `json:"id"`;Revision uint64 `json:"revision"`;Notify bool `json:"notify_on_every_run"`}
 if err:=json.Unmarshal(schedule.Body.Bytes(),&automation);err!=nil||automation.ID==""||!automation.Notify{
  t.Fatalf("weekly report was not created: %+v %v",automation,err)
 }
 enabled:=call(http.MethodPost,"/v1/automations/"+automation.ID+"/enabled",
  `{"revision":`+jsonNumber(automation.Revision)+`,"enabled":false}`,cookie,identity.CSRF)
 if enabled.Code!=http.StatusOK{
  t.Fatalf("browser schedule pause unavailable: %d %s",enabled.Code,enabled.Body.String())
 }
 planning:=call(http.MethodPost,"/v1/workflows/plan",
  `{"project":"study","objective":"Inspect the sources"}`,cookie,identity.CSRF)
 if planning.Code!=http.StatusServiceUnavailable{
  t.Fatalf("workflow planning did not reach the unavailable model boundary: %d %s",
   planning.Code,planning.Body.String())
 }
 if got:=call(http.MethodPost,"/mcp",`{"jsonrpc":"2.0","id":1,"method":"tools/list"}`,
  cookie,identity.CSRF);got.Code!=http.StatusUnauthorized{
  t.Fatalf("browser session gained arbitrary MCP authorization: %d",got.Code)
 }
}

func jsonNumber(value uint64)string{
 raw,_:=json.Marshal(value)
 return string(raw)
}
