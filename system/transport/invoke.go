package transport

import (
 "context"
 "encoding/json"
 "net/http"
 "strings"

 "github.com/dbontr/homelab-new/system/canonicaljson"
)

func (s *Server) invoke(ctx context.Context,name string,args json.RawMessage,id json.RawMessage) map[string]any {
 normalized,_,err:=canonicaljson.Object(args)
 if err!=nil{return toolError(err.Error())}
 args=normalized
 if name=="workspace__resource_plan" {
   if err:=s.deviceCatalog.ValidatePlan(args);err!=nil{return toolError(err.Error())}
 }
 if name=="workspace__workspace_create" || name=="workspace__task_compute_submit" {
   if err:=s.deviceCatalog.ValidateAllocation(args);err!=nil{return toolError(err.Error())}
 }
 for _,b:=range s.backend {
   prefix:=b.Name+"__"
   if strings.HasPrefix(name,prefix) {
     toolName:=strings.TrimPrefix(name,prefix)
     if !s.backendHasTool(ctx,b,toolName){return toolError("backend tool unavailable or not advertised")}
     if output,allowed:=s.checkPrivilege(name,args);!allowed{return output}
     params:=map[string]any{"name":toolName,"arguments":json.RawMessage(args)}
     raw,_:=json.Marshal(params)
     var remote any
     err:=s.forward(ctx,b,rpcRequest{JSONRPC:"2.0",ID:id,Method:"tools/call",Params:raw},&remote)
     if err!=nil{return toolError(err.Error())}
     output,ok:=remote.(map[string]any)
     if !ok{return toolError("invalid backend tool result")}
     return output
   }
 }
 if output,allowed:=s.checkPrivilege(name,args);!allowed{return output}
 v,err:=s.localTool(ctx,name,args)
 if err!=nil{return toolError(err.Error())}
 return result(v)
}

// Native requests use precisely the same tool dispatch as MCP.
func (s *Server) nativeToolCall(w http.ResponseWriter,r *http.Request) {
 var input struct {Arguments json.RawMessage `json:"arguments"`}
 if err:=decode(r,&input);err!=nil {writeError(w,http.StatusBadRequest,err.Error());return}
 name:=r.PathValue("name")
 if name=="" {writeError(w,http.StatusBadRequest,"tool name required");return}
 writeJSON(w,http.StatusOK,s.invoke(r.Context(),name,input.Arguments,json.RawMessage("1")))
}
