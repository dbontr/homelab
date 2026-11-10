package transport

import (
	"net/http"
	"net/http/httptest"
	"encoding/json"
	"path/filepath"

	"github.com/dbontr/homelab-new/system/security"
	"github.com/dbontr/homelab-new/system/session"
	"github.com/dbontr/homelab-new/system/store"
	"strings"
	"testing"
)

func TestOwnerHealthToolPathsAreExactAndNarrow(t *testing.T) {
	for name := range ownerHealthAppTools {
		path := "/v1/health/app-tools/" + name + "/call"
		if !ownerHealthAppToolPathAllowed(path) {
			t.Fatalf("approved Health operation not available: %s", path)
		}
	}
	for _, path := range []string{
		"/v1/tools/health__health_goal_set/call",
		"/v1/health/app-tools/health_record_add/call",
		"/v1/health/app-tools/health_goal_set/call/extra",
		"/v1/health/app-tools/health_goal_set/call/",
		"/v1/health/app-tools/health_goal_set//call",
		"/v1/health/app-tools/security_session_open/call",
		"/v1/health/app-tools/health_goal_set%2Fsecurity_session_open/call",
		"/v1/health/app-tools/health_goal_set",
		"/v1/health/app-tools/../health_goal_set/call",
	} {
		if ownerHealthAppToolPathAllowed(path) {
			t.Errorf("unexpected browser tool authorization: %q", path)
		}
	}
}

func TestOwnerHealthToolHandlerRejectsUnapprovedNames(t *testing.T) {
	server := &Server{}
	for _, name := range []string{"health_record_add", "security_session_open", "health_goal_set/other"} {
		req := httptest.NewRequest(http.MethodPost, "/v1/health/app-tools/"+name+"/call",
			strings.NewReader(`{"arguments":{}}`))
		req.SetPathValue("name", name)
		rec := httptest.NewRecorder()
		server.uiHealthAppToolCall(rec, req)
		if rec.Code != http.StatusNotFound {
			t.Fatalf("%q returned %d, want 404", name, rec.Code)
		}
	}
}


func TestOwnerHealthBrowserToolsRequireOriginSessionAndCSRF(t *testing.T) {
    db, err := store.Open(filepath.Join(t.TempDir(),"state.json"),"UTC")
    if err != nil { t.Fatal(err) }
    service, err := New(db,strings.Repeat("a",40),nil)
    if err != nil { t.Fatal(err) }
    password := "health-owner-only-passphrase"
    secret, err := security.HashPassphrase([]byte(password))
    if err != nil { t.Fatal(err) }
    sessions, err := session.New(secret)
    if err != nil { t.Fatal(err) }
    service.SetSessions(sessions)
    if err := service.SetAllowedOrigins([]string{"https://console.example"}); err != nil {
        t.Fatal(err)
    }
    handler := service.Handler()
    call := func(method, path, body, origin string, cookie *http.Cookie, csrf string) *httptest.ResponseRecorder {
        req := httptest.NewRequest(method, path, strings.NewReader(body))
        req.Host = "console.example"
        if origin != "" { req.Header.Set("Origin",origin) }
        if body != "" { req.Header.Set("Content-Type","application/json") }
        if cookie != nil { req.AddCookie(cookie) }
        if csrf != "" { req.Header.Set("X-CSRF-Token",csrf) }
        rec := httptest.NewRecorder()
        handler.ServeHTTP(rec,req)
        return rec
    }
    operation := "/v1/health/app-tools/health_plan_list/call"
    request := `{"arguments":{}}`
    if got := call("POST",operation,request,"https://console.example",nil,""); got.Code != 401 {
        t.Fatalf("anonymous Health operation accepted: %d",got.Code)
    }
    login := call("POST","/ui/session",`{"passphrase":"health-owner-only-passphrase"}`,"https://console.example",nil,"")
    if login.Code != 200 || len(login.Result().Cookies()) != 1 {
        t.Fatalf("cannot establish owner browser session: %d",login.Code)
    }
    cookie := login.Result().Cookies()[0]
    status := call("GET","/ui/session","","https://console.example",cookie,"")
    var identity struct { CSRF string `json:"csrf_token"` }
    if err := json.Unmarshal(status.Body.Bytes(),&identity); err != nil || identity.CSRF == "" {
        t.Fatalf("no owner CSRF token: %v",err)
    }
    if got := call("POST",operation,request,"https://console.example",cookie,""); got.Code != 401 {
        t.Fatalf("Health route omitted CSRF: %d",got.Code)
    }
    if got := call("POST",operation,request,"https://invalid.example",cookie,identity.CSRF); got.Code != 403 {
        t.Fatalf("Health route ignored allowed origin: %d",got.Code)
    }
    if got := call("POST",operation,request,"https://console.example",cookie,identity.CSRF); got.Code != 502 {
        t.Fatalf("allowlisted Health backend call did not reach unavailable backend: %d %s",got.Code,got.Body.String())
    }
    for _, path := range []string{
        "/v1/tools/health__health_plan_list/call",
        "/v1/health/app-tools/health_record_add/call",
        "/v1/health/app-tools/security_session_open/call",
        "/v1/health/app-tools/health_plan_list/extra/call",
    } {
        got := call("POST",path,request,"https://console.example",cookie,identity.CSRF)
        if got.Code != 401 && got.Code != 404 {
            t.Fatalf("nonallowlisted browser tool %q returned %d",path,got.Code)
        }
    }
    react := call("POST","/v1/health/reaction-test",`{"trials_ms":[250,200,300,350,225]}`,
        "https://console.example",cookie,identity.CSRF)
    if react.Code != 502 {
        t.Fatalf("reaction endpoint not authorized up to missing Health backend: %d",react.Code)
    }
}
