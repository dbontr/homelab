package transport

import (
	"bytes"
	"encoding/json"
	"image"
	"image/png"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dbontr/homelab-new/system/recipes"
	"github.com/dbontr/homelab-new/system/store"
)

func TestRecipeBundleOwnerSessionAndPhotoRoundTrip(t *testing.T) {
	root := t.TempDir()
	tasks, err := store.Open(filepath.Join(root, "tasks.json"), "UTC")
	if err != nil { t.Fatal(err) }
	server, err := New(tasks, strings.Repeat("t", 40), nil)
	if err != nil { t.Fatal(err) }
	source, err := recipes.Open(filepath.Join(root, "source", "recipes.json"))
	if err != nil { t.Fatal(err) }
	server.SetRecipes(source)
	manager, accessAssertion := accessTransportFixture(t)
	server.SetSessions(manager)
	if err := server.SetAllowedOrigins([]string{"https://console.example"}); err != nil { t.Fatal(err) }
	handler := server.Handler()
	call := func(method string, path string, content []byte, mime string, identity *accessTestIdentity, csrf string) *httptest.ResponseRecorder {
		request := httptest.NewRequest(method, path, bytes.NewReader(content))
		request.Host = "console.example"
		request.Header.Set("Origin", "https://console.example")
		if mime != "" { request.Header.Set("Content-Type", mime) }
		if csrf != "" { request.Header.Set("X-CSRF-Token", csrf) }
		if identity != nil { request.Header.Set("Cf-Access-Jwt-Assertion",identity.Value) }
		result := httptest.NewRecorder()
		handler.ServeHTTP(result, request)
		return result
	}
	accessToken := &accessTestIdentity{Value: accessAssertion}
	var identity struct{ CSRF string `json:"csrf_token"` }
	info := call("GET", "/ui/session", nil, "", accessToken, "")
	if err := json.Unmarshal(info.Body.Bytes(), &identity); err != nil || identity.CSRF == "" {
		t.Fatalf("missing session authorization: %v", err)
	}
	var picture bytes.Buffer
	if err := png.Encode(&picture, image.NewRGBA(image.Rect(0, 0, 2, 2))); err != nil { t.Fatal(err) }
	photo, err := source.PutImage(picture.Bytes(), "image/png")
	if err != nil { t.Fatal(err) }
	recipe := recipes.Recipe{
		Title: "Pasta", Servings: 2, ImageRef: photo,
		Ingredients: []recipes.Ingredient{{Name: "pasta", Amount: 2, Unit: "cups"}},
		Steps: []recipes.Step{{Text: "Cook."}},
	}
	if _, err := source.Save(recipe); err != nil { t.Fatal(err) }
	if read := call("GET", "/v1/recipes/bundle", nil, "", nil, ""); read.Code != 401 {
		t.Fatalf("anonymous ZIP download accepted: %d", read.Code)
	}
	exported := call("GET", "/v1/recipes/bundle", nil, "", accessToken, "")
	if exported.Code != 200 || exported.Header().Get("Content-Type") != "application/zip" ||
		exported.Header().Get("Cache-Control") != "no-store" ||
		exported.Header().Get("X-Content-Type-Options") != "nosniff" {
		t.Fatalf("ZIP export failed: %d %s", exported.Code, exported.Body.String())
	}
	bundle := exported.Body.Bytes()
	if read := call("POST", "/v1/recipes/bundle", bundle, "application/zip", nil, ""); read.Code != 401 {
		t.Fatalf("anonymous ZIP import accepted: %d", read.Code)
	}
	if read := call("POST", "/v1/recipes/bundle", bundle, "application/zip", accessToken, ""); read.Code != 401 {
		t.Fatalf("CSRF-free ZIP import accepted: %d", read.Code)
	}
	if read := call("POST", "/v1/recipes/bundle", bundle, "text/plain", accessToken, identity.CSRF); read.Code != 401 {
		t.Fatalf("unexpected ZIP media type accepted: %d", read.Code)
	}
	target, err := recipes.Open(filepath.Join(root, "target", "recipes.json"))
	if err != nil { t.Fatal(err) }
	server.SetRecipes(target)
	imported := call("POST", "/v1/recipes/bundle", bundle, "application/zip", accessToken, identity.CSRF)
	if imported.Code != 201 || !strings.Contains(imported.Body.String(), `"imported":1`) {
		t.Fatalf("ZIP import failed: %d %s", imported.Code, imported.Body.String())
	}
	list := target.List("")
	if len(list) != 1 { t.Fatal("import did not save the recipe") }
	name := strings.TrimPrefix(list[0].ImageRef, "/v1/recipes/images/")
	imageData, _, err := target.GetImage(name)
	if err != nil || !bytes.Equal(imageData, picture.Bytes()) {
		t.Fatalf("ZIP did not contain the original image: %v", err)
	}
	if repeated := call("POST", "/v1/recipes/bundle", bundle, "application/zip", accessToken, identity.CSRF); repeated.Code != 409 {
		t.Fatalf("repeat ZIP import overwrote records: %d", repeated.Code)
	}
}
