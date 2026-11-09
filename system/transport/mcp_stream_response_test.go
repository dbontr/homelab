package transport

import (
 "bufio"
 "context"
 "encoding/json"
 "io"
 "math"
 "net/http"
 "net/http/httptest"
 "strings"
 "testing"
 "time"
)

func TestMCPEventStreamKeepsLongRunningToolCallsAlive(t *testing.T){
 release:=make(chan struct{})
 server:=httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter,r *http.Request){
  w.Header().Set("Content-Type","text/event-stream")
  w.Header().Set("Cache-Control","no-store")
  streamMCPResult(r.Context(),w,json.RawMessage("42"),5*time.Millisecond,func()map[string]any{
   <-release
   return map[string]any{"isError":false,"content":[]any{map[string]string{
    "type":"text","text":"confirmed",
   }}}
  })
 }))
 defer server.Close()
 client:=&http.Client{Timeout:3*time.Second}
 response,err:=client.Get(server.URL)
 if err!=nil{t.Fatal(err)}
 defer response.Body.Close()
 reader:=bufio.NewReader(response.Body)
 heartbeat,err:=reader.ReadString('\n')
 if err!=nil{t.Fatal(err)}
 if heartbeat!=": keepalive\n"||response.Header.Get("Content-Type")!="text/event-stream"{
  t.Fatalf("no valid SSE keepalive before task completion: %q",heartbeat)
 }
 separator,err:=reader.ReadString('\n')
 if err!=nil||separator!="\n"{t.Fatalf("invalid SSE keepalive framing: %q %v",separator,err)}
 close(release)
 event,err:=reader.ReadString('\n')
 if err!=nil||event!="event: message\n"{
  t.Fatalf("final message was not sent after completion: %q %v",event,err)
 }
 data,err:=reader.ReadString('\n')
 if err!=nil||!strings.HasPrefix(data,"data: "){
  t.Fatalf("final JSON-RPC data missing: %q %v",data,err)
 }
 var envelope map[string]json.RawMessage
 if err:=json.Unmarshal([]byte(strings.TrimSpace(strings.TrimPrefix(data,"data: "))),&envelope);err!=nil{
  t.Fatal(err)
 }
 if string(envelope["id"])!="42"||!strings.Contains(string(envelope["result"]),"confirmed"){
  t.Fatalf("stream did not carry the exact authoritative result: %+v",envelope)
 }
 if _,err:=io.ReadAll(response.Body);err!=nil{t.Fatal(err)}
}

func TestMCPStreamedFastToolReturnsSingleEventWithoutHeartbeats(t *testing.T){
 recorder:=httptest.NewRecorder()
 recorder.Header().Set("Content-Type","text/event-stream")
 streamMCPResult(context.Background(),recorder,json.RawMessage(`"request"`),
  time.Hour,func()map[string]any{return map[string]any{"ok":true}})
 if recorder.Code!=200||strings.Count(recorder.Body.String(),"event: message\n")!=1||
  strings.Contains(recorder.Body.String(),": keepalive"){
  t.Fatalf("fast MCP call emitted a premature or duplicate event: %s",recorder.Body.String())
 }
}

func TestMCPStreamedEncodingFailureIsAnExplicitProtocolError(t *testing.T){
 recorder:=httptest.NewRecorder()
 if err:=writeMCPStreamEvent(recorder,json.RawMessage("3"),
  map[string]any{"unsupported":math.NaN()},nil);err!=nil{
  t.Fatal(err)
 }
 payload:=strings.TrimSuffix(strings.TrimPrefix(recorder.Body.String(),"event: message\ndata: "),"\n\n")
 var envelope map[string]json.RawMessage
 if err:=json.Unmarshal([]byte(payload),&envelope);err!=nil{t.Fatal(err)}
 if string(envelope["id"])!="3"||len(envelope["error"])==0||len(envelope["result"])!=0{
  t.Fatalf("unencodable result was incorrectly reported as success: %+v",envelope)
 }
}

func TestMCPStreamStopsAfterClientCancellation(t *testing.T){
 ctx,cancel:=context.WithCancel(context.Background())
 release:=make(chan struct{})
 recorder:=httptest.NewRecorder()
 complete:=make(chan struct{})
 go func(){
  streamMCPResult(ctx,recorder,json.RawMessage("8"),time.Hour,
   func()map[string]any{<-release;return map[string]any{"ok":true}})
  close(complete)
 }()
 cancel()
 select{
 case <-complete:
 case <-time.After(time.Second):t.Fatal("canceled stream did not exit promptly")
 }
 close(release)
 if recorder.Body.Len()!=0{
  t.Fatalf("canceled client received an authoritative completion event: %s",recorder.Body.String())
 }
}
