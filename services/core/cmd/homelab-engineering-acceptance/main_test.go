package main

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/dbontr/homelab-new/system/agentflow"
)

func testConfig() acceptanceConfig {
	return acceptanceConfig{
		Project: "homelab-platform", Repository: "dbontr/homelab", BaseBranch: "main",
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
	valid := map[string]any{"draft": true, "base_sha": c.Revision, "url": "https://github.com/dbontr/homelab/pull/198"}
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
		func(v map[string]any) { v["url"] = "https://evil.example.org/dbontr/homelab/pull/198" },
		func(v map[string]any) { v["url"] = "https://github.com/dbontr/homelab/pull/198/merge" },
	} {
		v := map[string]any{"draft": true, "base_sha": c.Revision, "url": "https://github.com/dbontr/homelab/pull/198"}
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
type fakeWorkspaceControl struct {
	states []string
	stopRequests int
	lists int
}
func (f *fakeWorkspaceControl) Call(_ context.Context, name string, args map[string]any) (map[string]any, error) {
	if args["project_id"] != "research" {
		return nil, errors.New("wrong project")
	}
	switch name {
	case "workspace_destroy":
		if args["workspace_id"] != "aabbccdd" {
			return nil, errors.New("wrong workspace")
		}
		f.stopRequests++
		return map[string]any{"status": "stopping"}, nil
	case "workspace_list":
		f.lists++
		if len(f.states) == 0 {
			return map[string]any{"result": []any{}}, nil
		}
		state := f.states[0]
		if len(f.states) > 1 {
			f.states = f.states[1:]
		}
		return map[string]any{"result": []any{map[string]any{"id": "aabbccdd", "status": state}}}, nil
	default:
		return nil, errors.New("unexpected compute operation")
	}
}
func TestWorkspaceStopRequiresConfirmedTerminalStatus(t *testing.T) {
	f := &fakeWorkspaceControl{states: []string{"stopping", "stopped"}}
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
	defer cancel()
	if err := stopWorkspace(ctx, f, "research", "aabbccdd"); err != nil {
		t.Fatal(err)
	}
	if f.stopRequests != 1 || f.lists != 2 {
		t.Fatalf("workspace stop was not confirmed: %+v", f)
	}
}
func TestWorkspaceStopFailsOnUncertainCleanup(t *testing.T) {
	f := &fakeWorkspaceControl{states: []string{"ready"}}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := stopWorkspace(ctx, f, "research", "aabbccdd"); err == nil {
		t.Fatal("unconfirmed workspace termination accepted")
	}
	f = &fakeWorkspaceControl{states: []string{"failed"}}
	if err := stopWorkspace(context.Background(), f, "research", "aabbccdd"); err == nil {
		t.Fatal("failed workspace state accepted")
	}
}

type fakeRepositoryControl struct {
	memberships []any
}
func (f fakeRepositoryControl) Call(_ context.Context, name string, args map[string]any) (map[string]any, error) {
	if name != "repository_memberships" || args["project_id"] != "research" {
		return nil, errors.New("unexpected project or operation")
	}
	return map[string]any{"result": f.memberships}, nil
}
func TestAcceptanceRequiresLinkedSourceRepository(t *testing.T) {
	ctx := context.Background()
	service := fakeRepositoryControl{memberships: []any{map[string]any{"repository":"example/repo","role":"source"}}}
	if err := verifyLinkedRepository(ctx, service, "research", "example/repo"); err != nil {
		t.Fatal(err)
	}
	if err := verifyLinkedRepository(ctx, service, "research", "example/other"); err == nil {
		t.Fatal("unlinked repository accepted")
	}
	service.memberships = []any{map[string]any{"repository":"example/repo","role":"reference"}}
	if err := verifyLinkedRepository(ctx, service, "research", "example/repo"); err == nil {
		t.Fatal("read-only linked repository accepted as source")
	}
}
