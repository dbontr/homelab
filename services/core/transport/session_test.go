package transport

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dbontr/homelab-new/system/security"
	"github.com/dbontr/homelab-new/system/session"
	"github.com/dbontr/homelab-new/system/store"
)

func TestConsoleBrowserSessionScopesMutations(t *testing.T) {
	db,err:=store.Open(filepath.Join(t.TempDir(),"state.json"),"UTC")
	if err!=nil{t.Fatal(err)}
	s,err:=New(db,strings.Repeat("g",40),nil)
	if err!=nil{t.Fatal(err)}
	record,err:=security.HashPassphrase([]byte("separate-owner-console-password"))
	if err!=nil{t.Fatal(err)}
	manager,err:=session.New(record)
	if err!=nil{t.Fatal(err)}
	s.SetSessions(manager)
	if err:=s.SetAllowedOrigins([]string{"https://console.example"});err!=nil{t.Fatal(err)}
	h:=s.Handler()
	call:=func(method,path,body string,cookie *http.Cookie,csrf string)*httptest.ResponseRecorder{
		req:=httptest.NewRequest(method,path,strings.NewReader(body))
		req.Host="console.example"
		req.Header.Set("Origin","https://console.example")
		if cookie!=nil{req.AddCookie(cookie)}
        if body!=""{req.Header.Set("Content-Type","application/json")}
		if csrf!=""{req.Header.Set("X-CSRF-Token",csrf)}
		w:=httptest.NewRecorder()
		h.ServeHTTP(w,req)
		return w
	}
	if w:=call("GET","/v1/nodes","",nil,"");w.Code!=401{t.Fatalf("unauthorized read: %d",w.Code)}
	login:=call("POST","/ui/session",`{"passphrase":"separate-owner-console-password"}`,nil,"")
	if login.Code!=200||len(login.Result().Cookies())!=1{
		t.Fatalf("login failed: %d %s",login.Code,login.Body.String())
	}
	cookie:=login.Result().Cookies()[0]
	if !cookie.HttpOnly||!cookie.Secure||cookie.SameSite!=http.SameSiteStrictMode{
		t.Fatal("insecure session cookie")
	}
	if w:=call("GET","/v1/nodes","",cookie,"");w.Code!=200{
		t.Fatalf("read-only inventory rejected: %d %s",w.Code,w.Body.String())
	}
	if w:=call("POST","/v1/tasks",`{"title":"unauthorized edit"}`,cookie,"");w.Code!=401{
		t.Fatalf("browser session changed tasks: %d",w.Code)
	}
	if w:=call("POST","/mcp",`{"jsonrpc":"2.0","id":1,"method":"tools/list"}`,cookie,"");w.Code!=401{
		t.Fatalf("browser session reached MCP: %d",w.Code)
	}
	status:=call("GET","/ui/session","",cookie,"")
	var result struct {
		Authenticated bool `json:"authenticated"`
		CSRF string `json:"csrf_token"`
	}
	if err:=json.Unmarshal(status.Body.Bytes(),&result);err!=nil{t.Fatal(err)}
	if !result.Authenticated||result.CSRF==""{t.Fatal("session status missing CSRF token")}
	created:=call("POST","/v1/tasks",`{"title":"Authorized task","minutes":90}`,cookie,result.CSRF)
 if created.Code!=201{t.Fatalf("owner task rejected: %d %s",created.Code,created.Body.String())}
 var parent store.Task
 if err:=json.Unmarshal(created.Body.Bytes(),&parent);err!=nil{t.Fatal(err)}
 model:=&taskPreviewModel{output:json.RawMessage(`{"steps":[
  {"title":"Define the task","minutes":30},
  {"title":"Verify the result","minutes":45,"depends_on_steps":[0]}
 ]}`)}
 s.SetCapabilityPlanner(model)
 previewPath:="/v1/tasks/"+parent.ID+"/decomposition-preview"
 if w:=call("POST",previewPath,`{"expected_revision":1,"share_title_with_model":true}`,cookie,"");w.Code!=401{
  t.Fatalf("task model disclosure bypassed CSRF: %d",w.Code)
 }
 if model.calls!=0{t.Fatal("model was contacted before owner session authorization")}
 if w:=call("POST",previewPath,`{"expected_revision":1,"share_title_with_model":false}`,cookie,result.CSRF);w.Code!=422{
  t.Fatalf("owner disclosure consent was not required: %d",w.Code)
 }
 preview:=call("POST",previewPath,`{"expected_revision":1,"share_title_with_model":true}`,cookie,result.CSRF)
 if preview.Code!=200||model.calls!=1{
  t.Fatalf("consented model preview unavailable: %d %s",preview.Code,preview.Body.String())
 }
 if w:=call("POST","/v1/tasks/"+parent.ID+"/decompose",
  `{"expected_revision":1,"steps":[{"title":"A","minutes":30},{"title":"B","minutes":45,"depends_on_steps":[0]}]}`,cookie,"");w.Code!=401{
  t.Fatalf("task decomposition bypassed CSRF: %d",w.Code)
 }
 decomposition:=call("POST","/v1/tasks/"+parent.ID+"/decompose",
  `{"expected_revision":1,"steps":[{"title":"A","minutes":30},{"title":"B","minutes":45,"depends_on_steps":[0]}]}`,cookie,result.CSRF)
 if decomposition.Code!=201{
  t.Fatalf("owner-reviewed decomposition failed: %d %s",decomposition.Code,decomposition.Body.String())
 }
 if len(db.Tasks(""))!=3{t.Fatal("browser flow did not persist the complete parent and subtask graph")}
 if w:=call("POST","/mcp",`{"jsonrpc":"2.0","id":1,"method":"tools/list"}`,cookie,result.CSRF);w.Code!=401{t.Fatalf("session reached MCP: %d",w.Code)}
 if w:=call("DELETE","/ui/session","",cookie,"wrong");w.Code!=403{
		t.Fatalf("CSRF bypassed: %d",w.Code)
	}
	if w:=call("DELETE","/ui/session","",cookie,result.CSRF);w.Code!=204{
		t.Fatalf("logout failed: %d",w.Code)
	}
	if w:=call("GET","/v1/nodes","",cookie,"");w.Code!=401{t.Fatal("logged-out session accepted")}
}

func TestSessionManagerNotConfiguredDoesNotPermitBrowserAccess(t *testing.T) {
	db,err:=store.Open(filepath.Join(t.TempDir(),"state.json"),"UTC")
	if err!=nil{t.Fatal(err)}
	s,err:=New(db,strings.Repeat("g",40),nil)
	if err!=nil{t.Fatal(err)}
	w:=request(s.Handler(),"GET","/ui/session","","")
	if w.Code!=503{t.Fatalf("missing owner verifier accepted: %d",w.Code)}
}


func TestOwnerEngineeringReadAllowlistDoesNotPermitExecutions(t *testing.T) {
    db, err := store.Open(filepath.Join(t.TempDir(),"state.json"),"UTC")
    if err != nil { t.Fatal(err) }
    service, err := New(db,strings.Repeat("t",40),nil)
    if err != nil { t.Fatal(err) }
    secret, err := security.HashPassphrase([]byte("owner-read-only-workspace-passphrase"))
    if err != nil { t.Fatal(err) }
    sessions, err := session.New(secret)
    if err != nil { t.Fatal(err) }
    service.SetSessions(sessions)
    token, csrf, _, err := sessions.Login([]byte("owner-read-only-workspace-passphrase"))
    if err != nil { t.Fatal(err) }
    _ = csrf
    allowed := []string{
        "/v1/engineering/projects",
        "/v1/engineering/nodes",
        "/v1/engineering/projects/project-a/jobs",
        "/v1/engineering/projects/project-a/workspaces",
        "/v1/engineering/projects/project-a/artifacts",
        "/v1/engineering/projects/project-a/context",
        "/v1/engineering/projects/project-a/repositories",
        "/v1/engineering/projects/project-a/research",
        "/v1/engineering/projects/project-a/research-sources",
    }
    for _, path := range allowed {
        req := httptest.NewRequest("GET",path,nil)
        req.AddCookie(&http.Cookie{Name:uiCookie,Value:token})
        if !service.sessionReadAllowed(req) {
            t.Errorf("legitimate engineering inspection was denied: %s",path)
        }
    }
    for _, path := range []string{
        "/v1/engineering/runs",
        "/v1/engineering/projects/project-a/jobs/other",
        "/v1/engineering/projects/project-a/resource-plan",
        "/v1/engineering/projects/project-a/../../../cluster",
        "/v1/engineering/projects/project-a/something",
    } {
        req := httptest.NewRequest("GET",path,nil)
        req.AddCookie(&http.Cookie{Name:uiCookie,Value:token})
        if service.sessionReadAllowed(req) {
            t.Errorf("browser session gained unauthorized engineering read: %s",path)
        }
    }
    req := httptest.NewRequest("POST","/v1/engineering/runs",
        strings.NewReader(`{"objective":"execute"}`))
    req.AddCookie(&http.Cookie{Name:uiCookie,Value:token})
    req.Header.Set("Origin","https://console.example")
    req.Header.Set("Content-Type","application/json")
    req.Header.Set("X-CSRF-Token",csrf)
    if service.sessionOwnerWriteAllowed(req) {
        t.Fatal("owner browser session gained engineering execution privileges")
    }
}

func TestOwnerCurriculumSourceRoutesRequireExactCSRFAndPath(t *testing.T) {
 db, err := store.Open(filepath.Join(t.TempDir(),"state.json"),"UTC")
 if err!=nil{t.Fatal(err)}
 service,err:=New(db,strings.Repeat("o",40),nil)
 if err!=nil{t.Fatal(err)}
 passphrase:=[]byte("separate-owner-learning-password")
 secret,err:=security.HashPassphrase(passphrase)
 if err!=nil{t.Fatal(err)}
 sessions,err:=session.New(secret)
 if err!=nil{t.Fatal(err)}
 service.SetSessions(sessions)
 token,csrf,_,err:=sessions.Login(passphrase)
 if err!=nil{t.Fatal(err)}
 allowed:=[]string{
  "/v1/learning/sources/source_1/curriculum/preview",
  "/v1/learning/sources/source_1/curriculum/import",
 }
 check:=func(method,path,origin,csrfToken,mimeType string)bool{
  req:=httptest.NewRequest(method,path,strings.NewReader(`{"limit":80}`))
  req.AddCookie(&http.Cookie{Name:uiCookie,Value:token})
  req.Header.Set("Origin",origin)
  req.Header.Set("Content-Type",mimeType)
  req.Header.Set("X-CSRF-Token",csrfToken)
  return service.sessionOwnerWriteAllowed(req)
 }
 for _,path:=range allowed{
  if !check("POST",path,"https://console.example",csrf,"application/json"){
   t.Errorf("Authenticated curriculum route rejected: %s",path)
  }
  if check("POST",path,"https://console.example","","application/json"){
   t.Errorf("Curriculum route allowed without CSRF: %s",path)
  }
  if check("POST",path,"",csrf,"application/json"){
   t.Errorf("Curriculum route allowed without Origin: %s",path)
  }
  if check("POST",path,"https://console.example",csrf,"text/plain"){
   t.Errorf("Curriculum route accepted wrong content type: %s",path)
  }
 }
 for _,path:=range []string{
  "/v1/learning/sources/source_1/curriculum/delete",
  "/v1/learning/sources/source_1/curriculum/import/more",
  "/v1/learning/sources/invalid.id/curriculum/import",
  "/v1/learning/sources/source_1/assessment/grade",
  "/v1/learning/sources/source_1/curriculum",
 }{
  if check("POST",path,"https://console.example",csrf,"application/json"){
   t.Errorf("Owner browser session admitted an unauthorized Learning route: %s",path)
  }
 }
}

func TestOwnerComputeMutationsAreExactCSRFBoundOperations(t *testing.T) {
 db,err:=store.Open(filepath.Join(t.TempDir(),"state.json"),"UTC")
 if err!=nil{t.Fatal(err)}
 service,err:=New(db,strings.Repeat("c",40),nil)
 if err!=nil{t.Fatal(err)}
 password:=[]byte("independent-owner-compute-passphrase")
 secret,err:=security.HashPassphrase(password)
 if err!=nil{t.Fatal(err)}
 sessions,err:=session.New(secret)
 if err!=nil{t.Fatal(err)}
 service.SetSessions(sessions)
 cookie,csrf,_,err:=sessions.Login(password)
 if err!=nil{t.Fatal(err)}
 request:=func(method,path,origin,csrfToken string)bool{
  req:=httptest.NewRequest(method,path,strings.NewReader("{}"))
  req.AddCookie(&http.Cookie{Name:uiCookie,Value:cookie})
  req.Header.Set("Origin",origin)
  req.Header.Set("Content-Type","application/json")
  req.Header.Set("X-CSRF-Token",csrfToken)
  return service.sessionOwnerWriteAllowed(req)
 }
 allowed:=[]string{
  "/v1/compute/workspaces/plan",
  "/v1/compute/workspaces",
  "/v1/compute/workspaces/0123456789abcdef0123456789abcdef/stop",
  "/v1/compute/catalog/register-image",
  "/v1/compute/catalog/register-software",
  "/v1/compute/catalog/propose-program",
  "/v1/compute/catalog/review-program",
  "/v1/compute/catalog/update-entry",
 }
 for _,path:=range allowed{
  if !request("POST",path,"https://console.example",csrf){
   t.Errorf("Legitimate owner Compute route denied: %s",path)
  }
  if request("POST",path,"https://console.example",""){
   t.Errorf("Compute write accepted without CSRF: %s",path)
  }
  if request("POST",path,"",csrf){
   t.Errorf("Compute write accepted without Origin: %s",path)
  }
 }
 denied:=[]string{
  "/v1/compute/catalog/delete",
  "/v1/compute/catalog/update-entry/fallback",
  "/v1/compute/workspaces/anything/exec",
  "/v1/compute/workspaces/anything/stop/extra",
  "/v1/compute/workers/jupiter/revoke",
  "/v1/compute/credentials",
  "/mcp",
 }
 for _,path:=range denied{
  if request("POST",path,"https://console.example",csrf){
   t.Errorf("Browser Compute scope admitted privileged path: %s",path)
  }
 }
}
