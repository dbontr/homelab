package agentflow

import (
 "context"
 "errors"
 "fmt"
 "path"
 "strings"
)

// importInputs binds existing project artifacts to exact workspace paths.
// The worker checks that the target is absent; existing repository source
// files and raw originals are not overwritten.
func(e *Engine) importInputs(ctx context.Context,w Workflow,args map[string]any)(map[string]any,error){
 checkout,err:=resultObject(w,"checkout")
 if err!=nil{return nil,err}
 repo:=str(checkout["repository"])
 if !repositoryName.MatchString(repo){return nil,errors.New("invalid import repository")}
 ws,err:=workspaceID(w)
 if err!=nil{return nil,err}
 items,ok:=args["artifacts"].([]any)
 if !ok||len(items)==0||len(items)>8{return nil,errors.New("invalid input artifact list")}
 records:=map[string]any{}
 for _,item:=range items {
  record:=obj(item)
  id,filename:=str(record["artifact_id"]),str(record["path"])
  if !validID(id)||!validSourcePath(filename)||sensitiveSourcePath(filename) {
   return nil,errors.New("invalid import artifact or path")
  }
  meta,err:=e.Compute.Call(ctx,"artifact_info",map[string]any{"project_id":w.Project,"artifact_id":id})
  if err!=nil{return nil,err}
  expected:=str(meta["sha256"])
  size,ok:=meta["size"].(float64)
  if str(meta["id"])!=id||!hexDigest(expected)||!ok||size<=0 {
   return nil,errors.New("input artifact lacks a verified project identity")
  }
  target:=path.Join(repoRoot(repo),filename)
  operation,err:=e.Compute.Call(ctx,"workspace_import_artifact",map[string]any{
   "project_id":w.Project,"workspace_id":ws,"artifact_id":id,"path":target,
  })
  if err!=nil{return nil,err}
  if _,err=e.awaitOperation(ctx,w.Project,ws,str(operation["operation_id"]));err!=nil{return nil,err}
  readback,err:=e.workspaceAction(ctx,w,"command",map[string]any{"command":"sha256sum -- "+shellQuote(target)})
  if err!=nil{return nil,err}
  code,ok:=readback["exit_code"].(float64)
  fields:=strings.Fields(str(readback["stdout"]))
  if !ok||code!=0||len(fields)<2||fields[0]!=expected {
   return nil,fmt.Errorf("imported artifact %s failed digest verification",filename)
  }
  records[filename]=map[string]any{"artifact_id":id,"sha256":expected,"size":size}
 }
 return map[string]any{"inputs":records},nil
}

// verifyImmutableInputs preserves every imported original across all commands,
// not only when it is selected as an output. A derivative cannot silently
// replace the project artifact used as its source.
func(e *Engine)verifyImmutableInputs(ctx context.Context,w Workflow) error {
 hasInputs:=false
 for _,step:=range w.Steps{if step.ID=="inputs"{hasInputs=true;break}}
 if !hasInputs{return nil}
 imported,err:=resultObject(w,"inputs")
 if err!=nil{return err}
 entries:=obj(imported["inputs"])
 if len(entries)==0||len(entries)>8{return errors.New("input artifact receipt is invalid")}
 checkout,err:=resultObject(w,"checkout")
 if err!=nil{return err}
 repo:=str(checkout["repository"])
 if !repositoryName.MatchString(repo){return errors.New("input repository identity invalid")}
 for name,raw:=range entries{
  record:=obj(raw)
  digest:=str(record["sha256"])
  if !validSourcePath(name)||sensitiveSourcePath(name)||!hexDigest(digest){
   return errors.New("invalid imported source identity")
  }
  target:=path.Join(repoRoot(repo),name)
  result,err:=e.workspaceAction(ctx,w,"command",map[string]any{"command":"sha256sum -- "+shellQuote(target)})
  if err!=nil{return err}
  code,confirmed:=result["exit_code"].(float64)
  fields:=strings.Fields(str(result["stdout"]))
  if !confirmed||code!=0||len(fields)<2||fields[0]!=digest{
   return fmt.Errorf("imported original %s was altered",name)
  }
 }
 return nil
}
