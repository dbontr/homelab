package transport

import (
 "encoding/json"
 "net/http"
 "net/http/httptest"
 "strings"
 "testing"
)

func requestMCPWithAccept(h http.Handler, payload, accept, token string)*httptest.ResponseRecorder{
 req:=httptest.NewRequest(http.MethodPost,"/mcp",strings.NewReader(payload))
 req.Header.Set("Accept",accept)
 if token!=""{req.Header.Set("Authorization","Bearer "+token)}
 w:=httptest.NewRecorder()
 h.ServeHTTP(w,req)
 return w
}

func decodeStreamedMCPResponse(t *testing.T,w *httptest.ResponseRecorder)map[string]json.RawMessage{
 t.Helper()
 if w.Code!=http.StatusOK||w.Header().Get("Content-Type")!="text/event-stream"{
  t.Fatalf("streamable HTTP response: %d %+v %s",w.Code,w.Header(),w.Body.String())
 }
 body:=w.Body.String()
 if !strings.HasPrefix(body,"event: message\ndata: ")||!strings.HasSuffix(body,"\n\n")||
  strings.Count(body,"event: message\n")!=1{
  t.Fatalf("JSON-RPC response was not framed as exactly one SSE message: %q",body)
 }
 data:=strings.TrimSuffix(strings.TrimPrefix(body,"event: message\ndata: "),"\n\n")
 var value map[string]json.RawMessage
 if err:=json.Unmarshal([]byte(data),&value);err!=nil{t.Fatalf("invalid streamed response: %v",err)}
 if string(value["jsonrpc"])!=`"2.0"` {t.Fatalf("wrong JSON-RPC protocol: %s",data)}
 return value
}

func TestMCPStreamableHTTPResponsePreservesJSONRPCIdentity(t *testing.T){
 h:=makeTestServer(t,nil)
 token:=strings.Repeat("s",40)
 if response:=requestMCPWithAccept(h,`{"jsonrpc":"2.0","id":1,"method":"ping"}`,
  "application/json, text/event-stream", "");response.Code!=http.StatusUnauthorized{
  t.Fatalf("unprotected MCP stream accepted: %d",response.Code)
 }
 tests:=[]struct{
  request string
  id string
  expected string
 }{
  {`{"jsonrpc":"2.0","id":"init","method":"initialize","params":{}}`,`"init"`,`"protocolVersion"`},
  {`{"jsonrpc":"2.0","id":2,"method":"tools/list"}`,`2`,`"task_add"`},
  {`{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"task_add","arguments":{"title":"Verify an experiment"}}}`,`3`,`"Verify an experiment"`},
  {`{"jsonrpc":"2.0","id":4,"method":"unknown"}`,`4`,`"error"`},
 }
 for _,tc:=range tests{
  response:=requestMCPWithAccept(h,tc.request,"application/json, text/event-stream",token)
  body:=decodeStreamedMCPResponse(t,response)
  if string(body["id"])!=tc.id||!strings.Contains(response.Body.String(),tc.expected){
   t.Fatalf("stream lost request identity or result: %+v",body)
  }
  if response.Header().Get("Cache-Control")!="no-store"{
   t.Fatal("MCP SSE response was cacheable")
  }
 }
}

func TestMCPEventStreamNegotiationAndNotificationSemantics(t *testing.T){
 h:=makeTestServer(t,nil)
 token:=strings.Repeat("k",40)
 ping:=`{"jsonrpc":"2.0","id":10,"method":"ping"}`
 for _,accept:=range []string{
  "", "application/json","application/json, text/event-stream;q=0",
  "text/event-stream;q=0.0",
 }{
  response:=requestMCPWithAccept(h,ping,accept,token)
  if response.Code!=200||response.Header().Get("Content-Type")!="application/json"{
   t.Fatalf("unsupported SSE negotiation changed JSON response: %s -> %d %s",
    accept,response.Code,response.Header().Get("Content-Type"))
  }
  var result map[string]json.RawMessage
  if err:=json.Unmarshal(response.Body.Bytes(),&result);err!=nil||
   string(result["id"])!="10"{t.Fatalf("JSON backward compatibility failed: %v",err)}
 }
 for _,accept:=range []string{"text/event-stream","application/json, text/event-stream;q=0.5"}{
  response:=requestMCPWithAccept(h,ping,accept,token)
  decodeStreamedMCPResponse(t,response)
 }
 notification:=requestMCPWithAccept(h,`{"jsonrpc":"2.0","method":"notifications/initialized"}`,
  "application/json, text/event-stream",token)
 if notification.Code!=http.StatusAccepted||notification.Body.Len()!=0{
  t.Fatalf("notification was answered like a request: %d %q",
   notification.Code,notification.Body.String())
 }
}

func TestMCPProtocolErrorsRemainFramedInEventStream(t *testing.T){
 h:=makeTestServer(t,nil)
 token:=strings.Repeat("e",40)
 for _,payload:=range []string{`{invalid`,`{"jsonrpc":"1.0","method":"ping","id":2}`} {
  response:=requestMCPWithAccept(h,payload,"text/event-stream",token)
  message:=decodeStreamedMCPResponse(t,response)
  if len(message["error"])==0{
   t.Fatalf("malformed JSON-RPC request had no protocol error: %+v",message)
  }
 }
}
