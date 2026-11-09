package controller

import (
 "encoding/base64"
 "encoding/json"
 "io"
 "net/http"
 "net/http/httptest"
 "os"
 "path/filepath"
 "strings"
 "sync"
 "testing"
)

const gitLabInitialCommit = "1111111111111111111111111111111111111111"
const gitLabNextCommit = "2222222222222222222222222222222222222222"
const gitLabFileCommit = "3333333333333333333333333333333333333333"

func TestGitLabRepositoryLocatorsAreProviderScoped(t *testing.T){
 for _,name:=range []string{
  "gitlab:research/physics","gitlab:lab/electronics/firmware",
 } {
  if repo,ok:=gitLabRepoName(name);!ok||repo==""||!validKnowledgeRepository(name){
   t.Fatalf("valid GitLab locator rejected: %q",name)
  }
 }
 for _,name:=range []string{
  "gitlab:research","gitlab:../private","gitlab:a/../private",
  "gitlab://user/pass","gitlab:group/.git","gitlab:group/a.lock",
  "gitlab:/group/repository","gitlab:group/rep?query=1",
 }{
  if _,ok:=gitLabRepoName(name);ok||validSourceRepository(name){
   t.Fatalf("unsafe GitLab locator accepted: %q",name)
  }
 }
 if _,ok:=gitLabRepoName("research/physics");ok{
  t.Fatal("GitHub repository silently treated as GitLab")
 }
}

func TestGitLabPATProtectedFile(t *testing.T){
 t.Setenv("PWM_GITLAB_PAT_FILE","")
 token,err:=configuredGitLabPAT()
 if err!=nil||token!=""{t.Fatal("absent PAT should be unconfigured")}
 path:=filepath.Join(t.TempDir(),"pat")
 value:="glpat-abcdefghijklmnopqrstuvwxyz"
 if err:=os.WriteFile(path,[]byte(value+"\n"),0600);err!=nil{t.Fatal(err)}
 t.Setenv("PWM_GITLAB_PAT_FILE",path)
 got,err:=configuredGitLabPAT()
 if err!=nil||got!=value{t.Fatalf("private PAT load failed: %v",err)}
 if err:=os.Chmod(path,0644);err!=nil{t.Fatal(err)}
 if _,err:=configuredGitLabPAT();err==nil{t.Fatal("world-readable PAT was accepted")}
 if err:=os.Chmod(path,0640);err!=nil{t.Fatal(err)}
 if _,err:=configuredGitLabPAT();err!=nil{t.Fatal("service-group read PAT was rejected",err)}
 if err:=os.WriteFile(path,[]byte("contains space"),0600);err!=nil{t.Fatal(err)}
 if _,err:=configuredGitLabPAT();err==nil{t.Fatal("invalid PAT bytes were accepted")}
}

func TestGitLabPATFileReadAndVersionedPublication(t *testing.T){
 type fixtureState struct{
  sync.Mutex
  head,content string
  writes int
 }
 state:=&fixtureState{head:gitLabInitialCommit,content:"# Original\n"}
 server:=httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter,r *http.Request){
  if r.Header.Get("PRIVATE-TOKEN")!="glpat-private-test-token"{
   t.Error("GitLab token missing or substituted")
   w.WriteHeader(http.StatusUnauthorized);return
  }
  if !strings.Contains(r.URL.EscapedPath(),"research%2Fnotes"){
   t.Errorf("GitLab project path was not safely escaped: %s",r.URL.EscapedPath())
   w.WriteHeader(http.StatusBadRequest);return
  }
  w.Header().Set("Content-Type","application/json")
  state.Lock()
  defer state.Unlock()
  switch{
  case strings.HasSuffix(r.URL.Path,"/projects/research/notes"):
   _,_=io.WriteString(w,`{"id":45,"path_with_namespace":"research/notes"}`)
  case strings.Contains(r.URL.Path,"/repository/commits/"):
   ref:=strings.TrimPrefix(r.URL.Path[strings.Index(r.URL.Path,"/repository/commits/"):],"/repository/commits/")
   if ref=="main"||ref==state.head||ref==gitLabInitialCommit{
    id:=state.head
    if ref==gitLabInitialCommit{id=gitLabInitialCommit}
    _,_=io.WriteString(w,`{"id":"`+id+`"}`)
   }else{w.WriteHeader(http.StatusNotFound);_,_=io.WriteString(w,`{"message":"not found"}`)}
  case strings.Contains(r.URL.Path,"/repository/files/"):
   if r.Method=="GET"{
    body:=map[string]any{
     "encoding":"base64","file_path":"docs/Project.md",
     "content":base64.StdEncoding.EncodeToString([]byte(state.content)),
     "last_commit_id":gitLabFileCommit,"blob_id":gitLabFileCommit,
    }
    _=json.NewEncoder(w).Encode(body)
   }else if r.Method=="PUT"{
    var input struct{
     Branch string `json:"branch"`
     LastCommitID string `json:"last_commit_id"`
     Content string `json:"content"`
    }
    if err:=json.NewDecoder(r.Body).Decode(&input);err!=nil||input.Branch!="main"||
     input.LastCommitID!=gitLabFileCommit{
     w.WriteHeader(400);return
    }
    state.writes++
    state.content=input.Content
    state.head=gitLabNextCommit
    _,_=io.WriteString(w,`{"branch":"main","file_path":"docs/Project.md"}`)
   }else{w.WriteHeader(http.StatusMethodNotAllowed)}
  default:
   w.WriteHeader(http.StatusNotFound)
   _,_=io.WriteString(w,`{"message":"not found"}`)
  }
 }))
 defer server.Close()
 gitlab:=NewGitLab("glpat-private-test-token")
 gitlab.API=server.URL+"/api/v4"
 gitlab.Client=server.Client()
 if _,err:=gitlab.Project("gitlab:research/notes");err!=nil{t.Fatal(err)}
 sha,err:=gitlab.Commit("gitlab:research/notes","main")
 if err!=nil||sha!=gitLabInitialCommit{t.Fatalf("commit resolution: %s %v",sha,err)}
 original,err:=gitlab.File("gitlab:research/notes","docs/Project.md",sha)
 if err!=nil||asString(original["content"])!="# Original\n"{
  t.Fatalf("file read failed: %v %v",original,err)
 }
 if _,err:=gitlab.CommitKnowledgeDocument("gitlab:research/notes","main",
  "docs/Project.md",gitLabNextCommit,"# Incorrect\n","Write");err==nil{
  t.Fatal("stale expected branch head allowed a write")
 }
 if state.writes!=0{t.Fatal("revision mismatch caused a GitLab mutation")}
 result,err:=gitlab.CommitKnowledgeDocument("gitlab:research/notes","main",
  "docs/Project.md",gitLabInitialCommit,"# Accepted\n","Save approved document")
 if err!=nil||result["verified"]!=true||result["sha"]!=gitLabNextCommit{
  t.Fatalf("GitLab revisioned publication failed: %v %v",result,err)
 }
 if state.writes!=1||state.content!="# Accepted\n"{
  t.Fatal("file last-commit guard was not used")
 }
 if _,err:=gitlab.CommitKnowledgeDocument("gitlab:research/notes","main",
  "docs/Project.md",gitLabInitialCommit,"# Replay\n","Save");err==nil{
  t.Fatal("stale publication replay was accepted")
 }
}

func TestGitLabArchiveUsesPinnedCommitWithoutRedirectingPAT(t *testing.T){
 redirected:=false
 var server *httptest.Server
 server=httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter,r *http.Request){
  if r.Header.Get("PRIVATE-TOKEN")!="glpat-private-test-token"{t.Error("missing scoped PAT")}
  if strings.Contains(r.URL.Path,"/repository/commits/"){
   w.Header().Set("Content-Type","application/json")
   _,_=io.WriteString(w,`{"id":"`+gitLabInitialCommit+`"}`)
   return
  }
  if strings.Contains(r.URL.Path,"/repository/archive.tar.gz"){
   if r.URL.Query().Get("sha")!=gitLabInitialCommit{t.Error("archive request not pinned")}
   if redirected{
    w.Header().Set("Location","https://untrusted.example/archive.tar.gz")
    w.WriteHeader(http.StatusFound);return
   }
   w.Header().Set("Content-Type","application/gzip")
   _,_=w.Write([]byte("archive-bytes"))
   return
  }
  w.WriteHeader(http.StatusNotFound)
 }))
 defer server.Close()
 gitlab:=NewGitLab("glpat-private-test-token")
 gitlab.Client=server.Client()
 gitlab.API=server.URL+"/api/v4"
 sha,body,err:=gitlab.SnapshotReader("gitlab:research/notes","main")
 if err!=nil||sha!=gitLabInitialCommit{t.Fatalf("pinned snapshot failed: %v %s",err,sha)}
 payload,err:=io.ReadAll(body)
 body.Close()
 if err!=nil||string(payload)!="archive-bytes"{t.Fatal("archive stream incorrect",err)}
 redirected=true
 if _,reader,err:=gitlab.SnapshotReader("gitlab:research/notes","main");err==nil{
  if reader!=nil{reader.Close()}
  t.Fatal("archive redirect was followed")
 }
}
