package personal

import (
 "encoding/json"
 "net/http"
 "net/http/httptest"
 "path/filepath"
 "strings"
 "testing"

 "github.com/dbontr/homelab-new/system/store"
)

func call(h http.Handler,method,path,payload string)*httptest.ResponseRecorder{
 request:=httptest.NewRequest(method,path,strings.NewReader(payload))
 response:=httptest.NewRecorder()
 h.ServeHTTP(response,request)
 return response
}

func TestPersonalRoutesPreserveExistingStoreAndResponseContracts(t *testing.T){
 state:=filepath.Join(t.TempDir(),"state.json")
 db,err:=store.Open(state,"UTC")
 if err!=nil{t.Fatal(err)}
 router:=http.NewServeMux()
 Register(router,db)
 if response:=call(router,"GET","/v1/tasks","");response.Code!=http.StatusOK{
  t.Fatalf("task listing returned %d",response.Code)
 }
 response:=call(router,"POST","/v1/tasks",`{"title":"Review control systems","minutes":20}`)
 if response.Code!=http.StatusCreated{t.Fatalf("task creation returned %d: %s",response.Code,response.Body.String())}
 var task store.Task
 if err:=json.Unmarshal(response.Body.Bytes(),&task);err!=nil{t.Fatal(err)}
 if task.ID==""||task.Title!="Review control systems"{t.Fatal("task response changed",task)}
 if response=call(router,"GET","/v1/tasks","");response.Code!=http.StatusOK||
  !strings.Contains(response.Body.String(),task.ID){t.Fatal("task listing omitted persistent record")}
 if response=call(router,"POST","/v1/tasks/"+task.ID+"/complete","");response.Code!=http.StatusOK{
  t.Fatalf("task completion returned %d: %s",response.Code,response.Body.String())
 }
 if response=call(router,"POST","/v1/goals",`{"title":"Understand feedback"}`);response.Code!=http.StatusCreated{
  t.Fatalf("goal creation returned %d",response.Code)
 }
 if response=call(router,"PUT","/v1/rules/study",`{"text":"Review weekly","kind":"preference"}`);response.Code!=http.StatusOK{
  t.Fatalf("rule update returned %d: %s",response.Code,response.Body.String())
 }
 if response=call(router,"GET","/v1/recommendations","");response.Code!=http.StatusOK{
  t.Fatalf("recommendation query returned %d",response.Code)
 }
 reopened,err:=store.Open(state,"UTC")
 if err!=nil{t.Fatal(err)}
 if len(reopened.Tasks(""))!=1||reopened.Tasks("")[0].Status!="completed"{
  t.Fatal("personal routes did not retain the existing state-file contract")
 }
}

func TestPersonalRouterHasNoGenericToolsOrAssistantEntryPoint(t *testing.T){
 db,err:=store.Open(filepath.Join(t.TempDir(),"state.json"),"UTC")
 if err!=nil{t.Fatal(err)}
 router:=http.NewServeMux()
 Register(router,db)
 for _,path:=range []string{"/mcp","/v1/tools","/ui/session","/v1/health/records"}{
  if response:=call(router,"GET",path,"");response.Code!=http.StatusNotFound{
   t.Fatalf("personal service exposed unrelated route %s: %d",path,response.Code)
  }
 }
 response:=call(router,"POST","/v1/tasks",`{"title":"Valid","unknown_property":true}`)
 if response.Code!=http.StatusBadRequest{
  t.Fatalf("unknown task attributes must be rejected, got %d",response.Code)
 }
}
