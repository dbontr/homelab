package agentflow

import (
 "context"
 "io"
 "encoding/json"
 "os"
 "path/filepath"
 "strings"
 "sync/atomic"
 "net/http"
 "net/http/httptest"
 "testing"
)

func TestModelAuthorUsesCurrentCredentialOnly(t *testing.T){
 token:="model-authorized-token-identity-one"
 calls:=0
 upstream:=httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter,r *http.Request){
  calls++
  if r.Method!="POST"||r.Header.Get("Authorization")!="Bearer "+token{
   t.Error("unapproved model credential")
  }
  w.Header().Set("Content-Type","application/json")
  _,_=io.WriteString(w,`{"choices":[{"message":{"content":"{}"},"finish_reason":"stop"}]}`)
 }))
 defer upstream.Close()
 author,err:=NewModelAuthor(upstream.URL+"/v1/chat/completions","model","")
 if err!=nil{t.Fatal(err)}
 author.TokenSource=func()(string,error){return token,nil}
 for _,value:=range []string{"model-authorized-token-identity-one","model-authorized-token-identity-two"}{
  token=value
  text,err:=author.completeJSON(context.Background(),"system","payload")
  if err!=nil||text!="{}"{t.Fatalf("model token failed after rotation: %v",err)}
 }
 if calls!=2{t.Fatal("model did not retrieve credentials for both calls")}
 token="invalid token"
 if _,err:=author.completeJSON(context.Background(),"system","payload");err==nil{
  t.Fatal("malformed credential reached model provider")
 }
 if calls!=2{t.Fatal("invalid model key was transmitted")}
 author.Token="static-provider-token"
 if _,err:=author.completeJSON(context.Background(),"system","payload");err==nil{
  t.Fatal("conflicting model credential authorities accepted")
 }
}

func TestComputeSourceRequiresLiveNonAdminEditorGrant(t *testing.T){
 token:="editor-source-credential-current-value"
 identityChecks:=0
 projectChecks:=0
 upstream:=httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter,r *http.Request){
  if r.Header.Get("Authorization")!="Bearer "+token{t.Error("compute credential was stale")}
  w.Header().Set("Content-Type","application/json")
  switch r.URL.Path{
  case "/api/whoami":
   identityChecks++
   _,_=io.WriteString(w,`{"principal":"engineering-service","administrator":false}`)
  case "/api/projects/test/role":
   projectChecks++
   _,_=io.WriteString(w,`{"project_id":"test","role":"editor"}`)
  default: w.WriteHeader(404)
  }
 }))
 defer upstream.Close()
 compute,err:=NewComputeClientFromSource(upstream.URL+"/mcp",func()(string,error){return token,nil})
 if err!=nil{t.Fatal(err)}
 if err:=compute.VerifyProjectCredential(context.Background());err!=nil{t.Fatal(err)}
 if err:=compute.VerifyProjectEditor(context.Background(),"test");err!=nil{t.Fatal(err)}
 token="editor-source-credential-rotated-value"
 if err:=compute.VerifyProjectCredential(context.Background());err!=nil{t.Fatal(err)}
 if identityChecks!=2||projectChecks!=1{t.Fatal("project editor verification missing")}
 token="invalid token"
 if err:=compute.VerifyProjectCredential(context.Background());err==nil{
  t.Fatal("untrusted compute credential was sent")
 }
}

func TestModelRouteRejectsTwoCredentialSources(t *testing.T){
 _,err:=NewModelRouter([]ModelRoute{{Name:"planner",Purpose:"planning",
  Endpoint:"https://example.test/v1/chat/completions",Model:"bounded",
  Token:"plain-token",TokenSource:func()(string,error){return "second-token",nil}}})
 if err==nil{t.Fatal("duplicate model token sources accepted")}
}

func TestComputeFileClientRechecksEditorGrantAndCredentialRotation(t *testing.T) {
    var secret atomic.Value
    secret.Store(strings.Repeat("a", 40))
    var editor, administrator atomic.Bool
    editor.Store(true)
    var calls atomic.Int32

    server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        if r.Header.Get("Authorization") != "Bearer "+secret.Load().(string) {
            w.WriteHeader(http.StatusUnauthorized)
            return
        }
        w.Header().Set("Content-Type", "application/json")
        switch r.URL.Path {
        case "/api/whoami":
            _ = json.NewEncoder(w).Encode(map[string]any{
                "principal": "oidc:acceptance-editor",
                "administrator": administrator.Load(),
            })
        case "/api/projects/research/role":
            role := "viewer"
            if editor.Load() { role = "editor" }
            _ = json.NewEncoder(w).Encode(map[string]any{"project_id":"research","role":role})
        case "/mcp":
            calls.Add(1)
            _, _ = io.WriteString(w, `{"jsonrpc":"2.0","id":1,"result":{"isError":false,"structuredContent":{"result":[]}}}`)
        default:
            http.NotFound(w, r)
        }
    }))
    defer server.Close()

    tokenFile := filepath.Join(t.TempDir(), "editor-token")
    writeToken := func(token string) {
        t.Helper()
        if err := os.WriteFile(tokenFile, []byte(token), 0600); err != nil {
            t.Fatal(err)
        }
    }
    writeToken(secret.Load().(string))
    client, err := NewComputeClientFromFile(server.URL+"/mcp", tokenFile)
    if err != nil { t.Fatal(err) }
    list := func() error {
        _, err := client.Call(context.Background(), "workspace_list", map[string]any{"project_id":"research"})
        return err
    }
    if err := list(); err != nil || calls.Load() != 1 {
        t.Fatalf("project editor call failed: %v", err)
    }
    editor.Store(false)
    if err := list(); err == nil || calls.Load() != 1 {
        t.Fatal("revoked project editor continued to issue compute RPCs")
    }
    editor.Store(true)
    secret.Store(strings.Repeat("b", 40))
    writeToken(secret.Load().(string))
    if err := list(); err != nil || calls.Load() != 2 {
        t.Fatalf("rotated editor credential was not used: %v", err)
    }
    administrator.Store(true)
    if err := list(); err == nil || calls.Load() != 2 {
        t.Fatal("administrator credential reached the engineering compute tools")
    }
}
