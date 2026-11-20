package transport

import (
    "encoding/json"
    "net/http"
    "net/http/httptest"
    "path/filepath"
    "strings"
    "testing"

    "github.com/dbontr/homelab-new/system/store"
)

func TestLearningConceptRegistrationUsesVerifiedLearningBackend(t *testing.T) {
    captured := make(chan map[string]any, 2)
    backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        var call rpcRequest
        if err := json.NewDecoder(r.Body).Decode(&call); err != nil {
            t.Error(err)
            w.WriteHeader(http.StatusBadRequest)
            return
        }
        switch call.Method {
        case "tools/list":
            _, _ = w.Write([]byte(`{"jsonrpc":"2.0","id":1,"result":{"tools":[{"name":"add_concept"}]}}`))
        case "tools/call":
            var parameters struct {
                Name string `json:"name"`
                Arguments map[string]any `json:"arguments"`
            }
            if err := json.Unmarshal(call.Params, &parameters); err != nil || parameters.Name != "add_concept" {
                t.Errorf("unexpected Learning operation: %+v: %v", parameters, err)
                w.WriteHeader(http.StatusBadRequest)
                return
            }
            captured <- parameters.Arguments
            w.Header().Set("Content-Type", "application/json")
            _, _ = w.Write([]byte(`{"jsonrpc":"2.0","id":1,"result":{"content":[{"type":"text","text":"{\"id\":\"fourier-transform\",\"mastery_awarded\":false}"}]}}`))
        default:
            t.Errorf("unexpected Learning RPC: %s", call.Method)
            w.WriteHeader(http.StatusBadRequest)
        }
    }))
    defer backend.Close()
    h := makeTestServer(t, []Backend{{Name: "learning", URL: backend.URL, Token: strings.Repeat("s", 32)}})
    token := strings.Repeat("a", 40)
    const path = "/v1/learning/concepts"
    const good = `{"identifier":"fourier-transform","title":"Fourier transform","summary":"Spectral representation of signals","domain":"signals"}`
    if denied := request(h, "POST", path, good, ""); denied.Code != http.StatusUnauthorized {
        t.Fatalf("anonymous concept creation accepted: %d", denied.Code)
    }
    result := request(h, "POST", path, good, token)
    if result.Code != http.StatusCreated || !strings.Contains(result.Body.String(), "fourier-transform") {
        t.Fatalf("concept registration failed: %d %s", result.Code, result.Body.String())
    }
    select {
    case args := <-captured:
        if args["identifier"] != "fourier-transform" || args["title"] != "Fourier transform" ||
            args["summary"] != "Spectral representation of signals" || args["domain"] != "signals" {
            t.Fatalf("Learning concept details changed: %+v", args)
        }
    default:
        t.Fatal("Learning service did not receive the registered concept")
    }
    for _, payload := range []string{
        `{"identifier":"Bad.Identifier","title":"Fourier transform","summary":"Definition"}`,
        `{"identifier":"fourier-transform","title":" ","summary":"Definition"}`,
        `{"identifier":"fourier-transform","title":"Fourier transform","summary":" "}`,
        `{"identifier":"../credentials","title":"Concept","summary":"Definition"}`,
        `{"identifier":"fourier-transform","title":"Fourier transform","summary":"Definition","unsupported":true}`,
    } {
        w := request(h, "POST", path, payload, token)
        if w.Code != http.StatusBadRequest {
            t.Errorf("invalid concept passed: %d %s", w.Code, w.Body.String())
        }
    }
    if len(captured) != 0 {
        t.Fatal("invalid concept reached Learning service")
    }
}

func TestOwnerLearningConceptRegistrationRequiresSignedOriginAndCSRF(t *testing.T) {
    db, err := store.Open(filepath.Join(t.TempDir(), "state.json"), "UTC")
    if err != nil { t.Fatal(err) }
    service, err := New(db, strings.Repeat("t", 40), nil)
    if err != nil { t.Fatal(err) }
    sessions, signed := accessTransportFixture(t)
    service.SetSessions(sessions)
    csrf, verified := sessions.Check(signed)
    if !verified { t.Fatal("invalid signed session") }
    test := func(method, path, origin, csrfToken, mediaType string) bool {
        req := httptest.NewRequest(method, path, strings.NewReader("{}"))
        req.Header.Set("Cf-Access-Jwt-Assertion", signed)
        req.Header.Set("Origin", origin)
        req.Header.Set("X-CSRF-Token", csrfToken)
        req.Header.Set("Content-Type", mediaType)
        return service.sessionOwnerWriteAllowed(req)
    }
    if !test("POST", "/v1/learning/concepts", "https://console.example", csrf, "application/json") {
        t.Fatal("verified owner cannot register a curriculum concept")
    }
    if test("POST", "/v1/learning/concepts", "https://console.example", "", "application/json") ||
        test("POST", "/v1/learning/concepts", "", csrf, "application/json") ||
        test("POST", "/v1/learning/concepts", "https://console.example", csrf, "text/plain") ||
        test("POST", "/v1/learning/concepts/fourier-transform", "https://console.example", csrf, "application/json") ||
        test("DELETE", "/v1/learning/concepts", "https://console.example", csrf, "application/json") {
        t.Fatal("concept route bypassed exact owner write policy")
    }
}
