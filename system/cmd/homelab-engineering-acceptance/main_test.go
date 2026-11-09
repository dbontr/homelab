package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/dbontr/homelab-new/system/agentflow"
)

func testConfig() acceptanceConfig {
	return acceptanceConfig{
		Project: "homelab-platform", Repository: "dbontr/homelab-new", BaseBranch: "main",
		Revision: strings.Repeat("a", 40), ComputeURL: "https://example.org/mcp",
		ComputeTokenFile: "/tmp/protected-identity", ModelURL: "https://model.example.org/v1/chat/completions",
		ModelName: "model", ImagePoolID: "debian-bookworm-slim", Timeout: 20 * time.Minute,
	}
}

func TestAcceptanceScopeValidation(t *testing.T) {
	good := testConfig()
	if err := good.validate(); err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		name   string
		change func(*acceptanceConfig)
	}{
		{"missing_identity", func(c *acceptanceConfig) { c.ComputeTokenFile = "" }},
		{"wrong_base", func(c *acceptanceConfig) { c.BaseBranch = "feature/other" }},
		{"untrusted_revision", func(c *acceptanceConfig) { c.Revision = "main" }},
		{"unbounded_duration", func(c *acceptanceConfig) { c.Timeout = 90 * time.Minute }},
		{"unsafe_project", func(c *acceptanceConfig) { c.Project = "../prod" }},
		{"missing_model", func(c *acceptanceConfig) { c.ModelName = "" }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c := good
			tc.change(&c)
			if err := c.validate(); err == nil {
				t.Fatal("unsafe acceptance accepted")
			}
		})
	}
}

func TestAcceptanceProgramUsesOneFileAndFixedCheck(t *testing.T) {
	c := testConfig()
	p := acceptanceProgram(c, "feedface0000beef")
	if !p.Autonomous || len(p.AllowedPaths) != 1 || len(p.Checks) != 1 || len(p.Changes) != 0 || p.ExpectedHead != p.Revision {
		t.Fatalf("acceptance program exceeded intended scope: %+v", p)
	}
	if p.AllowedPaths[0] != "tests/engineering-acceptance/feedface0000beef.txt" || !strings.Contains(p.Checks[0], "homelab-engineering-acceptance-feedface0000beef") {
		t.Fatal("acceptance source path and check are not coupled")
	}
	if p.DurationMinutes <= int(c.Timeout/time.Minute) {
		t.Fatal("workspace would expire before acceptance deadline")
	}
}

func TestDraftMustMatchExactRepositoryAndPinnedRevision(t *testing.T) {
	c := testConfig()
	valid := map[string]any{"draft": true, "base_sha": c.Revision, "url": "https://github.com/dbontr/homelab-new/pull/198"}
	makeRun := func(v map[string]any) agentflow.RunSummary {
		b, _ := json.Marshal(v)
		return agentflow.RunSummary{Steps: []agentflow.StepSummary{{Kind: "publish", Status: agentflow.Succeeded, Result: b}}}
	}
	if url, err := draftURL(makeRun(valid), c.Revision, c.Repository); err != nil || url != valid["url"] {
		t.Fatal(err)
	}
	for _, change := range []func(map[string]any){
		func(v map[string]any) { v["draft"] = false },
		func(v map[string]any) { v["base_sha"] = strings.Repeat("b", 40) },
		func(v map[string]any) { v["url"] = "https://evil.example.org/dbontr/homelab-new/pull/198" },
		func(v map[string]any) { v["url"] = "https://github.com/dbontr/homelab-new/pull/198/merge" },
	} {
		v := map[string]any{"draft": true, "base_sha": c.Revision, "url": "https://github.com/dbontr/homelab-new/pull/198"}
		change(v)
		if _, err := draftURL(makeRun(v), c.Revision, c.Repository); err == nil {
			t.Fatal("untrusted draft result accepted")
		}
	}
}

func TestProtectedModelCredential(t *testing.T) {
	p := filepath.Join(t.TempDir(), "token")
	if err := os.WriteFile(p, []byte(strings.Repeat("x", 40)), 0600); err != nil {
		t.Fatal(err)
	}
	token, err := tokenFromFile(p)
	if err != nil || len(token) != 40 {
		t.Fatal(err)
	}
	if err := os.Chmod(p, 0644); err != nil {
		t.Fatal(err)
	}
	if _, err := tokenFromFile(p); err == nil {
		t.Fatal("world-readable model token accepted")
	}
}