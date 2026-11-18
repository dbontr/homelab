package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dbontr/homelab-new/system/compute/internal/controller"
)

func TestEngineeringProgramManifest(t *testing.T) {
	entries, err := readCatalog(filepath.Join("..", "..", "catalog", "engineering-programs.json"))
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) < 20 {
		t.Fatalf("expected a general engineering catalog, got %d programs", len(entries))
	}
	byID := map[string]controller.ProgramProposal{}
	for _, p := range entries {
		byID[p.ID] = p
	}
	for _, id := range []string{
		"amd-vivado-2022-2", "amd-vivado-2024-1", "amd-vivado-2025-2",
		"go-1-27", "rust-toolchain", "gcc-cpp", "python-3", "julia-1-13",
		"lean4-lake", "ltspice-windows", "ngspice", "matlab-r2026b", "minecraft-java-reference",
	} {
		if _, ok := byID[id]; !ok {
			t.Fatalf("missing requested engineering tool: %s", id)
		}
	}
	if byID["ltspice-windows"].Platform != "windows-native" {
		t.Fatal("LTspice must not be described as a Linux container runtime")
	}
	if byID["matlab-r2026b"].SourceURL != "https://www.mathworks.com/downloads/" ||
		!strings.Contains(byID["matlab-r2026b"].LicenseNotes, "license") {
		t.Fatal("MATLAB must require official distribution and independent license qualification")
	}
	if !strings.Contains(byID["minecraft-java-reference"].LicenseNotes, "proprietary assets") ||
		byID["minecraft-java-reference"].Distribution != "oci" {
		t.Fatal("reference game must retain license and asset segregation")
	}
}

func TestReconcileRegistersRequestsAndPreservesExistingReview(t *testing.T) {
	catalog, err := readCatalog(filepath.Join("..", "..", "catalog", "engineering-programs.json"))
	if err != nil {
		t.Fatal(err)
	}
	existing := []controller.ProgramProposal{catalog[2]}
	posts := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer test-credential" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		switch r.Method {
		case http.MethodGet:
			_ = json.NewEncoder(w).Encode(existing)
		case http.MethodPost:
			var in controller.ProgramProposal
			if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
				t.Error(err)
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			existing = append(existing, in)
			posts++
			_ = json.NewEncoder(w).Encode(map[string]any{"id": in.ID, "status": "requested"})
		default:
			w.WriteHeader(http.StatusMethodNotAllowed)
		}
	}))
	defer server.Close()
	endpoint, err := controllerURL(server.URL)
	if err != nil {
		t.Fatal(err)
	}
	added, skipped, err := reconcile(context.Background(), server.Client(), endpoint, "test-credential", catalog)
	if err != nil || added != len(catalog)-1 || skipped != 1 {
		t.Fatalf("catalog import: added=%d skipped=%d err=%v", added, skipped, err)
	}
	added, skipped, err = reconcile(context.Background(), server.Client(), endpoint, "test-credential", catalog)
	if err != nil || added != 0 || skipped != len(catalog) || posts != len(catalog)-1 {
		t.Fatalf("idempotence: added=%d skipped=%d posts=%d err=%v", added, skipped, posts, err)
	}
}

func TestReconcileRejectsConflictingExistingProgram(t *testing.T) {
	entry := controller.ProgramProposal{
		ID: "python-3", Name: "Python", Version: "3.x", Platform: "linux-container",
		Distribution: "oci", SourceURL: "https://www.python.org/downloads/",
		LicenseNotes: "Check licenses",
	}
	mutated := entry
	mutated.SourceURL = "https://example.org/unverified"
	posts := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			_ = json.NewEncoder(w).Encode([]controller.ProgramProposal{mutated})
		} else {
			posts++
			w.WriteHeader(http.StatusNoContent)
		}
	}))
	defer server.Close()
	endpoint, err := controllerURL(server.URL)
	if err != nil {
		t.Fatal(err)
	}
	_, _, err = reconcile(context.Background(), server.Client(), endpoint, "test-credential", []controller.ProgramProposal{entry})
	if err == nil || !strings.Contains(err.Error(), "conflicting") || posts != 0 {
		t.Fatalf("identity conflict must stop all writes: posts=%d error=%v", posts, err)
	}
}

func TestControllerURLRequiresProtectedTransport(t *testing.T) {
	for _, raw := range []string{
		"http://external.example/", "https://user:pass@controller.example/",
		"https://controller.example/unexpected-path", "https://controller.example/#fragment",
	} {
		if _, err := controllerURL(raw); err == nil {
			t.Fatalf("accepted unsafe controller URL %q", raw)
		}
	}
	if got, err := controllerURL("https://controller.example/"); err != nil ||
		got != "https://controller.example/api/pool/programs" {
		t.Fatalf("wrong controller URL: %s %v", got, err)
	}
}

func TestCatalogRejectsDuplicateAndUntrustedURL(t *testing.T) {
	proposal := controller.ProgramProposal{
		ID: "python-3", Name: "Python", Version: "3.x", Platform: "linux-container",
		Distribution: "oci", SourceURL: "https://www.python.org/downloads/",
		LicenseNotes: "Check licenses",
	}
	for _, invalid := range [][]controller.ProgramProposal{
		{proposal, proposal},
		{func() controller.ProgramProposal { p := proposal; p.SourceURL = "http://example.org/"; return p }()},
	} {
		data, err := json.Marshal(invalid)
		if err != nil {
			t.Fatal(err)
		}
		path := filepath.Join(t.TempDir(), "catalog.json")
		if err := os.WriteFile(path, data, 0o600); err != nil {
			t.Fatal(err)
		}
		if _, err := readCatalog(path); err == nil {
			t.Fatal("invalid catalog must be rejected")
		}
	}
}

func TestTokenRequiresPrivateRegularFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "token")
	if err := os.WriteFile(path, []byte(strings.Repeat("a", 40)), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := readToken(path); err == nil {
		t.Fatal("group-readable credentials must be rejected")
	}
	if err := os.Chmod(path, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := readToken(path); err != nil {
		t.Fatal(err)
	}
}
