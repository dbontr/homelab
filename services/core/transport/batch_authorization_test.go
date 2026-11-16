package transport

import (
 "context"
 "encoding/json"
 "net"
 "net/http"
 "path/filepath"
 "strings"
 "testing"

 "github.com/dbontr/homelab-new/system/privileged"
 "github.com/dbontr/homelab-new/system/security"
)

func TestBatchSubmissionUsesIndependentExecutorAndBoundRequestIdentity(t *testing.T) {
 socket:=filepath.Join(t.TempDir(),"executor.sock")
 listener,err:=net.Listen("unix",socket)
 if err!=nil{t.Fatal(err)}
 requests:=make(chan map[string]any,1)
 server:=&http.Server{Handler:http.HandlerFunc(func(w http.ResponseWriter,r *http.Request){
  if r.URL.Path!="/v1/execute"||r.Header.Get("Cf-Access-Jwt-Assertion")!="signed-owner"{
   http.Error(w,"unexpected executor call",http.StatusForbidden);return
  }
  var input map[string]any
  if err:=json.NewDecoder(r.Body).Decode(&input);err!=nil{
   http.Error(w,"invalid executor request",http.StatusBadRequest);return
  }
  requests<-input
  w.Header().Set("Content-Type","application/json")
  _,_=w.Write([]byte(`{"allowed":false,"reason":"owner session required"}`))
 })}
 defer server.Close()
 go func(){_ =server.Serve(listener)}()
 client,err:=privileged.NewClient(socket)
 if err!=nil{t.Fatal(err)}
 gateway:=&Server{executorClient:client}
 key:=strings.Repeat("a",32)
 fields:=map[string]any{
  "host":"saturn","project_id":"research","command":"echo ok",
  "image":"image@sha256:abc","cpu":0.5,"memory_mb":256,"request_id":key,
 }
 encoded,err:=json.Marshal(fields)
 if err!=nil{t.Fatal(err)}
 ctx:=security.WithAccessAssertion(context.Background(),"signed-owner")
 result:=gateway.invoke(ctx,"compute__submit_batch_job",encoded,json.RawMessage("1"))
 receipt,ok:=result["structuredContent"].(map[string]any)
 if !ok||receipt["authorization_required"]!=true||receipt["request_id"]!=key{
  t.Fatalf("independent authorization request lost: %v",result)
 }
 select{
 case call:=<-requests:
  if call["request_id"]!=key||call["operation"]!="compute__submit_batch_job"||
   call["target"]!="saturn"{t.Fatalf("wrong executor request: %v",call)}
  scope,ok:=call["arguments"].(map[string]any)
  if !ok||scope["job_request_id"]!=key||scope["request_id"]!=nil{
   t.Fatalf("batch idempotency key not confined to canonical arguments: %v",scope)
  }
 default:t.Fatal("batch submission did not reach independent executor")
 }
 fields["job_request_id"]="forged"
 forbidden,_:=json.Marshal(fields)
 denied:=gateway.invoke(ctx,"compute__submit_batch_job",forbidden,json.RawMessage("2"))
 if denied["isError"]!=true{t.Fatalf("caller-supplied job identity accepted: %v",denied)}
 if !privilegedExecutorTool("compute__cancel_batch_job"){
  t.Fatal("batch cancellation bypasses independent executor")
 }
 empty:=&Server{}
 denied=empty.invoke(ctx,"compute__submit_batch_job",encoded,json.RawMessage("3"))
 if denied["isError"]!=true{t.Fatal("batch submission allowed without executor")}
}
