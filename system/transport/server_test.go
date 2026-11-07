package transport

import (
 "bytes"
 "encoding/json"
 "net/http"
 "net/http/httptest"
 "path/filepath"
 "strings"
 "testing"
 "github.com/dbontr/homelab-new/system/store"
)
func makeTestServer(t *testing.T,backend []Backend)http.Handler{
 t.Helper();db,e:=store.Open(filepath.Join(t.TempDir(),"state.json"),"UTC");if e!=nil{t.Fatal(e)}
 s,e:=New(db,strings.Repeat("a",40),backend);if e!=nil{t.Fatal(e)}
 return s.Handler()
}
func request(h http.Handler,method,path,payload,token string)*httptest.ResponseRecorder{
 req:=httptest.NewRequest(method,path,bytes.NewBufferString(payload))
 if token!=""{req.Header.Set("Authorization","Bearer "+token)}
 w:=httptest.NewRecorder();h.ServeHTTP(w,req);return w
}
func TestNativeAPIAndAccessControl(t *testing.T){
 h:=makeTestServer(t,nil)
 if w:=request(h,"GET","/v1/tasks","","");w.Code!=401{t.Fatalf("unauthorized status %d",w.Code)}
 token:=strings.Repeat("a",40)
 w:=request(h,"POST","/v1/tasks",`{"title":"Pick up supplies","minutes":15}`,token)
 if w.Code!=201{t.Fatalf("create %d %s",w.Code,w.Body.String())}
 var task store.Task
 if e:=json.Unmarshal(w.Body.Bytes(),&task);e!=nil{t.Fatal(e)}
 w=request(h,"POST","/v1/tasks/"+task.ID+"/complete","",token)
 if w.Code!=200{t.Fatalf("complete %s",w.Body.String())}
 w=request(h,"GET","/v1/reports/weekly","",token)
 if w.Code!=200||!strings.Contains(w.Body.String(),"total_completed"){t.Fatalf("report %s",w.Body.String())}
}
func TestMCPToolDiscoveryAndTaskExecution(t *testing.T){
 h:=makeTestServer(t,nil);token:=strings.Repeat("a",40)
 w:=request(h,"POST","/mcp",`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{}}`,token)
 if w.Code!=200||!strings.Contains(w.Body.String(),"2025-06-18"){t.Fatalf("initialize %s",w.Body.String())}
 w=request(h,"POST","/mcp",`{"jsonrpc":"2.0","id":2,"method":"tools/list"}`,token)
 if !strings.Contains(w.Body.String(),"task_add"){t.Fatalf("list %s",w.Body.String())}
 w=request(h,"POST","/mcp",`{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"task_add","arguments":{"title":"Review Fourier series"}}}`,token)
 if !strings.Contains(w.Body.String(),"Review Fourier series"){t.Fatalf("add %s",w.Body.String())}
}
func TestFederatedToolCalls(t *testing.T){
 downstream:=httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter,r *http.Request){
  if r.Header.Get("Authorization")!="Bearer internal-secret"{http.Error(w,"unauthorized",401);return}
  var input struct{Method string `json:"method"`}
  if e:=json.NewDecoder(r.Body).Decode(&input);e!=nil{t.Error(e)}
  w.Header().Set("Content-Type","application/json")
  switch input.Method{
  case "tools/list":_,_=w.Write([]byte(`{"jsonrpc":"2.0","id":1,"result":{"tools":[{"name":"echo","description":"Echo","inputSchema":{"type":"object"}}]}}`))
  case "tools/call":_,_=w.Write([]byte(`{"jsonrpc":"2.0","id":2,"result":{"content":[{"type":"text","text":"forwarded"}]}}`))
  default:http.Error(w,"unknown",400)
  }
 }));defer downstream.Close()
 h:=makeTestServer(t,[]Backend{{Name:"learning",URL:downstream.URL,Token:"internal-secret"}})
 token:=strings.Repeat("a",40)
 w:=request(h,"POST","/mcp",`{"jsonrpc":"2.0","id":1,"method":"tools/list"}`,token)
 if !strings.Contains(w.Body.String(),"learning__echo"){t.Fatalf("federated list %s",w.Body.String())}
 w=request(h,"POST","/mcp",`{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"learning__echo","arguments":{}}}`,token)
 if !strings.Contains(w.Body.String(),"forwarded"){t.Fatalf("federated call %s",w.Body.String())}
}
