package transport

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/dbontr/homelab-new/system/agentflow"
)

func TestCapabilityWorkflowArchiveAPIAndAuthorization(t *testing.T) {
	server, _, dir := flowFixture(t)
	submitted, err := server.submitCapabilityWorkflow(context.Background(), CapabilityProgram{
		Project: "study",
		Objective: "Retain project evidence",
		Steps: []CapabilityStage{{ID: "context", Action: "project_context"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := server.advanceCapabilityWorkflow(context.Background(), submitted.ID, 2); err != nil {
		t.Fatal(err)
	}
	aged := time.Now().Add(-25 * time.Hour)
	if err := os.Chtimes(filepath.Join(dir, submitted.ID+".json"), aged, aged); err != nil {
		t.Fatal(err)
	}
	moved, err := server.workflows.ArchiveTerminal(time.Hour, 10)
	if err != nil || len(moved) != 1 || moved[0] != submitted.ID {
		t.Fatalf("workflow not archived: %v %v", moved, err)
	}

	h := server.Handler()
	token := strings.Repeat("z", 40)
	if got := request(h, "GET", "/v1/workflows?archived=true", "", ""); got.Code != 401 {
		t.Fatalf("anonymous archive access: %d", got.Code)
	}
	if got := request(h, "GET", "/v1/workflows?archived=all", "", token); got.Code != 400 {
		t.Fatalf("invalid archive selector accepted: %d", got.Code)
	}
	active := request(h, "GET", "/v1/workflows", "", token)
	if active.Code != 200 || strings.Contains(active.Body.String(), submitted.ID) {
		t.Fatalf("archived workflow present in active listing: %d %s", active.Code, active.Body.String())
	}
	archived := request(h, "GET", "/v1/workflows?archived=true", "", token)
	if archived.Code != 200 || !strings.Contains(archived.Body.String(), submitted.ID) ||
		strings.Contains(archived.Body.String(), `"result":`) {
		t.Fatalf("archive summary missing or exposed evidence: %d %s", archived.Code, archived.Body.String())
	}
	detail := request(h, "GET", "/v1/workflows/"+submitted.ID, "", token)
	if detail.Code != 200 || !strings.Contains(detail.Body.String(), `"result":`) {
		t.Fatalf("archived detail unavailable: %d %s", detail.Code, detail.Body.String())
	}
	if _, err := server.capabilityWorkflowTool(context.Background(), "workflow_archive_list", json.RawMessage(`{}`)); err != nil {
		t.Fatalf("MCP archival listing unavailable: %v", err)
	}
	if got := request(h, "POST", "/v1/workflows/"+submitted.ID+"/cancel", `{"revision":1}`, token); got.Code != 409 {
		t.Fatalf("archived record accepted cancellation: %d", got.Code)
	}

	engineering := agentflow.Workflow{
		ID: "engineering-only", Project: "study",
		Steps: []agentflow.Step{{ID: "build", Kind: "workspace_command", Input: json.RawMessage(`{}`), MaxAttempts: 1}},
	}
	if err := server.workflows.Create(engineering); err != nil {
		t.Fatal(err)
	}
	if got := request(h, "GET", "/v1/workflows/engineering-only", "", token); got.Code != 404 {
		t.Fatalf("engineering record visible as capability workflow: %d", got.Code)
	}
	if got := request(h, "GET", "/v1/workflows", "", token); strings.Contains(got.Body.String(), "engineering-only") {
		t.Fatalf("engineering record leaked into capability listing: %s", got.Body.String())
	}
}
