package transport

import (
 "net/http"
 "net/http/httptest"
 "strings"
 "testing"
)

func mcpHeaderRequest(h http.Handler,token string,headers http.Header)*httptest.ResponseRecorder{
 request:=httptest.NewRequest(http.MethodPost,"/mcp",strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"ping"}`))
 for name,values:=range headers{
  for _,value:=range values{request.Header.Add(name,value)}
 }
 request.Header.Set("Authorization","Bearer "+token)
 output:=httptest.NewRecorder()
 h.ServeHTTP(output,request)
 return output
}

func TestMCPHTTPProtocolVersionValidation(t *testing.T){
 h:=makeTestServer(t,nil)
 token:=strings.Repeat("a",40)
 for _,version:=range []string{"2025-06-18","2025-03-26"}{
  response:=mcpHeaderRequest(h,token,http.Header{"Mcp-Protocol-Version":[]string{version}})
  if response.Code!=http.StatusOK||!strings.Contains(response.Body.String(),`"result"`){
   t.Fatalf("supported MCP version %q denied: %d %s",version,response.Code,response.Body.String())
  }
 }
 unversioned:=mcpHeaderRequest(h,token,http.Header{})
 if unversioned.Code!=http.StatusOK{
  t.Fatalf("legacy stateless MCP request refused: %d",unversioned.Code)
 }
 invalid:=[]http.Header{
  {"Mcp-Protocol-Version":[]string{"2024-11-05"}},
  {"Mcp-Protocol-Version":[]string{"2025-06-18, 2025-03-26"}},
  {"Mcp-Protocol-Version":[]string{"2025-06-18","2025-06-18"}},
  {"Mcp-Protocol-Version":[]string{""}},
 }
 for _,headers:=range invalid{
  response:=mcpHeaderRequest(h,token,headers)
  if response.Code!=http.StatusBadRequest{
   t.Fatalf("invalid MCP version accepted: %v, status=%d",headers,response.Code)
  }
  if !strings.Contains(response.Body.String(),"protocol version"){
   t.Fatalf("invalid MCP version lacks actionable error: %s",response.Body.String())
  }
 }
}

func TestMCPTransportRejectsNonJSONRequestsBeforeDispatch(t *testing.T){
 h:=makeTestServer(t,nil)
 token:=strings.Repeat("a",40)
 valid:=[]http.Header{
  {},
  {"Content-Type":[]string{"application/json"}},
  {"Content-Type":[]string{"application/json; charset=UTF-8"}},
 }
 for _,headers:=range valid{
  response:=mcpHeaderRequest(h,token,headers)
  if response.Code!=http.StatusOK{
   t.Fatalf("accepted JSON transport refused: %v: %d",headers,response.Code)
  }
 }
 invalid:=[]http.Header{
  {"Content-Type":[]string{"text/plain"}},
  {"Content-Type":[]string{"application/json; charset=latin1"}},
  {"Content-Type":[]string{"application/json; unknown=x"}},
  {"Content-Type":[]string{"application/json","application/json"}},
  {"Content-Type":[]string{"text/event-stream"}},
 }
 for _,headers:=range invalid{
  response:=mcpHeaderRequest(h,token,headers)
  if response.Code!=http.StatusUnsupportedMediaType||
   response.Header().Get("Content-Type")!="application/json"{
   t.Fatalf("non-JSON request admitted or misreported: %v, status=%d",headers,response.Code)
  }
 }
}

func TestMCPVersionValidationPreventsSideEffectsAndRetainsStreamableHTTP(t *testing.T){
 h:=makeTestServer(t,nil)
 token:=strings.Repeat("a",40)
 request:=httptest.NewRequest(http.MethodPost,"/mcp",strings.NewReader(
  `{"jsonrpc":"2.0","id":7,"method":"tools/call","params":{"name":"task_add","arguments":{"title":"Should not be saved"}}}`))
 request.Header.Set("Authorization","Bearer "+token)
 request.Header.Set("Accept","application/json, text/event-stream")
 request.Header.Set("MCP-Protocol-Version","2030-01-01")
 w:=httptest.NewRecorder()
 h.ServeHTTP(w,request)
 if w.Code!=http.StatusBadRequest||strings.Contains(w.Body.String(),"Should not be saved"){
  t.Fatalf("unsupported MCP version was dispatched: %d %s",w.Code,w.Body.String())
 }
 discovery:=mcpHeaderRequest(h,token,http.Header{
  "Mcp-Protocol-Version":[]string{"2025-06-18"},
  "Accept":[]string{"application/json, text/event-stream"},
  "Content-Type":[]string{"application/json"},
 })
 if discovery.Code!=http.StatusOK||discovery.Header().Get("Content-Type")!="text/event-stream"||
  !strings.Contains(discovery.Body.String(),"event: message"){
  t.Fatalf("validated Streamable HTTP response was not framed as SSE: %d %s",
   discovery.Code,discovery.Body.String())
 }
}
