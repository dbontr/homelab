package mcp

import (
    "bytes"
    "crypto/sha256"
    "encoding/hex"
    "errors"
    "io/fs"
    "mime"
    "net/http"
    "net/url"
    "os"
    "path"
    "path/filepath"
    "regexp"
    "strings"
    "time"
)

// AppBundle contains only public Rust/WASM code and styles, pinned at startup.
// Private Health data and service credentials are never added to this store.
type AppBundle struct {
    origin string
    pages map[string]string
    assets map[string][]byte
}

var assetOriginHost = regexp.MustCompile("^[A-Za-z0-9.-]+$")
var staticExtensions = map[string]bool{
    ".js":true, ".mjs":true, ".wasm":true, ".css":true,
    ".svg":true, ".png":true, ".jpg":true, ".jpeg":true,
    ".webp":true, ".ico":true, ".woff":true, ".woff2":true,
}
var healthAppViews = map[string]string{
    "cognitive":"health-cognitive.html",
    "dashboard":"health-dashboard.html",
    "workout-editor":"health-workout-editor.html",
    "groceries":"health-groceries.html",
    "workout-companion":"health-workout-companion.html",
}

func validateAppOrigin(raw string) error {
    parsed, err := url.Parse(raw)
    if err != nil || parsed.Scheme != "https" || parsed.Hostname() == "" ||
        !assetOriginHost.MatchString(parsed.Hostname()) || parsed.User != nil ||
        parsed.Path != "" || parsed.RawPath != "" || parsed.RawQuery != "" ||
        parsed.Fragment != "" || parsed.Opaque != "" || parsed.String() != raw ||
        strings.ContainsAny(raw, "\"'<> \r\n\t") {
        return errors.New("Health App assets require one exact HTTPS origin")
    }
    return nil
}

func validAssetName(name string) bool {
    if name == "" || len(name) > 240 || name == "." ||
        path.Clean(name) != name || strings.HasPrefix(name, "/") ||
        strings.ContainsAny(name, "\\\x00\r\n") {
        return false
    }
    for _, component := range strings.Split(name, "/") {
        if component == "." || component == ".." || component == "" ||
            strings.HasPrefix(component, ".") {
            return false
        }
    }
    return true
}

// LoadAppBundle checks every release artifact against the generated manifest
// and loads an immutable copy. An altered disk file cannot change the active
// browser code without an explicit service restart and revalidation.
func LoadAppBundle(directory, origin string) (*AppBundle, error) {
    if err := validateAppOrigin(origin); err != nil { return nil, err }
    if !filepath.IsAbs(directory) || filepath.Clean(directory) != directory {
        return nil, errors.New("Health App directory must be canonical and absolute")
    }
    real, err := filepath.EvalSymlinks(directory)
    if err != nil || real != directory {
        return nil, errors.New("Health App directory cannot be a symbolic link")
    }
    stat, err := os.Lstat(directory)
    if err != nil || !stat.IsDir() || stat.Mode().Perm()&0022 != 0 {
        return nil, errors.New("Health App directory is missing or writable by others")
    }
    manifestStat, err := os.Lstat(filepath.Join(directory, "SHA256SUMS"))
    if err != nil || !manifestStat.Mode().IsRegular() ||
        manifestStat.Size() < 64 || manifestStat.Size() > 256*1024 {
        return nil, errors.New("Health App SHA-256 manifest is missing or oversized")
    }
    manifest, err := os.ReadFile(filepath.Join(directory, "SHA256SUMS"))
    if err != nil { return nil, err }
    declared := map[string]string{}
    for _, line := range strings.Split(strings.TrimSuffix(string(manifest), "\n"), "\n") {
        fields := strings.SplitN(line, "  ./", 2)
        if len(fields) != 2 || len(fields[0]) != 64 {
            return nil, errors.New("invalid Health App manifest line")
        }
        if _, err := hex.DecodeString(fields[0]); err != nil ||
            strings.ToLower(fields[0]) != fields[0] ||
            !validAssetName(fields[1]) || declared[fields[1]] != "" {
            return nil, errors.New("invalid or duplicate Health App artifact")
        }
        declared[fields[1]] = fields[0]
    }
    if len(declared) < len(healthAppViews)+2 || len(declared) > 400 {
        return nil, errors.New("unexpected Health App bundle content count")
    }
    names := map[string]bool{}
    pages := map[string]string{}
    assets := map[string][]byte{}
    var total int64
    err = fs.WalkDir(os.DirFS(directory), ".", func(name string, entry fs.DirEntry, walkErr error) error {
        if walkErr != nil { return walkErr }
        if name == "." { return nil }
        if !validAssetName(name) || entry.Type()&os.ModeSymlink != 0 {
            return errors.New("invalid or linked bundle entry")
        }
        if entry.IsDir() { return nil }
        info, err := entry.Info()
        if err != nil || !info.Mode().IsRegular() {
            return errors.New("Health App bundle contains a special file")
        }
        if name == "SHA256SUMS" { return nil }
        digest, exists := declared[name]
        if !exists || info.Size() <= 0 || info.Size() > 64<<20 {
            return errors.New("Health App file is unmanifested, empty or oversized")
        }
        total += info.Size()
        if total > 96<<20 { return errors.New("Health App bundle is too large") }
        data, err := os.ReadFile(filepath.Join(directory, filepath.FromSlash(name)))
        if err != nil || int64(len(data)) != info.Size() {
            return errors.New("Health App asset changed during inspection")
        }
        sum := sha256.Sum256(data)
        if hex.EncodeToString(sum[:]) != digest {
            return errors.New("Health App asset checksum mismatch")
        }
        names[name] = true
        if name == "index.html" { return nil }
        // The shared WebAssembly distribution also carries Learning's
        // independently hosted entrypoint. Verify its bytes and marker but
        // do not expose its resource through a Health MCP tool.
        if name == "learning.html" {
            marker := `<meta name="homelab-mcp-view" content="learning">`
            if len(data)>1<<20 || !strings.Contains(string(data),marker) ||
                !strings.Contains(string(data),"<head>") {
                return errors.New("Learning App entrypoint marker is missing")
            }
            return nil
        }
        for view, expected := range healthAppViews {
            if name == expected {
                marker := "<meta name=\"homelab-mcp-view\" content=\"" + view + "\">"
                if !strings.Contains(string(data), marker) ||
                    !strings.Contains(string(data), "<head>") || len(data) > 1<<20 {
                    return errors.New("Health App entrypoint marker is missing")
                }
                pages[view] = string(data)
                return nil
            }
        }
        if !staticExtensions[strings.ToLower(filepath.Ext(name))] {
            return errors.New("Health App includes an unapproved public asset")
        }
        assets[name] = data
        return nil
    })
    if err != nil { return nil, err }
    if len(names) != len(declared) || len(pages) != len(healthAppViews) ||
        len(assets) == 0 || !names["index.html"] {
        return nil, errors.New("Health App release has missing or undeclared files")
    }
    return &AppBundle{origin:origin,pages:pages,assets:assets}, nil
}

func (b *AppBundle) view(name string) string {
    html := b.pages[name]
    if html == "" { return "" }
    // Dioxus emits root-relative script and asset URLs. Sandboxed srcdoc
    // documents have a different origin, so the verified resource host must
    // be part of those URLs. Relative imports still use the declared base URI.
    root := b.origin + "/mcp-assets/"
    html = strings.NewReplacer(
        `src="/`, `src="`+root,
        `href="/`, `href="`+root,
    ).Replace(html)
    return strings.Replace(html, "<head>",
        `<head><base href="`+root+`">`, 1)
}

func (b *AppBundle) serve(w http.ResponseWriter, r *http.Request) {
    if r.Method != http.MethodGet && r.Method != http.MethodHead {
        w.Header().Set("Allow","GET, HEAD")
        http.Error(w,"method not allowed",http.StatusMethodNotAllowed)
        return
    }
    name := strings.TrimPrefix(r.URL.Path,"/")
    name = strings.TrimPrefix(name,"mcp-assets/")
    if !validAssetName(name) { http.NotFound(w,r); return }
    data, ok := b.assets[name]
    if !ok { http.NotFound(w,r); return }
    kind := mime.TypeByExtension(path.Ext(name))
    if strings.HasSuffix(name,".wasm") { kind = "application/wasm" }
    if kind == "" { kind = "application/octet-stream" }
    w.Header().Set("Content-Type",kind)
    w.Header().Set("X-Content-Type-Options","nosniff")
    w.Header().Set("Access-Control-Allow-Origin","*")
    w.Header().Set("Cross-Origin-Resource-Policy","cross-origin")
    w.Header().Set("Cache-Control","public, max-age=3600")
    http.ServeContent(w,r,path.Base(name),time.Time{},bytes.NewReader(data))
}


// SetAppBundle selects a verified release before serving any requests.
// Native Rust assets are opt-in; legacy self-contained MCP resources remain
// available while browser and protocol acceptance is incomplete.
func (s *Server) SetAppBundle(bundle *AppBundle) error {
    if bundle == nil || bundle.origin == "" || len(bundle.pages) != len(healthAppViews) {
        return errors.New("Health App bundle is incomplete")
    }
    tools := []string{
        "health_cognitive_test", "health_dashboard",
        "health_workout_editor", "health_grocery_editor",
        "health_workout_companion",
    }
    for _, name := range tools {
        entry, exists := s.tools[name]
        if !exists || entry.Meta == nil {
            return errors.New("Health App has no declared MCP tool resource")
        }
        if _, ok := entry.Meta["ui"].(map[string]any); !ok {
            return errors.New("Health App has no UI metadata")
        }
    }
    for _, name := range tools {
        entry := s.tools[name]
        ui := entry.Meta["ui"].(map[string]any)
        ui["csp"] = map[string]any{
            "resourceDomains": []string{bundle.origin},
            "connectDomains": []string{bundle.origin},
            "baseUriDomains": []string{bundle.origin},
        }
        entry.Meta["ui"] = ui
        s.tools[name] = entry
    }
    s.bundle = bundle
    return nil
}

func (s *Server) appHTML(view, fallback string) string {
    if s.bundle == nil { return fallback }
    return s.bundle.view(view)
}

// Resource-level CSP is mandatory for host rendering. Tool metadata is
// advisory discovery data; it does not authorize remote assets by itself.
func (s *Server) appResource(uri, view, fallback string) map[string]any {
    resource := map[string]any{
        "uri": uri,
        "mimeType": "text/html;profile=mcp-app",
        "text": s.appHTML(view, fallback),
    }
    if s.bundle != nil {
        resource["_meta"] = map[string]any{
            "ui": map[string]any{
                "csp": map[string]any{
                    "resourceDomains": []string{s.bundle.origin},
                    "connectDomains": []string{s.bundle.origin},
                    "baseUriDomains": []string{s.bundle.origin},
                },
            },
        }
    }
    return resource
}
