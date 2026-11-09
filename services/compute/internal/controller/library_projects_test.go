package controller

import (
	"strings"
	"testing"
)

func TestLibraryProjectPublishesAReusableLeanLibrary(t *testing.T) {
	db, _, srv := fixture(t)
	status, created := call(t, srv, "POST", "/api/projects", testOwner, map[string]any{
		"id": "lean-proof-library", "title": "Lean Proof Library", "kind": "library",
		"library_kind": "lean", "description": "Reusable formal proofs",
	})
	wantOK(t, status, created)
	if created["kind"] != "library" || created["published_library"] != "lean-proof-library" {
		t.Fatalf("library publication missing from new project: %v", created)
	}
	if _, err := db.CreateProject("quantum", "Quantum algorithms"); err != nil {
		t.Fatal(err)
	}
	if _, err := db.CreateProject("verification", "Software verification"); err != nil {
		t.Fatal(err)
	}
	for _, project := range []string{"quantum", "verification"} {
		if _, err := db.LinkLibrary(project, "lean-proof-library"); err != nil {
			t.Fatalf("link library to %s: %v", project, err)
		}
	}
	if _, err := db.AddLibrarySource("lean-proof-library", "mathematics/formal", testCommit,
		"Math", "formalization"); err != nil {
		t.Fatal(err)
	}
	for _, project := range []string{"quantum", "verification", "lean-proof-library"} {
		resolved, err := db.ResolveLibrary(project, "lean-proof-library")
		if err != nil {
			t.Fatalf("library resolution for %s: %v", project, err)
		}
		sources := resolved["sources"].([]map[string]any)
		if len(sources) != 1 || sources[0]["revision"] != testCommit {
			t.Fatalf("unpinned published library source for %s: %v", project, sources)
		}
		if project != "lean-proof-library" && db.RepositoryLinked(project, "mathematics/formal") {
			t.Fatalf("library consumer %s inherited source-repository permissions", project)
		}
		policy, err := db.KnowledgePolicy(project)
		if err != nil || policy["mode"] != "local" {
			t.Fatalf("library link changed knowledge policy for %s: %v %v", project, policy, err)
		}
	}
	projects, err := db.Projects()
	if err != nil {
		t.Fatal(err)
	}
	kinds := map[string]string{}
	for _, project := range projects {
		kinds[asString(project["id"])] = asString(project["kind"])
	}
	if kinds["lean-proof-library"] != "library" ||
		kinds["quantum"] != "standard" || kinds["verification"] != "standard" {
		t.Fatalf("project kinds should be independent of linked libraries: %v", kinds)
	}
	status, publication := call(t, srv, "GET",
		"/api/projects/lean-proof-library/published-library", testOwner, nil)
	wantOK(t, status, publication)
	if publication["id"] != "lean-proof-library" || publication["kind"] != "lean" ||
		publication["publishing_project"] != "lean-proof-library" {
		t.Fatalf("published library lookup: %v", publication)
	}
	if _, err := db.PublishedProjectLibrary("quantum"); err == nil {
		t.Fatal("library consumer was mistaken for the library publisher")
	}
}

func TestLibraryProjectCreationIsAtomicAndValidatesKind(t *testing.T) {
	db, _, _ := fixture(t)
	if _, err := db.CreateLibraryProject("invalid-kind", "Bad", "", "unknown"); err == nil {
		t.Fatal("invalid library kind was accepted")
	}
	if _, err := db.CreateLibrary("occupied", "Existing library", "", "lean"); err != nil {
		t.Fatal(err)
	}
	if _, err := db.CreateLibraryProject("occupied", "Collision", "", "lean"); err == nil {
		t.Fatal("existing library ID was overwritten")
	}
	projects, err := db.Projects()
	if err != nil {
		t.Fatal(err)
	}
	for _, project := range projects {
		id := asString(project["id"])
		if id == "invalid-kind" || id == "occupied" {
			t.Fatalf("failed library-project creation left an orphan project: %s", id)
		}
	}
	if _, err := db.CreateLibraryProject(personalComputeScope, "Forbidden", "", "lean"); err == nil {
		t.Fatal("reserved personal task scope was accepted as a library project")
	}
	if _, err := db.CreateLibraryProject("empty-title", strings.Repeat(" ", 4), "", "lean"); err == nil {
		t.Fatal("blank library project title was accepted")
	}
}


func TestLibraryProjectMCPToolCreationAndInspection(t *testing.T) {
	_, _, srv := fixture(t)
	invoke := func(name string, args map[string]any) map[string]any {
		t.Helper()
		code, response := call(t, srv, "POST", "/mcp/", testOwner, map[string]any{
			"jsonrpc": "2.0", "id": 17, "method": "tools/call",
			"params": map[string]any{"name": name, "arguments": args},
		})
		wantOK(t, code, response)
		result := gitObj(response["result"])
		if result["isError"] == true {
			t.Fatalf("MCP %s failed: %v", name, result)
		}
		return gitObj(result["structuredContent"])
	}
	result := invoke("create_library_project", map[string]any{
		"project_id": "formal-lemmas", "title": "Formal Lemmas",
		"library_kind": "lean", "description": "Verified proof dependencies",
	})
	if result["kind"] != "library" || result["published_library"] != "formal-lemmas" {
		t.Fatalf("incorrect MCP library project result: %v", result)
	}
	published := invoke("project_published_library", map[string]any{"project_id": "formal-lemmas"})
	if published["id"] != "formal-lemmas" || published["publishing_project"] != "formal-lemmas" {
		t.Fatalf("MCP library publication missing: %v", published)
	}
}
