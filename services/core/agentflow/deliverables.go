package agentflow

import (
 "context"
 "errors"
 "fmt"
 "path"
 "strings"
)

// deliverOutputs publishes binary and sidecar results through the existing
// project artifact store. It does not publish them to a Git repository.
func (e *Engine) deliverOutputs(ctx context.Context, w Workflow, args map[string]any) (map[string]any,error) {
 checkout,err:=resultObject(w,"checkout")
 if err!=nil{return nil,err}
 repo:=str(checkout["repository"])
 if !repositoryName.MatchString(repo){return nil,errors.New("deliverable repository is invalid")}
 entries,ok:=args["paths"].([]any)
 if !ok||len(entries)==0||len(entries)>16{return nil,errors.New("invalid deliverable list")}
 outputs:=map[string]any{}
 for _,entry:=range entries {
  filename,ok:=entry.(string)
  if !ok||!validSourcePath(filename)||sensitiveSourcePath(filename) {
   return nil,errors.New("invalid deliverable path")
  }
  source:=path.Join(repoRoot(repo),filename)
  // Verify the bytes inside the sandbox and compare them with the artifact
  // stored by the independently authenticated Compute controller.
  digestResult,err:=e.workspaceAction(ctx,w,"command",map[string]any{
   "command":"sha256sum -- "+shellQuote(source),
  })
  if err!=nil{return nil,err}
  code,ok:=digestResult["exit_code"].(float64)
  fields:=strings.Fields(str(digestResult["stdout"]))
  if !ok||code!=0||len(fields)<2||!hexDigest(fields[0]) {
   return nil,fmt.Errorf("deliverable %s has no confirmed digest",filename)
  }
  exported,err:=e.workspaceAction(ctx,w,"export_file",map[string]any{"path":source})
  if err!=nil{return nil,err}
  id:=str(exported["artifact_id"])
  if id=="" {id=str(obj(exported["artifact"])["id"])}
  if !validID(id){return nil,errors.New("deliverable export returned no artifact ID")}
  stored,err:=e.Compute.Call(ctx,"artifact_info",map[string]any{"project_id":w.Project,"artifact_id":id})
  if err!=nil{return nil,err}
  size,sizeOK:=stored["size"].(float64)
  if str(stored["id"])!=id||str(stored["name"])!=path.Base(source)||
   str(stored["sha256"])!=fields[0]||!sizeOK||size<=0 {
   return nil,fmt.Errorf("deliverable %s differs from the verified source",filename)
  }
  outputs[filename]=map[string]any{"artifact_id":id,"sha256":fields[0],"size":size,"name":str(stored["name"])}
 }
 return map[string]any{"outputs":outputs},nil
}
