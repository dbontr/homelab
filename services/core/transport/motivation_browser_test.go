package transport

import (
    "encoding/json"
    "net/http"
    "net/http/httptest"
    "path/filepath"
    "strings"
    "testing"

    "github.com/dbontr/homelab-new/system/internal/motivationhttp"
    "github.com/dbontr/homelab-new/system/security"
    "github.com/dbontr/homelab-new/system/session"
    "github.com/dbontr/homelab-new/system/store"
)

func TestMotivationBrowserRoutesRequireOwnerSessionAndCSRF(t *testing.T) {
    secret := strings.Repeat("z", 40)
    var calls []string
    backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter,r *http.Request){
        if r.Header.Get("Authorization")!="Bearer "+secret {
            t.Error("motivation upstream credential mismatch")
            w.WriteHeader(http.StatusUnauthorized)
            return
        }
        calls=append(calls,r.Method+" "+r.URL.Path)
        w.Header().Set("Content-Type","application/json")
        _,_=w.Write([]byte(`{"profile":{"revision":0,"enabled":false},"interventions":[]}`))
    }))
    defer backend.Close()
    client,err:=motivationhttp.New(backend.URL,secret)
    if err!=nil { t.Fatal(err) }
    db,err:=store.Open(filepath.Join(t.TempDir(),"state.json"),"UTC")
    if err!=nil {t.Fatal(err)}
    server,err:=New(db,strings.Repeat("a",40),nil)
    if err!=nil {t.Fatal(err)}
    record,err:=security.HashPassphrase([]byte("independent-console-passphrase"))
    if err!=nil {t.Fatal(err)}
    manager,err:=session.New(record)
    if err!=nil {t.Fatal(err)}
    server.SetSessions(manager)
    server.SetMotivation(client)
    if err:=server.SetAllowedOrigins([]string{"https://console.example"});err!=nil {t.Fatal(err)}
    handler:=server.Handler()
    call:=func(method,path,body string,cookie *http.Cookie,csrf string)*httptest.ResponseRecorder{
        req:=httptest.NewRequest(method,path,strings.NewReader(body))
        req.Host="console.example"
        req.Header.Set("Origin","https://console.example")
        if cookie!=nil {req.AddCookie(cookie)}
        if body!="" {req.Header.Set("Content-Type","application/json")}
        if csrf!="" {req.Header.Set("X-CSRF-Token",csrf)}
        writer:=httptest.NewRecorder()
        handler.ServeHTTP(writer,req)
        return writer
    }
    if w:=call("GET","/v1/motivation/state","",nil,"");w.Code!=401 {t.Fatalf("unauthenticated read: %d",w.Code)}
    login:=call("POST","/ui/session",`{"passphrase":"independent-console-passphrase"}`,nil,"")
    if login.Code!=200 {t.Fatalf("login: %d %s",login.Code,login.Body.String())}
    cookie:=login.Result().Cookies()[0]
    state:=call("GET","/ui/session","",cookie,"")
    var sessionState struct { CSRF string `json:"csrf_token"` }
    if err:=json.Unmarshal(state.Body.Bytes(),&sessionState);err!=nil {t.Fatal(err)}
    if sessionState.CSRF=="" {t.Fatal("CSRF missing")}
    for _,path:=range []string{"/v1/motivation/state","/v1/motivation/metrics"} {
        if w:=call("GET",path,"",cookie,"");w.Code!=200 {
            t.Fatalf("authenticated read %s: %d %s",path,w.Code,w.Body.String())
        }
    }
    cases:=[]struct{method,path string}{
        {"PUT","/v1/motivation/profile"},{"POST","/v1/motivation/pause"},
        {"POST","/v1/motivation/interventions"},{"POST","/v1/motivation/feedback"},
        {"POST","/v1/motivation/checkins"},{"POST","/v1/motivation/dismiss"},
        {"POST","/v1/motivation/focus"},{"POST","/v1/motivation/focus/end"},
    }
    for _,item:=range cases {
        if w:=call(item.method,item.path,"{}",cookie,"");w.Code!=401 {
            t.Fatalf("CSRF bypass for %s %s: %d",item.method,item.path,w.Code)
        }
        if w:=call(item.method,item.path,"{}",cookie,sessionState.CSRF);w.Code!=200 {
            t.Fatalf("authorized call %s %s: %d %s",item.method,item.path,w.Code,w.Body.String())
        }
    }
    if len(calls)!=10 {t.Fatalf("expected ten authorized upstream calls, got %v",calls)}
    if w:=call("POST","/mcp",`{"jsonrpc":"2.0","id":1,"method":"tools/list"}`,cookie,sessionState.CSRF);w.Code!=401 {
        t.Fatal("browser session reached MCP")
    }
}
