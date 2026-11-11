package mcpsvc

import (
    "crypto/sha256"
    "encoding/hex"
    "fmt"
    "net/http"
    "net/http/httptest"
    "os"
    "path/filepath"
    "sort"
    "strings"
    "testing"
)

func learningBundleFixture(t *testing.T)string{
    t.Helper()
    root:=t.TempDir()
    content:=map[string]string{
        "index.html":"<head></head><body>Shared Dioxus runtime</body>",
        "learning.html":`<head><meta name="homelab-mcp-view" content="learning"><script src="/app.js"></script></head><body>Study</body>`,
        "app.js":"const deployed = true;",
        "app.wasm":"wasm-fixture",
    }
    for _,view:=range []string{"cognitive","dashboard","workout-editor","groceries","workout-companion"}{
        content["health-"+view+".html"]="<head></head><body>Health</body>"
    }
    names:=make([]string,0,len(content))
    for name,data:=range content {
        if err:=os.WriteFile(filepath.Join(root,name),[]byte(data),0600);err!=nil{t.Fatal(err)}
        names=append(names,name)
    }
    sort.Strings(names)
    var manifest strings.Builder
    for _,name:=range names{
        sum:=sha256.Sum256([]byte(content[name]))
        fmt.Fprintf(&manifest,"%s  ./%s\n",hex.EncodeToString(sum[:]),name)
    }
    if err:=os.WriteFile(filepath.Join(root,"SHA256SUMS"),[]byte(manifest.String()),0600);err!=nil{
        t.Fatal(err)
    }
    return root
}

func TestVerifiedLearningAppBundleServesOnlyManifestedPublicBytes(t *testing.T){
    root:=learningBundleFixture(t)
    bundle,err:=LoadLearningAppBundle(root,"https://learning.example.org")
    if err!=nil{t.Fatal(err)}
    if !strings.Contains(bundle.resourceHTML(),`<base href="https://learning.example.org/mcp-assets/">`)||
        !strings.Contains(bundle.resourceHTML(),`src="https://learning.example.org/mcp-assets/app.js"`){
        t.Fatal("Dioxus asset links were not bound to the declared resource origin")
    }
    req:=httptest.NewRequest(http.MethodGet,"https://learning.example.org/mcp-assets/app.js",nil)
    reply:=httptest.NewRecorder()
    bundle.ServeStatic(reply,req)
    if reply.Code!=http.StatusOK||reply.Body.String()!="const deployed = true;"||
        reply.Header().Get("X-Content-Type-Options")!="nosniff"{
        t.Fatalf("verified public asset unavailable: %d %s",reply.Code,reply.Body.String())
    }
    req=httptest.NewRequest(http.MethodPost,"https://learning.example.org/mcp-assets/app.js",nil)
    reply=httptest.NewRecorder()
    bundle.ServeStatic(reply,req)
    if reply.Code!=http.StatusMethodNotAllowed{t.Fatal("public asset write was permitted")}
    req=httptest.NewRequest(http.MethodGet,"https://untrusted.example.org/mcp-assets/app.js",nil)
    reply=httptest.NewRecorder()
    bundle.ServeStatic(reply,req)
    if reply.Code!=http.StatusForbidden{t.Fatal("resource origin substitution was accepted")}
    if _,exists:=bundle.assets["health-dashboard.html"];exists{
        t.Fatal("Health resource was exposed from Learning public asset root")
    }
}

func TestLearningAppBundleRejectsAlteredAndUndeclaredReleaseFiles(t *testing.T){
    for _,modify:=range []func(string)error{
        func(root string)error{
            return os.WriteFile(filepath.Join(root,"app.wasm"),[]byte("tampered"),0600)
        },
        func(root string)error{
            return os.WriteFile(filepath.Join(root,"unauthorized.js"),[]byte("unmanifested"),0600)
        },
        func(root string)error{
            return os.Symlink("app.js",filepath.Join(root,"extra-link.js"))
        },
        func(root string)error{
            return os.WriteFile(filepath.Join(root,"learning.html"),[]byte("<head></head>"),0600)
        },
    }{
        root:=learningBundleFixture(t)
        if err:=modify(root);err!=nil{t.Fatal(err)}
        if _,err:=LoadLearningAppBundle(root,"https://learning.example.org");err==nil{
            t.Fatal("altered Learning App bundle was accepted")
        }
    }
    root:=learningBundleFixture(t)
    for _,origin:=range []string{"http://learning.example.org",
        "https://learning.example.org/private","https://user@learning.example.org",
        "https://learning.example.org?token=abc","https://learning.example.org#fragment"}{
        if _,err:=LoadLearningAppBundle(root,origin);err==nil{
            t.Errorf("unsafe Learning asset origin accepted: %q",origin)
        }
    }
}
