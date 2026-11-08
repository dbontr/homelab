package agentflow

import (
 "bytes"
 "context"
 "encoding/json"
 "errors"
 "fmt"
 "path"
 "sort"
 "strings"
 "time"
)

type ToolCaller interface {
 Call(context.Context,string,map[string]any)(map[string]any,error)
}
type Author interface {
 Propose(context.Context,string,map[string]string,[]string)(map[string]string,error)
}

type Engine struct {
 Store *Store
 Compute ToolCaller
 Author Author
 AllowedProjects map[string]bool
}
func (e *Engine) Submit(p Program)(RunSummary,error){
 if e==nil||e.Store==nil||e.Compute==nil{return RunSummary{},errors.New("engineering service not configured")}
 if !e.AllowedProjects[p.Project]{return RunSummary{},errors.New("engineering project not authorized")}
 if len(p.Changes)==0&&e.Author==nil{return RunSummary{},errors.New("automatic authoring provider not configured")}
 w,err:=Compile(p);if err!=nil{return RunSummary{},err}
 if err:=e.Store.Create(w);err!=nil{return RunSummary{},err}
 w,err=e.Store.Get(w.ID);if err!=nil{return RunSummary{},err}
 return Summarize(w),nil
}
func (e *Engine) Get(id string)(RunSummary,error){
 w,err:=e.Store.Get(id);if err!=nil{return RunSummary{},err}
 if !e.AllowedProjects[w.Project]{return RunSummary{},errors.New("engineering project not authorized")}
 return Summarize(w),nil
}
func (e *Engine) List()([]RunSummary,error){
 runs,err:=e.Store.Summaries();if err!=nil{return nil,err}
 selected:=make([]RunSummary,0,len(runs))
 for _,run:=range runs{if e.AllowedProjects[run.Project]{selected=append(selected,run)}}
 return selected,nil
}
func (e *Engine) Cancel(id string,revision uint64)error{
 w,err:=e.Store.Get(id);if err!=nil{return err}
 if !e.AllowedProjects[w.Project]{return errors.New("engineering project not authorized")}
 return e.Store.Cancel(id,revision)
}
func resultObject(w Workflow,id string)(map[string]any,error){
 for _,st:=range w.Steps{
  if st.ID==id{
   if st.Status!=Succeeded{return nil,errors.New("required step has not completed")}
   var out map[string]any
   if err:=json.Unmarshal(st.Result,&out);err!=nil{return nil,errors.New("invalid recorded step output")}
   return out,nil
  }
 }
 return nil,errors.New("unknown predecessor")
}
func str(v any)string{out,_:=v.(string);return out}
func obj(v any)map[string]any{out,_:=v.(map[string]any);return out}
func stringMap(v any)(map[string]string,error){
 raw,err:=json.Marshal(v);if err!=nil{return nil,err}
 out:=map[string]string{}
 if err:=json.Unmarshal(raw,&out);err!=nil{return nil,err}
 return out,nil
}
func workspaceID(w Workflow)(string,error){
 provision,err:=resultObject(w,"provision");if err!=nil{return "",err}
 id:=str(provision["id"])
 if !validID(id){return "",errors.New("invalid compute workspace identity")}
 return id,nil
}
func repoRoot(repository string)string{
 return "repos/"+strings.ReplaceAll(repository,"/","__")
}
func decodeResult(v map[string]any)(map[string]any,error){
 value:=obj(v["result"])
 if value==nil{return nil,errors.New("compute operation has no confirmed result")}
 return value,nil
}
func (e *Engine) awaitOperation(ctx context.Context,project,ws,id string)(map[string]any,error){
 if !validID(id){return nil,errors.New("invalid compute operation identity")}
 ticker:=time.NewTicker(time.Second)
 defer ticker.Stop()
 for {
  reply,err:=e.Compute.Call(ctx,"workspace_operation",map[string]any{"project_id":project,"workspace_id":ws,"operation_id":id})
  if err!=nil{return nil,err}
  switch str(reply["status"]){
  case "succeeded":
   return decodeResult(reply)
  case "failed","cancelled","stopped":
   return nil,fmt.Errorf("compute operation %s",str(reply["status"]))
  case "queued","leased","running":
  default:
   return nil,errors.New("unknown compute operation status")
  }
  select{case <-ctx.Done():return nil,ctx.Err();case <-ticker.C:}
 }
}
func (e *Engine) awaitReady(ctx context.Context,project,ws string)error{
 ticker:=time.NewTicker(time.Second);defer ticker.Stop()
 for {
  reply,err:=e.Compute.Call(ctx,"workspace_list",map[string]any{"project_id":project})
  if err!=nil{return err}
  list,ok:=reply["result"].([]any)
  if !ok{return errors.New("invalid compute workspace listing")}
  found:=false
  for _,item:=range list{
   v:=obj(item)
   if str(v["id"])!=ws{continue}
   found=true
   switch str(v["status"]){
   case "ready":return nil
   case "starting": // Wait for the worker's observed status.
   default:return errors.New("workspace is not available for execution")
   }
  }
  if !found{return errors.New("created workspace not found")}
  select{case <-ctx.Done():return ctx.Err();case <-ticker.C:}
 }
}
func (e *Engine) workspaceAction(ctx context.Context,w Workflow,action string,payload map[string]any)(map[string]any,error){
 ws,err:=workspaceID(w);if err!=nil{return nil,err}
 reply,err:=e.Compute.Call(ctx,"workspace_call",map[string]any{"project_id":w.Project,"workspace_id":ws,"action":action,"payload":payload})
 if err!=nil{return nil,err}
 return e.awaitOperation(ctx,w.Project,ws,str(reply["operation_id"]))
}
func (e *Engine) Dispatch(ctx context.Context,key,kind string,input json.RawMessage)(json.RawMessage,error){
 if e==nil||e.Store==nil||e.Compute==nil{return nil,errors.New("engineering service not configured")}
 keys:=strings.Split(key,"/")
 if len(keys)!=2||!validID(keys[0])||!validID(keys[1]){return nil,errors.New("invalid operation identity")}
 w,err:=e.Store.Get(keys[0]);if err!=nil{return nil,err}
 if !e.AllowedProjects[w.Project]||w.Cancelled{return nil,errors.New("engineering project not authorized")}
 found:=false
 for _,st:=range w.Steps{if st.ID==keys[1]&&st.Kind==kind&&st.Status==Running&&bytes.Equal(st.Input,input){found=true;break}}
 if !found{return nil,errors.New("operation differs from active workflow step")}
 var args map[string]any
 if err:=json.Unmarshal(input,&args);err!=nil{return nil,err}
 var out map[string]any
 switch kind{
 case "provision":
  params:=map[string]any{"project_id":w.Project,"image_pool_id":args["image_pool_id"],"cpu":args["cpu"],"memory_mb":args["memory_mb"],"duration_minutes":args["duration_minutes"]}
  out,err=e.Compute.Call(ctx,"workspace_create",params)
  if err==nil&&!validID(str(out["id"])){err=errors.New("compute returned invalid workspace ID")}
 case "checkout":
  ws,e2:=workspaceID(w);if e2!=nil{return nil,e2}
  if err=e.awaitReady(ctx,w.Project,ws);err!=nil{return nil,err}
  reply,e2:=e.Compute.Call(ctx,"github_checkout_repository",map[string]any{"project_id":w.Project,"workspace_id":ws,"repository":args["repository"],"ref":args["ref"]})
  if e2!=nil{return nil,e2}
  out,err=e.awaitOperation(ctx,w.Project,ws,str(reply["operation_id"]))
 case "inspect":
  repo:=str(args["repository"])
  list,_:=args["paths"].([]any)
  files:=map[string]string{}
  for _,entry:=range list{
   name:=str(entry)
   if !validSourcePath(name){return nil,errors.New("invalid source context path")}
   result,e2:=e.workspaceAction(ctx,w,"read_file",map[string]any{"path":path.Join(repoRoot(repo),name)})
   if e2!=nil{return nil,e2}
   body:=str(result["content"])
   if len(body)>24000{return nil,errors.New("context file exceeds authoring limit")}
   files[name]=body
  }
  out=map[string]any{"files":files}
 case "author":
  changes,e2:=stringMap(args["changes"]);if e2!=nil{return nil,e2}
  if len(changes)==0{
   if e.Author==nil{return nil,errors.New("automatic authoring unavailable")}
   inspected,e2:=resultObject(w,"inspect");if e2!=nil{return nil,e2}
   files,e2:=stringMap(inspected["files"]);if e2!=nil{return nil,e2}
   allowed,_:=args["allowed_paths"].([]any)
   paths:=make([]string,0,len(allowed));allow:=map[string]bool{}
   for _,item:=range allowed {p:=str(item);if !validSourcePath(p)||allow[p]{return nil,errors.New("invalid editable path")};allow[p]=true;paths=append(paths,p)}
   if len(paths)==0{return nil,errors.New("automatic authorship has no authorized file paths")}
   changes,e2=e.Author.Propose(ctx,str(args["objective"]),files,paths);if e2!=nil{return nil,e2}
   for name:=range changes{if !allow[name]{return nil,errors.New("author proposed an unauthorized file")}}
  }
  if err=validateChanges(changes);err!=nil{return nil,err}
  out=map[string]any{"changes":changes}
 case "apply":
  authored,e2:=resultObject(w,"author");if e2!=nil{return nil,e2}
  changes,e2:=stringMap(authored["changes"]);if e2!=nil{return nil,e2}
  checkout,e2:=resultObject(w,"checkout");if e2!=nil{return nil,e2}
  repo:=str(checkout["repository"])
  if !repositoryName.MatchString(repo){return nil,errors.New("invalid checked-out repository identity")}
  paths:=sortedPaths(changes)
  for _,name:=range paths{
   result,e2:=e.workspaceAction(ctx,w,"write_file",map[string]any{"path":path.Join(repoRoot(repo),name),"content":changes[name]})
   if e2!=nil{return nil,e2}
   if code,ok:=result["exit_code"].(float64);ok&&code!=0{return nil,errors.New("source update failed")}
  }
  out=map[string]any{"applied":paths}
 case "verify":
  commands,_:=args["commands"].([]any)
  checkout,e2:=resultObject(w,"checkout");if e2!=nil{return nil,e2}
  repo:=str(checkout["repository"])
  reports:=make([]map[string]any,0,len(commands))
  for _,entry:=range commands{
   command:=str(entry)
   result,e2:=e.workspaceAction(ctx,w,"command",map[string]any{"command":"cd "+repoRoot(repo)+" && "+command})
   if e2!=nil{return nil,e2}
   code,ok:=result["exit_code"].(float64)
   if !ok{return nil,errors.New("unconfirmed verification exit status")}
   reports=append(reports,map[string]any{"command":command,"exit_code":code})
   if code!=0{return nil,fmt.Errorf("verification command failed with exit code %.0f",code)}
  }
  out=map[string]any{"passed":true,"checks":reports}
 case "export":
  authored,e2:=resultObject(w,"author");if e2!=nil{return nil,e2}
  changes,e2:=stringMap(authored["changes"]);if e2!=nil{return nil,e2}
  checkout,e2:=resultObject(w,"checkout");if e2!=nil{return nil,e2}
  repo:=str(checkout["repository"])
  artifacts:=map[string]string{}
  for _,name:=range sortedPaths(changes){
   result,e2:=e.workspaceAction(ctx,w,"export_file",map[string]any{"path":path.Join(repoRoot(repo),name)})
   if e2!=nil{return nil,e2}
   id:=str(result["artifact_id"])
   if !validID(id){return nil,errors.New("export did not return an artifact identity")}
   artifacts[name]=id
  }
  out=map[string]any{"artifacts":artifacts}
 case "publish":
  verification,e2:=resultObject(w,"verify");if e2!=nil{return nil,e2}
  if verification["passed"]!=true{return nil,errors.New("verification is not confirmed")}
  exported,e2:=resultObject(w,"export");if e2!=nil{return nil,e2}
  artifacts,e2:=stringMap(exported["artifacts"]);if e2!=nil{return nil,e2}
  if len(artifacts)==0{return nil,errors.New("no verified change artifacts")}
  out,err=e.Compute.Call(ctx,"github_publish_artifacts",map[string]any{
   "project_id":w.Project,"repository":args["repository"],"base_branch":args["base_branch"],
   "expected_head":args["expected_head"],"changes":artifacts,"title":args["title"],"body":args["body"],
  })
  if err==nil&&out["draft"]!=true{err=errors.New("publication was not confirmed as draft")}
 default:return nil,errors.New("unsupported engineering step")
 }
 if err!=nil{return nil,err}
 raw,err:=json.Marshal(out);if err!=nil{return nil,err}
 if len(raw)>64<<10{return nil,errors.New("engineering result exceeds retention limit")}
 return raw,nil
}
func sortedPaths(files map[string]string)[]string{
 names:=make([]string,0,len(files))
 for name:=range files{names=append(names,name)}
 sort.Strings(names)
 return names
}
func validateChanges(files map[string]string)error{
 if len(files)==0||len(files)>16{return errors.New("invalid number of authored files")}
 total:=0
 for name,data:=range files{if !validSourcePath(name)||len(data)==0||len(data)>24000{return errors.New("invalid authored source change")};total+=len(name)+len(data)}
 if total>56<<10{return errors.New("authored source exceeds persistent result limit")}
 encoded,err:=json.Marshal(map[string]any{"changes":files})
 if err!=nil||len(encoded)>64<<10{return errors.New("authored source exceeds encoded result limit")}
 return nil
}
