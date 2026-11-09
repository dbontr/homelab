package controller

import (
 "bytes"
 "encoding/base64"
 "encoding/json"
 "errors"
 "fmt"
 "io"
 "net/http"
 "net/url"
 "os"
 "path/filepath"
 "regexp"
 "strings"
 "time"
)

// A GitLab repository has an explicit provider prefix. This prevents an
// identically named GitHub repository from inheriting a GitLab authorization.
var gitLabPathPart = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.-]{0,99}$`)

func gitLabRepoName(locator string) (string, bool) {
 if !strings.HasPrefix(locator, "gitlab:") { return "", false }
 name := strings.TrimPrefix(locator, "gitlab:")
 if len(name) > 500 { return "", false }
 parts := strings.Split(name, "/")
 if len(parts) < 2 || len(parts) > 8 { return "", false }
 for _, part := range parts {
  if !gitLabPathPart.MatchString(part) || part == "." || part == ".." ||
   strings.HasSuffix(part, ".lock") { return "", false }
 }
 return name, true
}

func configuredGitLabPAT() (string, error) {
 path := os.Getenv("PWM_GITLAB_PAT_FILE")
 if path == "" { return "", nil }
 if !filepath.IsAbs(path) { return "", errors.New("GitLab credential file must have an absolute path") }
 info, err := os.Lstat(path)
 if err != nil { return "", fmt.Errorf("GitLab credential file is unavailable: %w", err) }
 // A Kubernetes Secret may be readable by its assigned service group. Neither
 // writable group access nor any other-user access is permitted.
 if !info.Mode().IsRegular() || info.Mode().Perm()&0037 != 0 ||
  info.Size() > 4096 || info.Size() < 20 {
  return "", errors.New("GitLab credential file has unsafe access or length")
 }
 data, err := os.ReadFile(path)
 if err != nil { return "", errors.New("GitLab credential file is not readable") }
 token := strings.TrimSuffix(string(data), "\n")
 if len(token) < 20 || len(token) > 4096 || strings.ContainsAny(token,"\x00\r\n\t ") {
  return "", errors.New("invalid GitLab credential file contents")
 }
 return token, nil
}

// GitLab is configured for one operator-approved HTTPS API origin. It never
// accepts arbitrary API URLs from an MCP tool or forwards its PAT on redirects.
type GitLab struct {
 Token string
 TokenSource func()(string,error)
 API string
 Client *http.Client
}

func NewGitLab(token string) *GitLab {
 return &GitLab{Token:token, API:"https://gitlab.com/api/v4",
  Client:&http.Client{Timeout:35*time.Second}}
}

func (g *GitLab) configured() bool { return g != nil && (g.Token != "" || g.TokenSource != nil) }

func (g *GitLab) credential()(string,error){
 if g==nil{return "",errors.New("GitLab is unavailable")}
 if g.TokenSource!=nil{
  token,err:=g.TokenSource()
  if err!=nil||len(token)<20{return "",errors.New("GitLab vault credential unavailable")}
  return token,nil
 }
 if g.Token==""{return "",errors.New("GitLab PAT is not configured")}
 return g.Token,nil
}

func (g *GitLab) request(method, path string, query url.Values, body any, limit int64) (int, []byte, error) {
 if !g.configured(){ return 0,nil,errors.New("GitLab PAT is not configured") }
 base,err:=url.Parse(g.API)
 if err!=nil||base.Scheme!="https"||base.Host==""||base.User!=nil||
  base.RawQuery!=""||base.Fragment!=""||base.Opaque!=""{
  return 0,nil,errors.New("invalid GitLab API origin")
 }
 if !strings.HasPrefix(path,"/projects/") || strings.Contains(path,"#") {
  return 0,nil,errors.New("invalid GitLab API path")
 }
 target,err:=url.Parse(g.API+path)
 if err!=nil||target.Host!=base.Host||target.Scheme!=base.Scheme{
  return 0,nil,errors.New("untrusted GitLab API target")
 }
 target.RawQuery=query.Encode()
 var payload io.Reader
 if body!=nil {
  raw,err:=json.Marshal(body)
  if err!=nil||len(raw)>256<<10 { return 0,nil,errors.New("invalid GitLab request body") }
  payload=bytes.NewReader(raw)
 }
 req,err:=http.NewRequest(method,target.String(),payload)
 if err!=nil{return 0,nil,errors.New("invalid GitLab API request")}
 token,err:=g.credential()
 if err!=nil{return 0,nil,err}
 req.Header.Set("PRIVATE-TOKEN",token)
 req.Header.Set("Accept","application/json")
 req.Header.Set("User-Agent","eminence-compute")
 if body!=nil{req.Header.Set("Content-Type","application/json")}
 client:=*g.Client
 client.CheckRedirect=func(*http.Request,[]*http.Request)error{
  return errors.New("GitLab API redirects are disabled")
 }
 resp,err:=client.Do(req)
 if err!=nil{return 0,nil,errors.New("GitLab API request failed")}
 defer resp.Body.Close()
 data,err:=io.ReadAll(io.LimitReader(resp.Body,limit+1))
 if err!=nil||int64(len(data))>limit{return 0,nil,errors.New("GitLab API response exceeds limit")}
 if resp.StatusCode>=300 && resp.StatusCode!=http.StatusNotFound {
  return 0,nil,fmt.Errorf("GitLab HTTP %d",resp.StatusCode)
 }
 return resp.StatusCode,data,nil
}

func (g *GitLab) requestJSON(method,path string,query url.Values,body any)(int,map[string]any,error){
 status,raw,err:=g.request(method,path,query,body,256<<10)
 if err!=nil||status==http.StatusNotFound{return status,nil,err}
 var value map[string]any
 if err:=json.Unmarshal(raw,&value);err!=nil||value==nil{
  return 0,nil,errors.New("invalid GitLab API JSON")
 }
 return status,value,nil
}

func gitLabProjectPath(repo string)(string,error){
 name,ok:=gitLabRepoName(repo)
 if !ok { return "",errors.New("invalid GitLab repository identifier") }
 return "/projects/"+url.PathEscape(name),nil
}

func (g *GitLab) Project(repo string)(map[string]any,error){
 prefix,err:=gitLabProjectPath(repo)
 if err!=nil{return nil,err}
 status,record,err:=g.requestJSON("GET",prefix,nil,nil)
 if err!=nil||status!=http.StatusOK{
  return nil,errors.New("GitLab repository is unavailable")
 }
 name,_:=gitLabRepoName(repo)
 if asString(record["path_with_namespace"])!=name||asFloat(record["id"])<=0{
  return nil,errors.New("GitLab returned a different repository identity")
 }
 return record,nil
}

func (g *GitLab) Commit(repo,ref string)(string,error){
 prefix,err:=gitLabProjectPath(repo)
 if err!=nil{return "",err}
 if !validateBranch(ref)&&!commitID.MatchString(ref){
  return "",errors.New("invalid GitLab ref")
 }
 status,record,err:=g.requestJSON("GET",prefix+"/repository/commits/"+url.PathEscape(ref),nil,nil)
 if err!=nil||status!=http.StatusOK{return "",errors.New("GitLab commit is unavailable")}
 sha:=asString(record["id"])
 if !commitID.MatchString(sha){return "",errors.New("invalid GitLab commit identity")}
 return sha,nil
}

func (g *GitLab) File(repo,path,ref string)(map[string]any,error){
 prefix,err:=gitLabProjectPath(repo)
 if err!=nil{return nil,err}
 if !validFilePath(path)||(!validateBranch(ref)&&!commitID.MatchString(ref)){
  return nil,errors.New("invalid GitLab file path or ref")
 }
 query:=url.Values{"ref":{ref}}
 status,record,err:=g.requestJSON("GET",prefix+"/repository/files/"+url.PathEscape(path),query,nil)
 if status==http.StatusNotFound{return nil,fmt.Errorf("GitLab HTTP 404: %w",os.ErrNotExist)}
 if err!=nil||status!=http.StatusOK{return nil,errors.New("GitLab repository file unavailable")}
 if record["encoding"]!="base64" || asString(record["file_path"])!=path{
  return nil,errors.New("unexpected GitLab repository file")
 }
 encoded:=strings.ReplaceAll(asString(record["content"]),"\n","")
 data,err:=base64.StdEncoding.DecodeString(encoded)
 if err!=nil||len(data)>128<<10{return nil,errors.New("invalid or oversized GitLab file")}
 return map[string]any{"repository":repo,"path":path,
  "revision":ref,"content":string(data),"content_sha256":checksum(data),
  "last_commit_id":record["last_commit_id"],"blob_id":record["blob_id"]},nil
}

// CommitKnowledgeDocument protects a GitLab file with its last-commit guard.
// GitLab does not provide a branch-wide compare-and-swap through this API,
// so the expected branch head is checked before writing and the file is read
// back at the resulting commit. Unrelated concurrent branch changes may merge.
func (g *GitLab) CommitKnowledgeDocument(repo,branch,path,expected,body,message string)(map[string]any,error){
 prefix,err:=gitLabProjectPath(repo)
 if err!=nil{return nil,err}
 if !validateBranch(branch)||!commitID.MatchString(expected)||!validFilePath(path)||
  body==""||len(body)>128<<10||strings.TrimSpace(message)==""||len(message)>256{
  return nil,errors.New("invalid GitLab knowledge publication")
 }
 if _,err:=g.Project(repo);err!=nil{return nil,err}
 current,err:=g.Commit(repo,branch)
 if err!=nil{return nil,err}
 if current!=expected{return nil,errors.New("GitLab branch revision conflict")}
 route:=prefix+"/repository/files/"+url.PathEscape(path)
 status,original,err:=g.requestJSON("GET",route,url.Values{"ref":{branch}},nil)
 if err!=nil{return nil,err}
 method:="PUT"
 payload:=map[string]any{"branch":branch,"content":body,"commit_message":message}
 if status==http.StatusNotFound{
  method="POST"
 }else{
  last:=asString(original["last_commit_id"])
  if !commitID.MatchString(last){return nil,errors.New("GitLab file revision missing")}
  payload["last_commit_id"]=last
  if asString(original["encoding"])!="base64"{return nil,errors.New("unexpected GitLab file encoding")}
  bytesOriginal,err:=base64.StdEncoding.DecodeString(strings.ReplaceAll(asString(original["content"]),"\n",""))
  if err!=nil{return nil,errors.New("invalid GitLab prior file contents")}
  if bytes.Equal(bytesOriginal,[]byte(body)){return nil,errors.New("GitLab knowledge content is unchanged")}
 }
 status,_,err=g.requestJSON(method,route,nil,payload)
 if err!=nil||status<200||status>=300{
  return nil,errors.New("GitLab rejected knowledge publication")
 }
 next,err:=g.Commit(repo,branch)
 if err!=nil{return nil,errors.New("GitLab publication could not be verified")}
 file,err:=g.File(repo,path,next)
 if err!=nil||asString(file["content"])!=body{
  return nil,errors.New("GitLab publication content was not confirmed")
 }
 return map[string]any{"repository":repo,"branch":branch,
  "path":path,"sha":next,"content_sha256":checksum([]byte(body)),
  "verified":true,"revision_guard":"branch_preflight_and_file_last_commit"},nil
}
