package transport

import (
	"bytes"
	"encoding/json"
	"image"
	"image/png"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dbontr/homelab-new/system/recipes"
	"github.com/dbontr/homelab-new/system/store"
)

func TestRecipeImageHTTPAccessAndContentRestrictions(t *testing.T) {
	state, err := store.Open(filepath.Join(t.TempDir(), "tasks.json"), "UTC")
	if err != nil {
		t.Fatal(err)
	}
	server, err := New(state, strings.Repeat("t", 40), nil)
	if err != nil {
		t.Fatal(err)
	}
	db, err := recipes.Open(filepath.Join(t.TempDir(), "recipes.json"))
	if err != nil {
		t.Fatal(err)
	}
	server.SetRecipes(db)
	manager, accessAssertion := accessTransportFixture(t)
	server.SetSessions(manager)
	if err := server.SetAllowedOrigins([]string{"https://console.example"}); err != nil {
		t.Fatal(err)
	}
	handler := server.Handler()
	call := func(method, path, mediaType string, body []byte, identity *accessTestIdentity, csrf string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, path, bytes.NewReader(body))
		req.Host = "console.example"
		req.Header.Set("Origin", "https://console.example")
		if mediaType != "" {
			req.Header.Set("Content-Type", mediaType)
		}
		if csrf != "" {
			req.Header.Set("X-CSRF-Token", csrf)
		}
		if identity != nil {
			req.Header.Set("Cf-Access-Jwt-Assertion",identity.Value)
		}
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, req)
		return response
	}
	accessToken := &accessTestIdentity{Value: accessAssertion}
	status := call("GET", "/ui/session", "", nil, accessToken, "")
	var sessionData struct {
		CSRF string `json:"csrf_token"`
	}
	if err := json.Unmarshal(status.Body.Bytes(), &sessionData); err != nil || sessionData.CSRF == "" {
		t.Fatalf("missing owner token: %v", err)
	}

	var body bytes.Buffer
	if err := png.Encode(&body, image.NewRGBA(image.Rect(0, 0, 2, 2))); err != nil {
		t.Fatal(err)
	}
	picture := body.Bytes()
	if r := call("POST", "/v1/recipes/images", "image/png", picture, nil, ""); r.Code != 401 {
		t.Fatalf("anonymous upload accepted: %d", r.Code)
	}
	if r := call("POST", "/v1/recipes/images", "image/png", picture, accessToken, ""); r.Code != 401 {
		t.Fatalf("image upload without CSRF accepted: %d", r.Code)
	}
	if r := call("POST", "/v1/recipes/images", "image/svg+xml", picture, accessToken, sessionData.CSRF); r.Code < 400 {
		t.Fatalf("unexpected image type accepted: %d", r.Code)
	}
	if r := call("POST", "/v1/recipes/images", "image/png", []byte("not an image"), accessToken, sessionData.CSRF); r.Code != 422 {
		t.Fatalf("invalid image accepted: %d %s", r.Code, r.Body.String())
	}
	uploaded := call("POST", "/v1/recipes/images", "image/png", picture, accessToken, sessionData.CSRF)
	if uploaded.Code != http.StatusCreated {
		t.Fatalf("image upload: %d %s", uploaded.Code, uploaded.Body.String())
	}
	var asset struct {
		ImageRef string `json:"image_ref"`
	}
	if err := json.Unmarshal(uploaded.Body.Bytes(), &asset); err != nil || !strings.HasPrefix(asset.ImageRef, "/v1/recipes/images/") {
		t.Fatalf("invalid image reference: %v %s", err, uploaded.Body.String())
	}
	if r := call("GET", asset.ImageRef, "", nil, nil, ""); r.Code != 401 {
		t.Fatalf("anonymous image retrieval accepted: %d", r.Code)
	}
	read := call("GET", asset.ImageRef, "", nil, accessToken, "")
	if read.Code != 200 || read.Header().Get("Content-Type") != "image/png" ||
		!bytes.Equal(read.Body.Bytes(), picture) ||
		read.Header().Get("X-Content-Type-Options") != "nosniff" {
		t.Fatalf("authenticated image read failed: %d %s", read.Code, read.Body.String())
	}
	if r := call("POST", "/v1/recipes/images", "image/png", bytes.Repeat([]byte{1}, recipes.MaxImageBytes+1), accessToken, sessionData.CSRF); r.Code != 413 {
		t.Fatalf("oversized image accepted: %d", r.Code)
	}
	if r := call("GET", "/v1/recipes/images/not-an-image.png", "", nil, accessToken, ""); r.Code != 404 {
		t.Fatalf("malformed image path accepted: %d", r.Code)
	}
}
