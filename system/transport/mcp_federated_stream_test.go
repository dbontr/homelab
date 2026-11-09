package transport

import (
 "context"
 "encoding/json"
 "io"
 "net/http"
 "net/http/httptest"
 "path/filepath"
 "strings"
 "testing"

 "github.com/dbontr/homelab-new/system/store"
)

func TestFederatedSSEAcceptsOneCompleteMatchingResponse(t *testing.T){
 stream:=strings.Join([]string{
  ": keepalive",
  "",
  "event: progress",
  `data: {"jsonrpc":"2.0","id":9,"result":{"untrusted":true}}`,
  "",
  "event: message",
  `data: {"jsonrpc":"2.0",`,
  `data: "id":9,"result":{"ok":true}}`,
  "",
  "",
 },"\n")
 data,err:=streamReply([]byte(stream),json.RawMessage("9"))
 if err!=nil{t.Fatal(err)}
 var got struct{
  JSONRPC string `json:"jsonrpc"`
  ID int `json:"id"`
  Result struct{OK bool `json:"ok"`} `json:"result"`
 }
 if err:=json.Unmarshal(data,&got);err!=nil||got.JSONRPC!="2.0"||got.ID!=9||!got.Result.OK{
  t.Fatalf("streamed tool response changed identity or content: %+v %v",got,err)
 }
}

func TestFederatedSSERejectsAmbiguousResponsesAndIncompleteEvents(t *testing.T){
 response:=`{"jsonrpc":"2.0","id":9,"result":{"ok":true}}`
 failure:=`{"jsonrpc":"2.0","id":9,"error":{"code":-32000,"message":"unavailable"}}`
 errors:=[]struct{name,stream string}{
  {"duplicate success","data: "+response+"\n\n"+"data: "+response+"\n\n"},
  {"error followed by success","data: "+failure+"\n\n"+"data: "+response+"\n\n"},
  {"success followed by error","data: "+response+"\n\n"+"data: "+failure+"\n\n"},
  {"unflushed final event","event: message\ndata: "+response+"\n"},
  {"truncated final event","event: message\ndata: "+response},
  {"missing matching ID","event: message\ndata: "+strings.Replace(response,`"id":9`,`"id":10`,1)+"\n\n"},
  {"both result and error","data: "+`{"jsonrpc":"2.0","id":9,"result":{},"error":{"code":-32000,"message":"failure"}}`+"\n\n"},
  {"neither result nor error","data: "+`{"jsonrpc":"2.0","id":9}`+"\n\n"},
 }
 for _,tc:=range errors{
  if got,err:=streamReply([]byte(tc.stream),json.RawMessage("9"));err==nil{
   t.Fatalf("%s accepted as confirmed tool response: %s",tc.name,got)
  }
 }
}

func TestFederationRejectsConflictingRemoteConfirmations(t *testing.T){
 backend:=httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter,r *http.Request){
  if r.Header.Get("MCP-Protocol-Version")!="2025-06-18"{
   t.Errorf("federation did not negotiate protocol version")
  }
  w.Header().Set("Content-Type","text/event-stream")
  _,_=io.WriteString(w,"event: message\ndata: "+`{"jsonrpc":"2.0","id":7,"result":{"completed":false}}`+"\n\n")
  _,_=io.WriteString(w,"event: message\ndata: "+`{"jsonrpc":"2.0","id":7,"result":{"completed":true}}`+"\n\n")
 }))
 defer backend.Close()
 db,err:=store.Open(filepath.Join(t.TempDir(),"personal.json"),"UTC")
 if err!=nil{t.Fatal(err)}
 gateway,err:=New(db,strings.Repeat("f",40),nil)
 if err!=nil{t.Fatal(err)}
 var result map[string]any
 err=gateway.forward(context.Background(),Backend{Name:"test",URL:backend.URL},
  rpcRequest{JSONRPC:"2.0",ID:json.RawMessage("7"),Method:"tools/call"},&result)
 if err==nil||result!=nil{
  t.Fatalf("conflicting SSE confirmations were accepted: %+v %v",result,err)
 }
}
