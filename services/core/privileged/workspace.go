package privileged

import (
 "bytes"
 "context"
 "encoding/json"
 "errors"
 "io"
 "net/http"
 "net/url"
 "os"
 "path/filepath"
 "regexp"
 "strings"
 "time"
)

// WorkspacePolicy delegates explicitly approved mutations to the canonical
// Workspace controller. The gateway never holds the controller root token.
type WorkspacePolicy struct {
 Endpoint string
 TokenFile string
 // TokenSource reads a provider token only after the independently authorized
 // operation has an audit-start entry.
 TokenSource func()(string,error) `json:"-"`
 Projects []string
 Workers []string
 Images []string
 Software []string
 Packages []string
 Client *http.Client `json:"-"`
}

var workspaceIdentifier=regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_.-]{0,95}$`)
var workspacePackage=regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9+._-]{0,79}$`)

func containsID(allowed []string,id string)bool{
 for _,candidate:=range allowed{if candidate==id{return true}}
 return false
}

func(p *WorkspacePolicy)validate()error{
 if p==nil{return nil}
 u,err:=url.Parse(p.Endpoint)
 if err!=nil||u.User!=nil||u.Opaque!=""||u.Fragment!=""||u.RawQuery!=""||u.Host==""||u.Path!="/mcp"||
  !(u.Scheme=="https"||(u.Scheme=="http"&&(u.Hostname()=="127.0.0.1"||u.Hostname()=="::1"||u.Hostname()=="localhost"))){
  return errors.New("Workspace endpoint must be an exact HTTPS or loopback MCP URL")
 }
 if (p.TokenFile=="")==(p.TokenSource==nil){
  return errors.New("Workspace must have exactly one protected credential authority")
 }
 if p.TokenFile!=""{
  if !filepath.IsAbs(p.TokenFile){return errors.New("Workspace credential requires an absolute protected file")}
  info,err:=os.Lstat(p.TokenFile)
  if err!=nil||!info.Mode().IsRegular()||info.Mode().Perm()&0077!=0{
   return errors.New("Workspace credential file must be a protected regular file")
  }
 }
 all:=[][]string{p.Projects,p.Workers,p.Images,p.Software,p.Packages}
 enabled:=false
 for i,values:=range all{
  if len(values)>64{return errors.New("Workspace resource policy exceeds limit")}
  if len(values)>0{enabled=true}
  seen:=map[string]bool{}
  for _,value:=range values{
   pattern:=workspaceIdentifier
   if i==4{pattern=workspacePackage}
   if !pattern.MatchString(value)||seen[value]{return errors.New("invalid Workspace resource allowlist")}
   seen[value]=true
  }
 }
 if !enabled{return errors.New("Workspace executor must name explicit resources")}
 return nil
}

func(p *WorkspacePolicy)operationArgs(operation string,fields map[string]any)(string,map[string]any,error){
 if p==nil{return "",nil,errors.New("Workspace administration is disabled")}
 value:=func(name string)string{
  field,_:=fields[name].(string)
  if !workspaceIdentifier.MatchString(field){return ""}
  return field
 }
 switch operation{
 case "infrastructure__workspace_stop":
  project,workspace:=value("project_id"),value("workspace_id")
  if len(fields)!=3||project==""||workspace==""||!containsID(p.Projects,project){
   return "",nil,errors.New("Workspace stop policy denied")
  }
  return "workspace_destroy",map[string]any{"project_id":project,"workspace_id":workspace},nil
 case "infrastructure__workspace_build_environment":
  project,workspace,name:=value("project_id"),value("workspace_id"),value("name")
  if len(fields)!=4||project==""||workspace==""||name==""||!containsID(p.Projects,project){
   return "",nil,errors.New("Workspace environment policy denied")
  }
  return "environment_build",map[string]any{"project_id":project,"workspace_id":workspace,"name":name},nil
 case "infrastructure__workspace_revoke_worker":
  worker:=value("node_id")
  if len(fields)!=2||worker==""||!containsID(p.Workers,worker){
   return "",nil,errors.New("Workspace worker policy denied")
  }
  return "worker_revoke",map[string]any{"node_id":worker},nil
 case "infrastructure__workspace_register_software":
  image,software:=value("image_id"),value("software_id")
  packages,ok:=fields["packages"].([]any)
  if (len(fields)!=4&&len(fields)!=5)||!ok||len(packages)==0||len(packages)>64||
   image==""||software==""||!containsID(p.Images,image)||!containsID(p.Software,software){
   return "",nil,errors.New("Workspace software policy denied")
  }
  selected:=make([]string,0,len(packages))
  seen:=map[string]bool{}
  for _,item:=range packages{
   name,ok:=item.(string)
   if !ok||!workspacePackage.MatchString(name)||!containsID(p.Packages,name)||seen[name]{
    return "",nil,errors.New("Workspace package is not allowed")
   }
   selected=append(selected,name);seen[name]=true
  }
  downstream:=map[string]any{"image_id":image,"software_id":software,"packages":selected}
  if len(fields)==5{
   description,ok:=fields["description"].(string)
   if !ok||len(description)>512||strings.ContainsAny(description,"\x00\r"){
    return "",nil,errors.New("invalid Workspace software description")
   }
   downstream["description"]=description
  }
  return "pool_register_software",downstream,nil
 default:
  return "",nil,errors.New("unregistered Workspace operation")
 }
}

// Call sends only validated downstream arguments and a credential sourced from
// protected systemd storage. The response is never used as a command or URL.
func(p *WorkspacePolicy)Call(ctx context.Context,operation string,fields map[string]any)error{
 name,arguments,err:=p.operationArgs(operation,fields)
 if err!=nil{return err}
 if (p.TokenFile=="")==(p.TokenSource==nil){
  return errors.New("Workspace credential authority is ambiguous")
 }
 token:=""
 if p.TokenSource!=nil{
  token,err=p.TokenSource()
  if err!=nil{return errors.New("Workspace vault credential unavailable")}
 }else{
  var secret []byte
  secret,err=os.ReadFile(p.TokenFile)
  if err!=nil||len(secret)>8192{return errors.New("Workspace authorization credential unavailable")}
  token=strings.TrimSuffix(string(secret),"\n")
  for i:=range secret{secret[i]=0}
 }
 if len(token)<32||strings.ContainsAny(token,"\x00\r\n\t "){
  return errors.New("Workspace credential is invalid")
 }
 payload,err:=json.Marshal(map[string]any{
  "jsonrpc":"2.0","id":1,"method":"tools/call",
  "params":map[string]any{"name":name,"arguments":arguments},
 })
 if err!=nil{return err}
 request,err:=http.NewRequestWithContext(ctx,http.MethodPost,p.Endpoint,bytes.NewReader(payload))
 if err!=nil{return err}
 request.Header.Set("Authorization","Bearer "+token)
 request.Header.Set("Accept","application/json")
 request.Header.Set("Content-Type","application/json")
 client:=p.Client
 if client==nil{client=&http.Client{Timeout:30*time.Second,Transport:&http.Transport{Proxy:nil}}}
 safe:=*client
 safe.CheckRedirect=func(*http.Request,[]*http.Request)error{return errors.New("Workspace redirects are forbidden")}
 response,err:=safe.Do(request)
 if err!=nil{return errors.New("Workspace controller unavailable")}
 defer response.Body.Close()
 if response.StatusCode!=http.StatusOK||!strings.HasPrefix(response.Header.Get("Content-Type"),"application/json"){
  return errors.New("Workspace controller rejected operation")
 }
 data,err:=io.ReadAll(io.LimitReader(response.Body,32769))
 if err!=nil||len(data)>32768{return errors.New("Workspace response exceeded limit")}
 return verifyWorkspaceToolResult(data)
}
