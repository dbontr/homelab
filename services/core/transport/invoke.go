package transport

import (
 "context"
 "encoding/json"
 "net/http"
 "strings"

 "github.com/dbontr/homelab-new/system/canonicaljson"
 "github.com/dbontr/homelab-new/system/security"
 "github.com/dbontr/homelab-new/system/privileged"
 "github.com/dbontr/homelab-new/system/approval"
)

func (s *Server) invoke(ctx context.Context,name string,args json.RawMessage,id json.RawMessage) map[string]any {
 if strings.HasPrefix(name,"compute__"){
  name="workspace__"+strings.TrimPrefix(name,"compute__")
 }
 if name=="workspace__github_merge_pull_request"||name=="workspace__github_mark_pull_request_ready"||
  name=="workspace__github_repository_api"||name=="workspace__github_cleanup_merged_branch"{
  return toolError("repository promotion requires independent owner review")
 }
 normalized,_,err:=canonicaljson.Object(args)
 if err!=nil{return toolError(err.Error())}
 args=normalized
 if strings.HasPrefix(name,"infrastructure__fleet_"){
  if s.fleetClient==nil{return toolError("independent fleet coordinator not configured")}
  raw,err:=s.fleetClient.Call(ctx,name,args)
  if err!=nil{return toolError("independent fleet coordinator unavailable")}
  var value map[string]any
  if err:=json.Unmarshal(raw,&value);err!=nil{return toolError("invalid fleet reply")}
  return result(value)
 }
 if name=="infrastructure__workspace_create" || name=="infrastructure__workspace_stop" || name=="infrastructure__workspace_build_environment" || name=="infrastructure__workspace_register_software" || name=="infrastructure__workspace_revoke_worker" || name=="infrastructure__service_start" || name=="infrastructure__service_stop" || name=="infrastructure__service_reload" || name=="infrastructure__service_restart" || name=="infrastructure__release_activate" || name=="infrastructure__release_revert" ||
  name=="cluster__deployment_restart" || name=="cluster__deployment_scale" {
  if s.executorClient==nil{return toolError("independent privileged executor not configured")}
  decision,err:=s.executorClient.Execute(ctx,name,privilegeTarget(name,args),args)
  if err!=nil{return toolError("independent privileged executor unavailable")}
  if !decision.Allowed{
   return result(map[string]any{"authorization_required":true,"reason":decision.Reason,
     "confirmation_id":decision.ConfirmationID,"confirmation_expires_at":decision.ConfirmationExpiresAt,
     "instructions":"Sign in through the protected owner page and confirm destructive changes there. Retry identical arguments."})
  }
  if decision.State==privileged.ExecutionAccepted{
   return result(map[string]any{"accepted":true,"state":decision.State,"audit_id":decision.AuditID})
  }
  if decision.State==privileged.ExecutionCompleted{
   return result(map[string]any{"executed":true,"completed":true,"state":decision.State,"audit_id":decision.AuditID})
  }
  return toolError("independent privileged executor returned an invalid execution outcome")
 }
 if name=="workspace__resource_plan" ||
  name=="workspace__workspace_create" || name=="workspace__task_compute_submit" {
  if s.deviceCatalog==nil{return toolError("device policy catalog is not configured")}
 }
 if name=="workspace__resource_plan" {
   if err:=s.deviceCatalog.ValidatePlan(args);err!=nil{return toolError(err.Error())}
 }
 if name=="workspace__workspace_create" || name=="workspace__task_compute_submit" {
   if err:=s.deviceCatalog.ValidateAllocation(args);err!=nil{return toolError(err.Error())}
 }
 for _,b:=range s.backend {
   prefix:=b.Name+"__"
   if strings.HasPrefix(name,prefix) {
     if approval.ScopeFor(name)!="" {
      return toolError("privileged backend operations require executor-side authorization and are not available through direct gateway forwarding")
     }
     toolName:=strings.TrimPrefix(name,prefix)
     if !s.backendHasTool(ctx,b,toolName){return toolError("backend tool unavailable or not advertised")}
     if output,allowed:=s.checkPrivilegeContext(ctx,name,args);!allowed{return output}
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
 if output,allowed:=s.checkPrivilegeContext(ctx,name,args);!allowed{return output}
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
 writeJSON(w,http.StatusOK,s.invoke(security.WithAccessAssertion(r.Context(),r.Header.Get("Cf-Access-Jwt-Assertion")),name,input.Arguments,json.RawMessage("1")))
}
