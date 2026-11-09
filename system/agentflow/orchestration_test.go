package agentflow

import (
	"encoding/json"
	"testing"
	"time"
)

func TestTeamDependenciesRequireAcyclicAssignments(t *testing.T) {
	tasks := []TeamTask{
		{Key: "runtime", DependsOn: nil},
		{Key: "api", DependsOn: []string{"runtime"}},
		{Key: "ui", DependsOn: []string{"api"}},
	}
	if err := validateTaskDependencies(tasks); err != nil {
		t.Fatal(err)
	}
	tasks[0].DependsOn = []string{"ui"}
	if err := validateTaskDependencies(tasks); err == nil {
		t.Fatal("cyclic agent dependencies accepted")
	}
	tasks[0].DependsOn = nil
	tasks[2].DependsOn = []string{"unassigned"}
	if err := validateTaskDependencies(tasks); err == nil {
		t.Fatal("unknown predecessor accepted")
	}
}

func TestIntegrationManifestRejectsUnsafeAndDuplicateArtifacts(t *testing.T) {
	valid := []IntegrationArtifact{
		{Path: "src/core.go", ArtifactID: "aabb11", SHA256: shaHex("first")},
		{Path: "src/api.go", ArtifactID: "aabb22", SHA256: shaHex("second")},
	}
	if _, err := integrationFileMap(valid); err != nil {
		t.Fatal(err)
	}
	repeated := append(append([]IntegrationArtifact(nil), valid...), valid[0])
	if _, err := integrationFileMap(repeated); err == nil {
		t.Fatal("duplicate source artifact accepted")
	}
	secret := []IntegrationArtifact{{Path: ".env", ArtifactID: "aabb11", SHA256: shaHex("secret")}}
	if _, err := integrationFileMap(secret); err == nil {
		t.Fatal("credential-bearing file accepted")
	}
}

func TestGraphIncludesDeclaredImportsWithoutClaimingSemanticResolution(t *testing.T) {
	code := CodeMap{Files: []CodeOutline{
		{Path: "lib/server.go", Language: "go", Imports: []string{"net/http", "encoding/json"}},
		{Path: "lib/client.go", Language: "go", Imports: []string{"net/http"}},
	}}
	graph := graphFromMap(code)
	if len(graph.Nodes) != 4 || len(graph.Edges) != 3 {
		t.Fatalf("unexpected source graph: %+v", graph)
	}
	if graph.Edges[0].Kind != "declares_import" {
		t.Fatalf("graph incorrectly labeled source relationship: %+v", graph.Edges[0])
	}
	if graph.Nodes[0].ID > graph.Nodes[1].ID {
		t.Fatal("graph output is not stable")
	}
}

func TestKnowledgeProposalNeedsEvidenceAndBoundedContent(t *testing.T) {
	action := AgentAction{Action: "propose_knowledge", KnowledgeKind: "finding",
		KnowledgeTitle: "Bounded scheduling", KnowledgeBody: "The workflow uses project limits.",
		EvidencePaths: []string{"system/scheduler.go"}}
	if !validKnowledgeProposal(action) {
		t.Fatal("valid source-backed finding rejected")
	}
	action.EvidencePaths = []string{".env"}
	if validKnowledgeProposal(action) {
		t.Fatal("protected source accepted as knowledge evidence")
	}
}

func TestVerificationDiagnosisKeepsOutputHashAndCoverage(t *testing.T) {
	result := classifyCheck("go test ./...", 1,
		"total: (statements) 83.5%", "--- FAIL: TestIntegrity")
	if result.Category != "test_failure" || result.Coverage != 83.5 || !hexDigest(result.OutputSHA) {
		t.Fatalf("diagnostic metadata differs: %+v", result)
	}
}

func TestUnlinkedIntegrationCannotAcquireAWorkerLease(t *testing.T) {
	s := testStore(t)
	w := Workflow{ID: "run-orphan", Project: "research", IntegrationTeamID: "team-unknown",
		Steps: []Step{{ID: "provision", Kind: "provision", MaxAttempts: 1,
			Input: json.RawMessage(`{"image_pool_id":"debian-bookworm-slim"}`)}}}
	if err := s.Create(w); err != nil {
		t.Fatal(err)
	}
	if _, err := s.ClaimReady(w.ID, time.Minute); err == nil {
		t.Fatal("unlinked integration workflow acquired an execution lease")
	}
}
