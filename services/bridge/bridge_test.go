package bridge

import (
 "io"
 "os"
 "path/filepath"
 "net/http"
 "net/http/httptest"
 "strings"
 "testing"
)

const clientSecret = "client-facing-secret-of-sufficient-length"
const internalSecret = "service-only-secret-of-sufficient-length"

func upstream(t *testing.T)(string,*int,func()){
 t.Helper()
 calls:=0
 server:=httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter,r *http.Request){
  calls++
  if got:=r.Header.Get("Authorization");got!="Bearer "+internalSecret{
   t.Errorf("internal credential was not isolated: %q",got)
  }
  if r.Header.Get("Cookie")!=""||r.Header.Get("X-Forwarded-Host")!=""||
   r.Header.Get("Forwarded")!=""{
   t.Error("caller identity and forwarding headers leaked to services")
  }
  w.Header().Set("Content-Type","application/json")
  _,_=io.WriteString(w,`{"upstream":true}`)
 }))
 return server.URL,&calls,server.Close
}

func request(t *testing.T,h http.Handler,method,path,token string)*httptest.ResponseRecorder{
 t.Helper()
 req:=httptest.NewRequest(method,path,nil)
 if token!=""{req.Header.Set("Authorization","Bearer "+token)}
 req.Header.Set("Cookie","browser-session=untrusted")
 req.Header.Set("X-Forwarded-Host","untrusted.example")
 req.Header.Set("Forwarded","for=203.0.113.1")
 w:=httptest.NewRecorder()
 h.ServeHTTP(w,req)
 return w
}

func TestMCPIsOneAuthenticatedOptionalInterface(t *testing.T){
 url,calls,done:=upstream(t)
 defer done()
 handler,err:=New(Config{Surface:MCP,BackendURL:url,
  ClientToken:clientSecret,ServiceToken:internalSecret})
 if err!=nil{t.Fatal(err)}
 if r:=request(t,handler,"POST","/mcp","");r.Code!=401{
  t.Fatal("unauthenticated MCP reached upstream",r.Code)
 }
 if r:=request(t,handler,"POST","/mcp","wrong");r.Code!=401{
  t.Fatal("wrong interface token accepted")
 }
 for _,path:=range []string{"/v1/tasks","/ui/session","/mcp/extra"}{
  if r:=request(t,handler,"POST",path,clientSecret);r.Code!=404{
   t.Fatalf("MCP exposed unrelated path %s: %d",path,r.Code)
  }
 }
 if r:=request(t,handler,"GET","/mcp",clientSecret);r.Code!=404{
  t.Fatal("MCP accepted unsupported transport method")
 }
 if *calls!=0{t.Fatal("unauthorized MCP request reached services")}
 r:=request(t,handler,"POST","/mcp",clientSecret)
 if r.Code!=200||!strings.Contains(r.Body.String(),`"upstream":true`)||*calls!=1{
  t.Fatal("MCP did not forward to unified service dispatcher",r.Code)
 }
}

func TestAPIIsReadOnlyUnlessAnExactWriteResourceIsApproved(t *testing.T){
 url,calls,done:=upstream(t)
 defer done()
 handler,err:=New(Config{Surface:API,BackendURL:url,
  ClientToken:clientSecret,ServiceToken:internalSecret,
  WritePaths:[]string{"/v1/tasks"}})
 if err!=nil{t.Fatal(err)}
 for _,path:=range []string{"/mcp","/ui/session","/v1/tools","/v1/tools/system/call"}{
  if r:=request(t,handler,"GET",path,clientSecret);r.Code!=404{
   t.Fatalf("public API exposed disallowed resource %s: %d",path,r.Code)
  }
 }
 if r:=request(t,handler,"POST","/v1/tasks-extra",clientSecret);r.Code!=403{
  t.Fatal("prefix-matched an unapproved resource",r.Code)
 }
 if r:=request(t,handler,"POST","/v1/health/records",clientSecret);r.Code!=403{
  t.Fatal("mutation outside the write allowlist was admitted")
 }
 if *calls!=0{t.Fatal("blocked public API operation reached services")}
 for _,tc:=range []struct{method,path string}{
  {"GET","/v1/health/records"},
  {"POST","/v1/tasks"},
  {"PUT","/v1/tasks/item"},
  {"DELETE","/v1/tasks/item"},
 }{
  if r:=request(t,handler,tc.method,tc.path,clientSecret);r.Code!=200{
   t.Fatalf("authorized API method denied %s %s: %d",tc.method,tc.path,r.Code)
  }
 }
 if *calls!=4{t.Fatal("approved API operations were not forwarded")}
}

func TestInterfacesRejectSpoofedOriginsAndAmbiguousPaths(t *testing.T){
 url,calls,done:=upstream(t)
 defer done()
 h,err:=New(Config{Surface:MCP,BackendURL:url,
  ClientToken:clientSecret,ServiceToken:internalSecret,
  Origins:[]string{"https://assistant.example"}})
 if err!=nil{t.Fatal(err)}
 r:=httptest.NewRequest("POST","/mcp",nil)
 r.Header.Set("Origin","https://attacker.example")
 r.Header.Set("Authorization","Bearer "+clientSecret)
 out:=httptest.NewRecorder()
 h.ServeHTTP(out,r)
 if out.Code!=403||*calls!=0{t.Fatal("unapproved Origin reached internal service")}
 r.Header.Set("Origin","https://assistant.example")
 out=httptest.NewRecorder()
 h.ServeHTTP(out,r)
 if out.Code!=200||*calls!=1{t.Fatal("approved Origin was not admitted")}
 h2,err:=New(Config{Surface:API,BackendURL:url,
  ClientToken:clientSecret,ServiceToken:internalSecret})
 if err!=nil{t.Fatal(err)}
 for _,path:=range []string{"/v1/tasks%2Fprivate","/v1/tasks%5cprivate","/v1//tasks"}{
  req:=httptest.NewRequest("GET",path,nil)
  req.Header.Set("Authorization","Bearer "+clientSecret)
  out:=httptest.NewRecorder()
  h2.ServeHTTP(out,req)
  if out.Code==200{t.Fatalf("ambiguous API path admitted: %s",path)}
 }
 if *calls!=1{t.Fatal("invalid path reached private service")}
}

func TestInterfacesRequireIsolatedCredentialsAndPrivateBackend(t *testing.T){
 for _,c:=range []Config{
  {Surface:MCP,BackendURL:"https://example.test",ClientToken:clientSecret,ServiceToken:internalSecret},
  {Surface:MCP,BackendURL:"http://192.0.2.10:8080",ClientToken:clientSecret,ServiceToken:internalSecret},
  {Surface:MCP,BackendURL:"http://127.0.0.1:8080/another",ClientToken:clientSecret,ServiceToken:internalSecret},
  {Surface:MCP,BackendURL:"http://127.0.0.1:8080",ClientToken:clientSecret,ServiceToken:clientSecret},
  {Surface:API,BackendURL:"http://127.0.0.1:8080",ClientToken:clientSecret,ServiceToken:internalSecret,WritePaths:[]string{"/v1/tools"}},
  {Surface:API,BackendURL:"http://127.0.0.1:8080",ClientToken:clientSecret,ServiceToken:internalSecret,WritePaths:[]string{"/v1/tasks/../other"}},
 }{
  if _,err:=New(c);err==nil{t.Fatalf("untrusted interface configuration admitted: %+v",c)}
 }
}

func TestPrivateSystemdCredentialAllowsOnlyProtectedGroupRead(t *testing.T) {
 dir:=t.TempDir()
 credential:=filepath.Join(dir,"service-token")
 if err:=os.WriteFile(credential,[]byte(internalSecret),0440);err!=nil{t.Fatal(err)}
 t.Setenv("CREDENTIALS_DIRECTORY","")
 if _,err:=protectedToken(credential);err==nil{t.Fatal("group-readable file outside systemd credentials admitted")}
 t.Setenv("CREDENTIALS_DIRECTORY",dir)
 if _,err:=protectedToken(credential);err!=nil{t.Fatalf("systemd credential rejected: %v",err)}
 if err:=os.Chmod(credential,0460);err!=nil{t.Fatal(err)}
 if _,err:=protectedToken(credential);err==nil{t.Fatal("group-writable credential admitted")}
 if err:=os.Chmod(credential,0444);err!=nil{t.Fatal(err)}
 if _,err:=protectedToken(credential);err==nil{t.Fatal("world-readable credential admitted")}
}
