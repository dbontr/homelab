package controller

import (
	"encoding/base64"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestProjectKnowledgeContextUsesOnlySelectedGitAuthority(t *testing.T) {
	db, controller, _ := fixture(t)
	if _, err := db.CreateProject("research", "Research"); err != nil {
		t.Fatal(err)
	}
	if _, err := db.WriteNote("research", "project", "stale local content", 0); err != nil {
		t.Fatal(err)
	}
	local, err := controller.ProjectKnowledgeContext("research")
	if err != nil || local["authority"] != "local" || local["project_document"] == nil {
		t.Fatalf("local knowledge was not selected: %v %v", local, err)
	}
	if _, err := db.ConfigureKnowledgePolicy("research", "none", "", "", "", 0); err != nil {
		t.Fatal(err)
	}
	disabled, err := controller.ProjectKnowledgeContext("research")
	if err != nil || disabled["authority"] != "none" || disabled["project_document"] != nil {
		t.Fatalf("disabled knowledge disclosed local notes: %v %v", disabled, err)
	}
	const sha = "0123456789012345678901234567890123456789"
	remote := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.URL.Path == "/repos/example/knowledge/commits/main":
			fmt.Fprintf(w, `{"sha":"%s"}`, sha)
		case r.URL.Path == "/repos/example/knowledge/contents/knowledge/Project.md":
			if r.URL.Query().Get("ref") != sha {
				t.Errorf("knowledge read was not pinned to a commit")
			}
			fmt.Fprintf(w, `{"type":"file","encoding":"base64","sha":"blob","content":"%s"}`,
				base64.StdEncoding.EncodeToString([]byte("# Authoritative project")))
		case r.URL.Path == "/repos/example/knowledge/contents/knowledge/Context.md":
			http.NotFound(w, r)
		default:
			t.Errorf("unexpected remote request: %s", r.URL)
			http.NotFound(w, r)
		}
	}))
	defer remote.Close()
	controller.GitHub = NewGitHub("test-token")
	controller.GitHub.API = remote.URL
	if _, err := db.ConfigureKnowledgePolicy("research", "git", "example/knowledge", "main", "knowledge", 1); err != nil {
		t.Fatal(err)
	}
	context, err := controller.ProjectKnowledgeContext("research")
	if err != nil {
		t.Fatal(err)
	}
	doc := gitObj(context["project_document"])
	if context["authority"] != "git" || context["commit_sha"] != sha ||
		doc["content"] != "# Authoritative project" || context["context_document"] != nil ||
		strings.Contains(fmt.Sprint(context), "stale local content") {
		t.Fatalf("project context did not honor Git authority: %v", context)
	}
}
