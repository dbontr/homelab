package mcp

import (
    "crypto/sha256"
    "fmt"
    "net/http"
    "net/http/httptest"
    "os"
    "path/filepath"
    "sort"
    "strings"
    "testing"
)

func sourceBundle(t *testing.T) string {
    t.Helper()
    root := t.TempDir()
    artifacts := map[string][]byte{
        "index.html": []byte("<!doctype html><html><head></head><body>app</body></html>"),
        "assets/client.js": []byte("export const app = true;"),
        "assets/client.wasm": []byte{0, 97, 115, 109, 1, 0, 0, 0},
        "assets/main.css": []byte("body { color: CanvasText; }"),
    }
    for view, filename := range healthAppViews {
        artifacts[filename] = []byte(
            "<!doctype html><html><head><meta name=\"homelab-mcp-view\" content=\""+
            view+"\"></head><body>app</body></html>")
    }
    names := make([]string, 0, len(artifacts))
    for name := range artifacts { names = append(names, name) }
    sort.Strings(names)
    var manifest strings.Builder
    for _, name := range names {
        file := filepath.Join(root, filepath.FromSlash(name))
        if err := os.MkdirAll(filepath.Dir(file), 0700); err != nil { t.Fatal(err) }
        if err := os.WriteFile(file, artifacts[name], 0600); err != nil { t.Fatal(err) }
        digest := sha256.Sum256(artifacts[name])
        fmt.Fprintf(&manifest, "%x  ./%s\n", digest, name)
    }
    if err := os.WriteFile(filepath.Join(root,"SHA256SUMS"),
        []byte(manifest.String()), 0600); err != nil { t.Fatal(err) }
    return root
}

func TestRustAppBundleHasExactPublicAssetsAndViewIdentity(t *testing.T) {
    root := sourceBundle(t)
    bundle, err := LoadAppBundle(root, "https://assets.example.test")
    if err != nil { t.Fatal(err) }
    if len(bundle.pages) != 5 { t.Fatalf("missing Rust views: %d",len(bundle.pages)) }
    // Root-relative Dioxus boot scripts must resolve to the declared HTTPS
    // asset origin rather than the sandbox host's origin.
    bundle.pages["cognitive"] = strings.Replace(bundle.pages["cognitive"],
        "</head>", "<script src=\"/wasm/client.js\"></script><link href=\"/assets/style.css\" rel=\"stylesheet\"></head>",1)
    rendered := bundle.view("cognitive")
    for _, target := range []string{
        "src=\"https://assets.example.test/mcp-assets/wasm/client.js\"",
        "href=\"https://assets.example.test/mcp-assets/assets/style.css\"",
    } {
        if !strings.Contains(rendered,target) {
            t.Fatalf("Dioxus asset URL did not use the selected origin: %s", target)
        }
    }
    for view := range healthAppViews {
        html := bundle.view(view)
        if !strings.Contains(html,"<base href=\"https://assets.example.test/mcp-assets/\">") ||
            !strings.Contains(html,"content=\""+view+"\"") {
            t.Fatalf("incorrect view identity: %s",view)
        }
    }
    for _, test := range []struct { path string; status int; contentType string }{
        {"/assets/client.wasm",200,"application/wasm"},
        {"/mcp-assets/assets/client.wasm",200,"application/wasm"},
        {"/mcp-assets/assets/client.js",200,"javascript"},
        {"/mcp-assets/health-cognitive.html",404,""},
        {"/mcp-assets/SHA256SUMS",404,""},
        {"/.env",404,""},
        {"/assets/../index.html",404,""},
    } {
        rec := httptest.NewRecorder()
        bundle.serve(rec,httptest.NewRequest(http.MethodGet,test.path,nil))
        if rec.Code != test.status { t.Fatalf("%s: expected %d got %d",test.path,test.status,rec.Code) }
        if test.contentType != "" && !strings.Contains(rec.Header().Get("Content-Type"),test.contentType) {
            t.Fatalf("%s: MIME type %q",test.path,rec.Header().Get("Content-Type"))
        }
    }
    rec := httptest.NewRecorder()
    bundle.serve(rec,httptest.NewRequest(http.MethodPost,"/assets/client.wasm",nil))
    if rec.Code != http.StatusMethodNotAllowed { t.Fatal("public assets accepted a write") }
    if err := (&Server{tools:map[string]toolEntry{}}).SetAppBundle(bundle); err == nil {
        t.Fatal("bundle selected without registered MCP resources")
    }
}

func TestRustAppBundleRejectsTampering(t *testing.T) {
    root := sourceBundle(t)
    target := filepath.Join(root,"assets","client.js")
    if err := os.WriteFile(target,[]byte("malicious code"),0600); err != nil { t.Fatal(err) }
    if _, err := LoadAppBundle(root,"https://assets.example.test"); err == nil {
        t.Fatal("modified JavaScript was accepted without its exact digest")
    }
}

func TestRustAppBundleRejectsUndeclaredOrLinkedFiles(t *testing.T) {
    root := sourceBundle(t)
    if err := os.WriteFile(filepath.Join(root,"assets","surprise.js"),[]byte("x"),0600); err != nil {
        t.Fatal(err)
    }
    if _, err := LoadAppBundle(root,"https://assets.example.test"); err == nil {
        t.Fatal("unmanifested public asset accepted")
    }
    if err := os.Remove(filepath.Join(root,"assets","surprise.js")); err != nil { t.Fatal(err) }
    if err := os.Symlink(filepath.Join(root,"index.html"),filepath.Join(root,"assets","linked.js")); err != nil {
        t.Fatal(err)
    }
    if _, err := LoadAppBundle(root,"https://assets.example.test"); err == nil {
        t.Fatal("symbolic asset substitution accepted")
    }
}

func TestRustAppBundleRequiresExactHTTPSOriginAndSafeNames(t *testing.T) {
    for _, input := range []string{
        "", "http://example.test", "https://example.test/path",
        "https://user@example.test", "https://example.test?cache=1",
        "https://example.test/", "https://example.test\" onclick=\"alert(1)",
        "https://example.test/#section",
    } {
        if validateAppOrigin(input) == nil { t.Errorf("accepted invalid asset origin %q",input) }
    }
    if err := validateAppOrigin("https://apps.example.test"); err != nil { t.Fatal(err) }
    for _, name := range []string{"../etc/passwd","/absolute","assets//client.js",".env","assets/.secret",""} {
        if validAssetName(name) { t.Errorf("accepted unsafe public asset %q",name) }
    }
}


func TestRustAppBundlePreservesMCPAuthorityAndOriginalResourceIdentity(t *testing.T) {
    root := sourceBundle(t)
    bundle, err := LoadAppBundle(root,"https://assets.example.test")
    if err != nil { t.Fatal(err) }
    tools := map[string]toolEntry{}
    for _, name := range []string{
        "health_cognitive_test","health_dashboard",
        "health_workout_editor","health_grocery_editor","health_workout_companion",
    } {
        tools[name] = toolEntry{Tool:Tool{Name:name,Meta:map[string]any{
            "ui":map[string]any{"resourceUri":"ui://personal-health-mcp/test.html"},
        }}}
    }
    server := &Server{tools:tools}
    if fallback := server.appHTML("cognitive","legacy view"); fallback != "legacy view" {
        t.Fatal("disabled Rust App selection did not retain the legacy resource")
    }
    if err := server.SetAppBundle(bundle); err != nil { t.Fatal(err) }
    resource := server.appResource(cognitiveUIURI,"cognitive","legacy view")
    metadata, _ := resource["_meta"].(map[string]any)
    config, _ := metadata["ui"].(map[string]any)
    contentPolicy, _ := config["csp"].(map[string]any)
    for _, field := range []string{"resourceDomains","connectDomains","baseUriDomains"} {
        values, _ := contentPolicy[field].([]string)
        if len(values) != 1 || values[0] != bundle.origin {
            t.Fatalf("MCP Apps resource requires exact %s: %#v",field,values)
        }
    }

    if !strings.Contains(server.appHTML("cognitive","legacy view"),"content=\"cognitive\"") {
        t.Fatal("selected Rust App did not preserve its exact view marker")
    }
    for name, entry := range server.tools {
        ui, _ := entry.Meta["ui"].(map[string]any)
        csp, _ := ui["csp"].(map[string]any)
        origins, _ := csp["resourceDomains"].([]string)
        if len(origins) != 1 || origins[0] != "https://assets.example.test" {
            t.Fatalf("%s has missing or overbroad asset CSP",name)
        }
    }
    handler, err := server.HTTPHandler(strings.Repeat("x",32),nil)
    if err != nil { t.Fatal(err) }
    assets := httptest.NewRecorder()
    handler.ServeHTTP(assets,httptest.NewRequest(http.MethodGet,"/assets/client.wasm",nil))
    if assets.Code != http.StatusOK {
        t.Fatalf("public static code unavailable: %d",assets.Code)
    }
    private := httptest.NewRecorder()
    req := httptest.NewRequest(http.MethodPost,"/mcp",strings.NewReader("{}"))
    req.Header.Set("Content-Type","application/json")
    handler.ServeHTTP(private,req)
    if private.Code != http.StatusUnauthorized {
        t.Fatalf("static asset configuration bypassed private MCP authorization: %d",private.Code)
    }
}
