package controller

import (
	"testing"
	"time"
)

func TestProjectCatalogVisibilityIsReversibleAndNonDestructive(t *testing.T) {
	db, _, srv := fixture(t)
	project(t, srv)
	if _, err := db.CreateProject("other", "Other research"); err != nil { t.Fatal(err) }
	if _, err := db.CreateProject("homelab-platform", "Platform"); err != nil { t.Fatal(err) }
	if _, err := db.WriteNote("demo", "context", "permanent record", 0); err != nil { t.Fatal(err) }
	artifact, err := db.ArtifactSave("demo", "proof.txt", []byte("verified"))
	if err != nil { t.Fatal(err) }
	principal := "oidc:https://issuer.example#reviewer"
	if _, err := db.SetProjectGrant("demo", principal, "viewer", ""); err != nil { t.Fatal(err) }

	catalog, err := db.ProjectCatalog()
	if err != nil || len(catalog) != 3 { t.Fatalf("catalog: %v %v", catalog, err) }
	for _, row := range catalog {
		if row["visible"] != true || asInt(row["revision"]) != 0 {
			t.Fatalf("default project visibility is incorrect: %v", row)
		}
	}
	if _, err := db.SetProjectCatalogVisibility("demo", false, 0, "other"); err == nil {
		t.Fatal("confirmation may not reference a different project")
	}
	if _, err := db.SetProjectCatalogVisibility("homelab-platform", false, 0, "homelab-platform"); err == nil {
		t.Fatal("canonical platform must not be hidden")
	}
	hidden, err := db.SetProjectCatalogVisibility("demo", false, 0, "demo")
	if err != nil || hidden["visible"] != false || hidden["revision"] != 1 {
		t.Fatalf("failed to hide catalog entry: %v %v", hidden, err)
	}
	if _, err := db.SetProjectCatalogVisibility("demo", true, 0, "demo"); err == nil {
		t.Fatal("stale revision accepted")
	}
	if _, err := db.SetProjectCatalogVisibility("missing", false, 0, "missing"); err == nil {
		t.Fatal("unknown project accepted")
	}
	visible, err := db.Projects()
	if err != nil || len(visible) != 2 { t.Fatalf("visible projects: %v %v", visible, err) }
	for _, row := range visible {
		if row["id"] == "demo" { t.Fatal("hidden project leaked into ordinary listing") }
	}
	granted, err := db.ProjectsForPrincipal(principal)
	if err != nil || len(granted) != 0 { t.Fatalf("hidden grant visible: %v %v", granted, err) }
	if note, err := db.ReadNote("demo", "context"); err != nil || note["body"] != "permanent record" {
		t.Fatalf("hiding project altered knowledge: %v %v", note, err)
	}
	if stored, err := db.ArtifactInfo("demo", asString(artifact["id"])); err != nil || stored["sha256"] != artifact["sha256"] {
		t.Fatalf("hiding project altered artifact: %v %v", stored, err)
	}
	reopened, err := OpenDatabase(db.filename)
	if err != nil { t.Fatal(err) }
	defer reopened.Close()
	afterRestart, err := reopened.ProjectCatalog()
	if err != nil { t.Fatal(err) }
	found := false
	for _, row := range afterRestart {
		if row["id"] == "demo" {
			found = true
			if row["visible"] != false || asInt(row["revision"]) != 1 { t.Fatalf("hidden state not persisted: %v", row) }
		}
	}
	if !found { t.Fatal("historical project disappeared from administrator inventory") }
	shown, err := db.SetProjectCatalogVisibility("demo", true, 1, "demo")
	if err != nil || shown["visible"] != true || shown["revision"] != 2 {
		t.Fatalf("restore project visibility: %v %v", shown, err)
	}
	visible, err = db.Projects()
	if err != nil || len(visible) != 3 { t.Fatalf("restored catalog: %v %v", visible, err) }
	granted, err = db.ProjectsForPrincipal(principal)
	if err != nil || len(granted) != 1 || granted[0]["id"] != "demo" {
		t.Fatalf("restored project membership: %v %v", granted, err)
	}
}

func TestProjectCatalogMCPAuthorization(t *testing.T) {
	db, server, srv := fixture(t)
	project(t, srv)
	key, issuer := testOIDCIssuer(t, server)
	token := signedTestToken(t, key, issuer, "project-workspace", "researcher", time.Now().Add(time.Hour).Unix())
	principal := "oidc:" + issuer + "#researcher"
	if _, err := db.SetProjectGrant("demo", principal, "owner", ""); err != nil { t.Fatal(err) }
	callTool := func(token, name string, arguments map[string]any) bool {
		t.Helper()
		code, response := call(t, srv, "POST", "/mcp/", token, map[string]any{
			"jsonrpc": "2.0", "id": 1, "method": "tools/call",
			"params": map[string]any{"name": name, "arguments": arguments},
		})
		wantOK(t, code, response)
		result, _ := response["result"].(map[string]any)
		return asBool(result["isError"])
	}
	update := map[string]any{"project_id": "demo", "visible": false, "expected_revision": 0, "confirm_project_id": "demo"}
	if !callTool(token, "project_catalog_set", update) || !callTool(token, "list_project_catalog", map[string]any{}) {
		t.Fatal("OIDC project owner may not administer the global catalog")
	}
	if !callTool(testOwner, "project_catalog_set", map[string]any{"project_id": "demo", "visible": "false", "expected_revision": 0, "confirm_project_id": "demo"}) {
		t.Fatal("catalog tool accepted a non-boolean visibility value")
	}
	if !callTool(testOwner, "project_catalog_set", map[string]any{"project_id": "demo", "visible": false, "expected_revision": 1.5, "confirm_project_id": "demo"}) {
		t.Fatal("catalog tool accepted a fractional revision")
	}
	if callTool(testOwner, "project_catalog_set", update) || callTool(testOwner, "list_project_catalog", map[string]any{}) {
		t.Fatal("owner catalog operations were rejected")
	}
	if !callTool(testOwner, "project_catalog_set", update) {
		t.Fatal("duplicate catalog mutation should fail its revision guard")
	}
}
