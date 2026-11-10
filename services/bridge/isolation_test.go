package bridge

import (
 "io"
 "net/http"
 "net/http/httptest"
 "strings"
 "sync/atomic"
 "testing"
 "time"
)

// A disabled optional interface cannot terminate the domain service or its
// other interfaces. Each interface presents only its own service identity.
func TestInterfaceLifecycleIsIndependentOfDomainService(t *testing.T) {
 const apiClient = "api-client-credential-long-and-isolated"
 const apiService = "api-service-credential-long-and-isolated"
 const mcpClient = "mcp-client-credential-long-and-isolated"
 const mcpService = "mcp-service-credential-long-and-isolated"
 var nativeCalls, mcpCalls atomic.Int32
 core:=httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter,r *http.Request){
  switch r.URL.Path {
  case "/v1/tasks":
   if r.Header.Get("Authorization")!="Bearer "+apiService{
    t.Error("API did not use its independent service identity")
    w.WriteHeader(http.StatusUnauthorized);return
   }
   nativeCalls.Add(1)
  case "/mcp":
   if r.Header.Get("Authorization")!="Bearer "+mcpService{
    t.Error("MCP did not use its independent service identity")
    w.WriteHeader(http.StatusUnauthorized);return
   }
   mcpCalls.Add(1)
  case "/healthz":
  default:
   w.WriteHeader(http.StatusNotFound);return
  }
  w.Header().Set("Content-Type","application/json")
  _,_=io.WriteString(w,`{"status":"ready"}`)
 }))
 defer core.Close()
 apiHandler,err:=New(Config{Surface:API,BackendURL:core.URL,ClientToken:apiClient,ServiceToken:apiService})
 if err!=nil{t.Fatal(err)}
 mcpHandler,err:=New(Config{Surface:MCP,BackendURL:core.URL,ClientToken:mcpClient,ServiceToken:mcpService})
 if err!=nil{t.Fatal(err)}
 api:=httptest.NewServer(apiHandler)
 mcp:=httptest.NewServer(mcpHandler)
 defer api.Close()
 defer mcp.Close()
 client:=&http.Client{Timeout:3*time.Second}
 call:=func(method,url,token string)int{
  req,err:=http.NewRequest(method,url,strings.NewReader("{}"))
  if err!=nil{t.Fatal(err)}
  req.Header.Set("Authorization","Bearer "+token)
  response,err:=client.Do(req)
  if err!=nil{t.Fatal(err)}
  defer response.Body.Close()
  _,_=io.Copy(io.Discard,response.Body)
  return response.StatusCode
 }
 if got:=call("GET",api.URL+"/v1/tasks",apiClient);got!=200{t.Fatalf("API failed before shutdown: %d",got)}
 if got:=call("POST",mcp.URL+"/mcp",mcpClient);got!=200{t.Fatalf("MCP failed before shutdown: %d",got)}
 api.Close()
 if got:=call("POST",mcp.URL+"/mcp",mcpClient);got!=200{t.Fatalf("stopping API disabled MCP: %d",got)}
 if got:=call("GET",core.URL+"/healthz","");got!=200{t.Fatalf("stopping API disabled domain service: %d",got)}
 mcp.Close()
 if got:=call("GET",core.URL+"/healthz","");got!=200{t.Fatalf("stopping MCP disabled domain service: %d",got)}
 if got,want:=nativeCalls.Load(),int32(1);got!=want{t.Fatalf("native calls=%d, want %d",got,want)}
 if got,want:=mcpCalls.Load(),int32(2);got!=want{t.Fatalf("MCP calls=%d, want %d",got,want)}
}

func TestUnavailableServiceDoesNotBecomeInterfaceOwnedState(t *testing.T){
 core:=httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter,_ *http.Request){
  w.WriteHeader(http.StatusOK)
 }))
 apiHandler,err:=New(Config{Surface:API,BackendURL:core.URL,ClientToken:clientSecret,ServiceToken:internalSecret})
 if err!=nil{t.Fatal(err)}
 mcpHandler,err:=New(Config{Surface:MCP,BackendURL:core.URL,ClientToken:clientSecret,ServiceToken:internalSecret})
 if err!=nil{t.Fatal(err)}
 core.Close()
 for _,tc:=range []struct{handler http.Handler;method,path string}{
  {apiHandler,"GET","/v1/tasks"},
  {mcpHandler,"POST","/mcp"},
 }{
  got:=request(t,tc.handler,tc.method,tc.path,clientSecret)
  if got.Code!=http.StatusServiceUnavailable{t.Errorf("%s: upstream outage returned %d, want 503",tc.path,got.Code)}
 }
}
