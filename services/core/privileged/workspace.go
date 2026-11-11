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
 "syscall"
 "time"

 "github.com/dbontr/homelab-new/system/canonicaljson"
 "github.com/dbontr/homelab-new/services/core/security"
)

// WorkspacePolicy restricts the controller endpoints and resource identities
// available to the privileged executor.
type WorkspacePolicy struct {
 Endpoint string
 TokenFile string
 // TokenSource supports legacy trusted credential injection; the network
 // vault never constructs an unscoped source.
 TokenSource func()(string,error) `json:"-"`
 ScopedTokenSource func(context.Context,security.CredentialTicket,string,string,json.RawMessage)(string,error) `json:"-"`
 CredentialAlias string `json:"-"`
 Projects []string
 Workers []string
 Devices []string
 Images []string
 ImageReferences []string
 Software []string
 Programs []string
 Packages []string
 Client *http.Client `json:"-"`
}

type CredentialAuthorization struct {
 Operation string
 Target string
 Arguments json.RawMessage
 Tickets []security.CredentialTicket
}

var workspaceIdentifier=regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_.-]{0,95}$`)
var workspaceResourceID=regexp.MustCompile(`^[a-z][a-z0-9-]{1,62}$`)
var workspacePackage=regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9+._-]{0,79}$`)
var workspaceCredentialAlias=regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_-]{0,63}$`)

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
 sources:=0
 if p.TokenFile!=""{sources++}
 if p.TokenSource!=nil{sources++}
 if p.ScopedTokenSource!=nil{sources++}
 if sources!=1{
  return errors.New("Workspace must have exactly one protected credential authority")
 }
 if p.ScopedTokenSource!=nil&&!workspaceCredentialAlias.MatchString(p.CredentialAlias){
  return errors.New("Workspace scoped credential alias is invalid")
 }
 if p.TokenFile!=""{
  if !filepath.IsAbs(p.TokenFile){return errors.New("Workspace credential requires an absolute protected file")}
  info,err:=os.Lstat(p.TokenFile)
  if err!=nil||!info.Mode().IsRegular()||info.Mode().Perm()&0077!=0{
   return errors.New("Workspace credential file must be a protected regular file")
  }
 }
 all:=[][]string{p.Projects,p.Workers,p.Images,p.Software,p.Packages,p.Programs,p.Devices}
 enabled:=false
 for i,values:=range all{
  if len(values)>64{return errors.New("Workspace resource policy exceeds limit")}
  if len(values)>0{enabled=true}
  seen:=map[string]bool{}
  for _,value:=range values{
   pattern:=workspaceResourceID
   if i==4{pattern=workspacePackage}
   if i==6{pattern=workspaceDeviceIdentity}
   if !pattern.MatchString(value)||seen[value]{return errors.New("invalid Workspace resource allowlist")}
   seen[value]=true
  }
 }
 if len(p.ImageReferences)>64{return errors.New("Workspace image allowlist exceeds limit")}
 seenImages:=map[string]bool{}
 for _,reference:=range p.ImageReferences{
  if !validWorkspaceImageReference(reference)||seenImages[reference]{
   return errors.New("Workspace image reference is not a distinct immutable digest")
  }
  seenImages[reference]=true
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
 case "infrastructure__workspace_register_image", "infrastructure__workspace_propose_program",
  "infrastructure__workspace_review_program", "infrastructure__workspace_update_entry":
  return p.catalogArgs(operation,fields)
 case "infrastructure__workspace_create":
  args,err:=p.allocationArgs(fields)
  if err!=nil{return "",nil,err}
  return "workspace_create",args,nil
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

// readWorkspaceCredential validates the opened file descriptor so an attacker
// cannot replace a credential path with a symlink after service configuration.
func readWorkspaceCredential(path string)(string,error){
 const maxCredentialBytes=8192
 file,err:=os.OpenFile(path,os.O_RDONLY|syscall.O_NOFOLLOW,0)
 if err!=nil{return "",errors.New("Workspace authorization credential unavailable")}
 defer file.Close()
 info,err:=file.Stat()
 if err!=nil||!info.Mode().IsRegular()||info.Mode().Perm()&0077!=0||
  info.Size()<32||info.Size()>maxCredentialBytes{
  return "",errors.New("Workspace authorization credential unavailable")
 }
 secret,err:=io.ReadAll(io.LimitReader(file,maxCredentialBytes+1))
 if err!=nil||len(secret)>maxCredentialBytes{
  return "",errors.New("Workspace authorization credential unavailable")
 }
 token:=strings.TrimSuffix(string(secret),"\n")
 for i:=range secret{secret[i]=0}
 return token,nil
}

// Call sends only validated downstream arguments and a credential sourced from
// protected systemd storage. The response is never used as a command or URL.
func(p *WorkspacePolicy)Call(ctx context.Context,operation string,fields map[string]any,approvals ...CredentialAuthorization)error{
 name,arguments,err:=p.operationArgs(operation,fields)
 if err!=nil{return err}
 sources:=0
 if p.TokenFile!=""{sources++}
 if p.TokenSource!=nil{sources++}
 if p.ScopedTokenSource!=nil{sources++}
 if sources!=1{return errors.New("Workspace credential authority is ambiguous")}
 token:=""
 if p.ScopedTokenSource!=nil{
  if len(approvals)!=1 || approvals[0].Operation!=operation ||
   approvals[0].Target!=fields["host"]{
   return errors.New("Workspace credential lacks an exact authorized operation")
  }
  canonical,_,e:=canonicaljson.Object(approvals[0].Arguments)
  if e!=nil{return errors.New("invalid Workspace authorized arguments")}
  encoded,e:=json.Marshal(fields)
  if e!=nil{return errors.New("invalid Workspace operation arguments")}
  expected,_,e:=canonicaljson.Object(encoded)
  if e!=nil||!bytes.Equal(expected,canonical){
   return errors.New("Workspace operation does not match its credential authorization")
  }
  var ticket security.CredentialTicket
  count:=0
  for _,candidate:=range approvals[0].Tickets {
   if candidate.Alias==p.CredentialAlias{ticket=candidate;count++}
  }
  if count!=1{return errors.New("Workspace credential capability missing or ambiguous")}
  token,err=p.ScopedTokenSource(ctx,ticket,operation,approvals[0].Target,canonical)
  if err!=nil{return errors.New("Workspace scoped credential unavailable")}
 }else if p.TokenSource!=nil{
  token,err=p.TokenSource()
  if err!=nil{return errors.New("Workspace trusted credential unavailable")}
 }else{
  token,err=readWorkspaceCredential(p.TokenFile)
  if err!=nil{return err}
 }
 if len(token)<32||len(token)>8192||strings.ContainsAny(token,"\x00\r\n\t "){
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
