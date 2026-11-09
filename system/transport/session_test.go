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
