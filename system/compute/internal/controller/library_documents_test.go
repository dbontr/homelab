package controller

import (
	"net/http"
	"os"
	"strings"
	"testing"
	"time"
)

func TestLocalLibraryDocumentsStayInWorkspaceStorage(t *testing.T) {
	db, _, server := fixture(t)
	if _, err := db.CreateLibraryProject("proof-corpus", "Proof Corpus", "", "lean"); err != nil { t.Fatal(err) }
	if _, err := db.CreateProject("quantum", "Quantum Research"); err != nil { t.Fatal(err) }
	if _, err := db.CreateProject("unrelated", "Unrelated Work"); err != nil { t.Fatal(err) }
	if _, err := db.LinkLibrary("quantum", "proof-corpus"); err != nil { t.Fatal(err) }

	original := "# Proof identity\n\nVerified under the recorded hypotheses.\n"
	published, err := db.PublishLibraryDocument("proof-corpus", "proof-corpus", "theorem.md", original)
	if err != nil { t.Fatal(err) }
	if published["sha256"] != checksum([]byte(original)) || published["immutable"] != true {
		t.Fatalf("document is not content-addressed: %#v", published)
	}
	if _, err := db.PublishLibraryDocument("proof-corpus", "proof-corpus", "theorem.md", "Different"); err == nil {
		t.Fatal("immutable document was overwritten")
	}
	if _, err := db.PublishLibraryDocument("quantum", "proof-corpus", "foreign.md", "Not allowed"); err == nil {
		t.Fatal("consumer published into another project's library")
	}
	if _, err := db.PublishLibraryDocument("proof-corpus", "proof-corpus", "../traversal.md", "Not allowed"); err == nil {
		t.Fatal("unsafe document path accepted")
	}
	for _, project := range []string{"proof-corpus", "quantum"} {
		got, err := db.ReadLibraryDocument(project, "proof-corpus", "theorem.md")
		if err != nil || got["body"] != original || got["sha256"] != checksum([]byte(original)) {
			t.Fatalf("linked library document unavailable: %#v %v", got, err)
		}
		entries, err := db.LibraryDocuments(project, "proof-corpus")
		if err != nil || len(entries) != 1 { t.Fatalf("linked document listing: %#v %v", entries, err) }
	}
	if _, err := db.ReadLibraryDocument("unrelated", "proof-corpus", "theorem.md"); err == nil {
		t.Fatal("unlinked project read private library")
	}
	status, got := call(t, server, http.MethodGet,
		"/api/projects/quantum/libraries/proof-corpus/documents/theorem.md", testOwner, nil)
	wantOK(t, status, got)
	if got["body"] != original { t.Fatalf("REST document response: %#v",got) }
	status, saved := call(t, server, http.MethodPost,
		"/api/projects/proof-corpus/libraries/proof-corpus/documents", testOwner,
		map[string]any{"name":"supplement.md","body":"# Supplement\n"})
	wantOK(t, status, saved)

}

func TestLibraryDocumentIntegrityRejectsTampering(t *testing.T) {
	db, _, _ := fixture(t)
	if _, err := db.CreateLibraryProject("articles", "Articles", "", "research"); err != nil { t.Fatal(err) }
	item, err := db.PublishLibraryDocument("articles", "articles", "review.md", "# Review\n")
	if err != nil { t.Fatal(err) }
	path := db.objectPath(asString(item["sha256"]))
	if err := os.WriteFile(path, []byte("# Edited\n"), 0600); err != nil { t.Fatal(err) }
	if _, err := db.ReadLibraryDocument("articles", "articles", "review.md"); err == nil {
		t.Fatal("document checksum mismatch accepted")
	}
}

func TestKnowledgeWritesUseLocalAuthorityAndCallerGrants(t *testing.T) {
	db, controller, server := fixture(t)
	if _, err := db.CreateProject("calculus", "Calculus"); err != nil { t.Fatal(err) }
	key, issuer := testOIDCIssuer(t, controller)
	principal := "oidc:" + issuer + "#editor"
	if _, err := db.SetProjectGrant("calculus", principal, "editor", ""); err != nil { t.Fatal(err) }
	token := signedTestToken(t, key, issuer, "project-workspace", "editor", time.Now().Add(time.Hour).Unix())
	code, note := call(t, server, http.MethodPost, "/api/projects/calculus/notes/context", token,
		map[string]any{"body":"Theorems verified to date","expected_version":0})
	wantOK(t, code, note)
	if note["version"] != float64(1) { t.Fatalf("note version: %#v",note) }
	code, _ = call(t, server, http.MethodPost, "/api/projects/calculus/notes/context", token,
		map[string]any{"body":"Unexpected overwrite","expected_version":0})
	if code == http.StatusOK { t.Fatal("stale note write accepted") }
	code, context := call(t, server, http.MethodGet, "/api/projects/calculus/knowledge/context", token, nil)
	wantOK(t, code, context)
	if context["authority"]!="local" { t.Fatalf("wrong knowledge authority: %#v",context) }
	if _, err := db.ConfigureKnowledgePolicy("calculus", "none", "", "", "", 0); err != nil {t.Fatal(err)}
	code, _ = call(t, server, http.MethodGet, "/api/projects/calculus/notes/context", token, nil)
	if code == http.StatusOK { t.Fatal("disabled local notes were disclosed") }
	if _, err := db.WriteNote("calculus", "context", "Forbidden", 1); err == nil { t.Fatal("write bypassed disabled policy") }
	code, disabled := call(t, server, http.MethodGet, "/api/projects/calculus/knowledge/context", token, nil)
	wantOK(t, code, disabled)
	if disabled["authority"] != "none" || disabled["context_document"] != nil {
		t.Fatalf("disabled authority disclosed local documents: %#v", disabled)
	}
	if _, err := db.ConfigureKnowledgePolicy("calculus", "local", "", "", "", 1); err != nil {t.Fatal(err)}
	code, read := call(t, server, http.MethodGet, "/api/projects/calculus/notes/context", token, nil)
	wantOK(t, code, read)
	if !strings.Contains(asString(read["body"]), "verified") {t.Fatal("local note was not preserved")}
}
