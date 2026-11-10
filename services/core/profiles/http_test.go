package profiles

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
)

func requestProfile(h http.Handler, method, path, body string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, path, strings.NewReader(body))
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w
}

func TestProfileHTTPAndToolOperationsShareRevisions(t *testing.T) {
	registry, err := Open(filepath.Join(t.TempDir(), "profiles.json"))
	if err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	Register(mux, registry)

	first := `{"spec":{"id":"builder","title":"Builder","steps":[{"id":"build","command":"make"}],"parameters":{"RAM_GB":"8"}},"expected_version":0}`
	if got := requestProfile(mux, http.MethodPut, "/v1/compute/profiles/builder", first); got.Code != http.StatusOK {
		t.Fatalf("native profile creation: %d %s", got.Code, got.Body.String())
	}
	second := json.RawMessage(`{"spec":{"id":"builder","title":"Builder","steps":[{"id":"build","command":"make"}],"parameters":{"RAM_GB":"4"}},"expected_version":1}`)
	updated, err := Execute(registry, "compute_profile_save", second)
	if err != nil {
		t.Fatal(err)
	}
	if updated.(Profile).Version != 2 {
		t.Fatalf("tool update has wrong version: %+v", updated)
	}
	for _, tc := range []struct {
		path  string
		value string
	}{
		{"/v1/compute/profiles/builder?version=1", `"RAM_GB":"8"`},
		{"/v1/compute/profiles/builder", `"RAM_GB":"4"`},
	} {
		got := requestProfile(mux, http.MethodGet, tc.path, "")
		if got.Code != http.StatusOK || !strings.Contains(got.Body.String(), tc.value) {
			t.Fatalf("profile revision %s: %d %s", tc.path, got.Code, got.Body.String())
		}
	}
	list, err := Execute(registry, "compute_profile_list", nil)
	if err != nil || len(list.([]Profile)) != 1 || list.([]Profile)[0].Version != 2 {
		t.Fatalf("tool list failed: %v %+v", err, list)
	}
	if got := requestProfile(mux, http.MethodPut, "/v1/compute/profiles/builder", first); got.Code != http.StatusBadRequest {
		t.Fatalf("stale native update accepted: %d %s", got.Code, got.Body.String())
	}
}

func TestProfileHTTPEnforcesInputAndAvailability(t *testing.T) {
	registry, err := Open(filepath.Join(t.TempDir(), "profiles.json"))
	if err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	Register(mux, registry)
	for _, tc := range []struct {
		method string
		path   string
		body   string
		status int
	}{
		{http.MethodGet, "/v1/compute/profiles/missing", "", http.StatusNotFound},
		{http.MethodGet, "/v1/compute/profiles/missing?version=-1", "", http.StatusBadRequest},
		{http.MethodGet, "/v1/compute/profiles/missing?version=bogus", "", http.StatusBadRequest},
		{http.MethodPut, "/v1/compute/profiles/other", `{"spec":{"id":"builder"}}`, http.StatusBadRequest},
		{http.MethodPut, "/v1/compute/profiles/builder", `{"spec":{},"unexpected":true}`, http.StatusBadRequest},
	} {
		got := requestProfile(mux, tc.method, tc.path, tc.body)
		if got.Code != tc.status {
			t.Errorf("%s %s: got %d, want %d: %s", tc.method, tc.path, got.Code, tc.status, got.Body.String())
		}
	}
	unavailable := http.NewServeMux()
	Register(unavailable, nil)
	if got := requestProfile(unavailable, http.MethodGet, "/v1/compute/profiles", ""); got.Code != http.StatusServiceUnavailable {
		t.Fatalf("unconfigured registry: %d %s", got.Code, got.Body.String())
	}
	if _, err := Execute(nil, "compute_profile_list", nil); err == nil {
		t.Fatal("tool accepted an unavailable registry")
	}
	if _, err := Execute(registry, "compute_profile_plan", nil); err == nil {
		t.Fatal("resource planning must remain in the Workspace orchestrator")
	}
}
