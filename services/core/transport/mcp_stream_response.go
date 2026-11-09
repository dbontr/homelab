package transport

import (
 "context"
 "encoding/json"
 "fmt"
 "io"
 "net/http"
 "time"
)

// writeMCPStreamEvent writes one authoritative JSON-RPC response. If a result
// cannot be encoded, the client receives a protocol error, never partial JSON.
func writeMCPStreamEvent(w http.ResponseWriter,id json.RawMessage,value any,failure *rpcError)error{
 envelope:=map[string]any{"jsonrpc":"2.0","id":id}
 if failure!=nil{envelope["error"]=failure}else{envelope["result"]=value}
 payload,err:=json.Marshal(envelope)
 if err!=nil{
  payload,_=json.Marshal(map[string]any{
   "jsonrpc":"2.0","id":id,
   "error":rpcError{Code:-32603,Message:"unable to encode tool result"},
  })
 }
 if _,err:=fmt.Fprintf(w,"event: message\ndata: %s\n\n",payload);err!=nil{return err}
 if flush,ok:=w.(http.Flusher);ok{flush.Flush()}
 return nil
}

// streamMCPResult emits only SSE comments until a tool call finishes. Comments
// are transport keepalives, not reports of task progress or completion.
// Only this goroutine writes to the HTTP response.
func streamMCPResult(ctx context.Context,w http.ResponseWriter,id json.RawMessage,
 interval time.Duration,execute func()map[string]any){
 if interval<=0{interval=15*time.Second}
 done:=make(chan map[string]any,1)
 go func(){done<-execute()}()
 ticker:=time.NewTicker(interval)
 defer ticker.Stop()
 opened:=false
 for {
  select{
  case result:=<-done:
   if !opened{w.WriteHeader(http.StatusOK)}
   _=writeMCPStreamEvent(w,id,result,nil)
   return
  case <-ticker.C:
   if err:=ctx.Err();err!=nil{return}
   if !opened{w.WriteHeader(http.StatusOK);opened=true}
   if _,err:=io.WriteString(w,": keepalive\n\n");err!=nil{return}
   if flush,ok:=w.(http.Flusher);ok{flush.Flush()}
  case <-ctx.Done():
   return
  }
 }
}
