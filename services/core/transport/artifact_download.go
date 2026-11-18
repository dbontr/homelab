package transport

import (
 "context"
 "crypto/sha256"
 "encoding/json"
 "encoding/hex"
 "errors"
 "fmt"
 "io"
 "mime"
 "net/http"
 "net/url"
 "os"
 "path"
 "regexp"
 "strings"

 "github.com/dbontr/homelab-new/system/security"
)

const maxBrowserArtifactBytes int64 = 256 << 20
var downloadArtifactName = regexp.MustCompile("^[A-Za-z0-9][A-Za-z0-9._-]{0,255}$")

// workspaceArtifactURL derives an exact project-scoped binary endpoint from
// the operator's already approved Workspace backend, never from user input.
func workspaceArtifactURL(endpoint,project,artifact string)(string,error){
 if !consoleProjectID.MatchString(project)||!visualArtifactID.MatchString(artifact){
  return "",errors.New("invalid project artifact identity")
 }
 u,err:=url.Parse(endpoint)
 if err!=nil||u.Host==""||u.User!=nil||u.RawQuery!=""||u.Fragment!=""||
  !(u.Scheme=="https"||(u.Scheme=="http"&&(u.Hostname()=="localhost"||u.Hostname()=="127.0.0.1"||u.Hostname()=="::1")))||
  !strings.HasSuffix(u.Path,"/mcp"){
  return "",errors.New("workspace endpoint does not support artifact downloads")
 }
 base:=strings.TrimSuffix(u.Path,"/mcp")
 u.Path=path.Join(base,"api","projects",project,"artifacts",artifact)
 if !strings.HasPrefix(u.Path,"/"){u.Path="/"+u.Path}
 return u.String(),nil
}

// Confirm exact artifact ownership and metadata through the same authorized MCP
// controller before retrieving a binary from the controller's native API.
func(s *Server)scopedArtifactMetadata(ctx context.Context,backend Backend,project,id string)(map[string]any,error){
 params,_:=json.Marshal(map[string]any{
  "name":"artifact_info",
  "arguments":map[string]string{"project_id":project,"artifact_id":id},
 })
 var response any
 if err:=s.forward(ctx,backend,rpcRequest{
  JSONRPC:"2.0",ID:json.RawMessage("1"),Method:"tools/call",Params:params,
 },&response);err!=nil{return nil,err}
 raw,ok:=response.(map[string]any)
 if !ok||raw["isError"]==true{return nil,errors.New("artifact not authorized")}
 meta,ok:=raw["structuredContent"].(map[string]any)
 if !ok||meta["id"]!=id||meta["project"]!=project{return nil,errors.New("artifact scope mismatch")}
 digest,_:=meta["sha256"].(string)
 name,_:=meta["name"].(string)
 size,ok:=meta["size"].(float64)
 if !visualArtifactSHA256.MatchString(digest)||!downloadArtifactName.MatchString(name)||!ok||size<1||size>float64(maxBrowserArtifactBytes){
  return nil,errors.New("artifact metadata or size invalid")
 }
 return meta,nil
}

func (s *Server)uiDownloadProjectArtifact(w http.ResponseWriter,r *http.Request){
 project,id:=r.PathValue("project"),r.PathValue("artifact")
 if !consoleProjectID.MatchString(project)||!visualArtifactID.MatchString(id){
  writeError(w,http.StatusBadRequest,"invalid project artifact identity");return
 }
 var backend *Backend
 for i:=range s.backend{
  if s.backend[i].Name=="workspace"{backend=&s.backend[i];break}
 }
 if backend==nil{writeError(w,http.StatusServiceUnavailable,"artifact download unavailable");return}
  artifact,err:=s.scopedArtifactMetadata(r.Context(),*backend,project,id)
  if err!=nil{writeError(w,http.StatusNotFound,"artifact not found or access denied");return}

 endpoint,err:=workspaceArtifactURL(backend.URL,project,id)
 if err!=nil{writeError(w,http.StatusServiceUnavailable,"artifact endpoint unavailable");return}
 req,err:=http.NewRequestWithContext(r.Context(),http.MethodGet,endpoint,nil)
 if err!=nil{writeError(w,http.StatusServiceUnavailable,"artifact endpoint invalid");return}
 req.Header.Set("Accept","application/octet-stream")
 token:=backend.Token
 if backend.TokenSource!=nil{
  if token!=""{writeError(w,http.StatusServiceUnavailable,"conflicting backend credentials");return}
  token,err=backend.TokenSource()
  if err!=nil||len(token)<20||strings.ContainsAny(token,"\x00\r\n\t "){
   writeError(w,http.StatusServiceUnavailable,"artifact service credential unavailable");return
  }
 }
 if token!="" {req.Header.Set("Authorization","Bearer "+token)}
 if backend.ForwardOwnerAssertion&&backend.Name=="workspace"{
  assertion:=security.AccessAssertion(r.Context())
  if assertion!=""&&len(assertion)<=16<<10{
   req.Header.Set("Cf-Access-Jwt-Assertion",assertion)
  }
 }
 response,err:=s.client.Do(req)
 if err!=nil{writeError(w,http.StatusServiceUnavailable,"artifact service unavailable");return}
 defer response.Body.Close()
 if response.StatusCode!=http.StatusOK{
  writeError(w,http.StatusNotFound,"artifact unavailable or access denied");return
 }
 kind,_,err:=mime.ParseMediaType(response.Header.Get("Content-Type"))
 if err!=nil||kind!="application/octet-stream"{
  writeError(w,http.StatusBadGateway,"unsupported artifact response");return
 }
 digest:=response.Header.Get("X-Content-SHA256")
 if digest!=artifact["sha256"] {
  writeError(w,http.StatusBadGateway,"artifact checksum unavailable");return
 }
 _,disposition,err:=mime.ParseMediaType(response.Header.Get("Content-Disposition"))
 filename:=disposition["filename"]
 if err!=nil||!downloadArtifactName.MatchString(filename)||filename!=artifact["name"]{
  writeError(w,http.StatusBadGateway,"invalid artifact filename");return
 }
 if response.ContentLength>maxBrowserArtifactBytes||(response.ContentLength>=0&&float64(response.ContentLength)!=artifact["size"]){
  writeError(w,http.StatusRequestEntityTooLarge,"artifact exceeds browser download limit");return
 }
 // Validate the complete response before exposing a byte. Large CR3 files
 // remain off the heap, with an owner-private temporary file removed on exit.
 temp,err:=os.CreateTemp("","homelab-project-export-*")
 if err!=nil{writeError(w,http.StatusServiceUnavailable,"artifact staging unavailable");return}
 defer os.Remove(temp.Name())
 defer temp.Close()
 hash:=sha256.New()
 n,err:=io.Copy(io.MultiWriter(temp,hash),io.LimitReader(response.Body,maxBrowserArtifactBytes+1))
 if err!=nil||n>maxBrowserArtifactBytes||n<1||float64(n)!=artifact["size"]||response.ContentLength>=0&&n!=response.ContentLength{
  writeError(w,http.StatusBadGateway,"incomplete or excessive artifact transfer");return
 }
 if hex.EncodeToString(hash.Sum(nil))!=digest{
  writeError(w,http.StatusBadGateway,"artifact integrity verification failed");return
 }
 if _,err:=temp.Seek(0,0);err!=nil{
  writeError(w,http.StatusInternalServerError,"artifact staging failed");return
 }
 w.Header().Set("Content-Type","application/octet-stream")
 w.Header().Set("Content-Disposition",mime.FormatMediaType("attachment",map[string]string{"filename":filename}))
 w.Header().Set("Cache-Control","private, no-store")
 w.Header().Set("X-Content-Type-Options","nosniff")
 w.Header().Set("X-Content-SHA256",digest)
 w.Header().Set("Content-Length",fmt.Sprint(n))
 _,_=io.Copy(w,temp)
}

