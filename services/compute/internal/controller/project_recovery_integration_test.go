package controller

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// A recovery inventory is derived separately from each SQLite authority.
// The legacy-reconcile CLI must also compare every physical artifact byte.
type recoveredLegacyRecord struct {
	LegacyID    string   `json:"legacy_id"`
	Revision    string   `json:"revision"`
	ProjectID   string   `json:"project_id"`
	Kind        string   `json:"kind"`
	CreatedAt   string   `json:"created_at"`
	SourcePath  string   `json:"source_path"`
	RestorePath string   `json:"restore_path"`
	SHA256      string   `json:"sha256"`
	Size        int64    `json:"size"`
	Links       []string `json:"links,omitempty"`
}

func writeRecoveryInventory(t *testing.T, db *Database, destination string) int {
	t.Helper()
	rows, err := db.query("SELECT id,project,name,sha256,size,created FROM artifacts ORDER BY id")
	if err != nil {
		t.Fatal(err)
	}
	records := make([]recoveredLegacyRecord, 0, len(rows))
	byID := make(map[string]recoveredLegacyRecord, len(rows))
	for _, row := range rows {
		id := asString(row["id"])
		digest := asString(row["sha256"])
		record := recoveredLegacyRecord{
			LegacyID: "artifact:" + id, Revision: "sha256:" + digest,
			ProjectID: asString(row["project"]), Kind: "artifact",
			CreatedAt: time.Unix(int64(asFloat(row["created"])), 0).UTC().Format(time.RFC3339Nano),
			SourcePath: id, RestorePath: id,
			SHA256: digest, Size: int64(asFloat(row["size"])),
		}
		records = append(records, record)
		byID[id] = record
	}
	// The copy name encodes the original immutable artifact ID. Do not
	// infer this relationship from a similarly named Git repository.
	for index, row := range rows {
		name := asString(row["name"])
		if !strings.HasPrefix(name, "legacy-acceptance-") {
			continue
		}
		remainder := strings.TrimPrefix(name, "legacy-acceptance-")
		sourceID, _, ok := strings.Cut(remainder, "-")
		if !ok {
			t.Fatalf("legacy copy has no original identifier: %s", name)
		}
		source, exists := byID[sourceID]
		if !exists || source.ProjectID != "acceptance" {
			t.Fatalf("legacy copy has no matching original: %s", name)
		}
		records[index].Links = []string{source.LegacyID + "@" + source.Revision}
	}
	raw, err := json.MarshalIndent(struct {
		Version int                     `json:"version"`
		Records []recoveredLegacyRecord `json:"records"`
	}{1, records}, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(destination, raw, 0600); err != nil {
		t.Fatal(err)
	}
	return len(records)
}

func TestEncryptedIntegratedResearchRecovery(t *testing.T) {
	repository, root := setupResticTest(t)
	db, err := OpenDatabase(filepath.Join(root, "source", "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	for _, project := range []struct{ id, title string }{
		{"homelab-platform", "Platform"}, {"acceptance", "Historical acceptance"},
		{"workspace-mcp", "Historical workspace"},
	} {
		if _, err := db.CreateProject(project.id, project.title); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := db.WriteNote("homelab-platform", "project", "First research context", 0); err != nil {
		t.Fatal(err)
	}
	if _, err := db.WriteNote("homelab-platform", "project", "Revisioned research context", 1); err != nil {
		t.Fatal(err)
	}
	if _, err := db.LinkRepository("homelab-platform", "example/research"); err != nil {
		t.Fatal(err)
	}
	legacyData := []byte("preserved historical scientific evidence")
	original, err := db.ArtifactSave("acceptance", "result.bin", legacyData)
	if err != nil {
		t.Fatal(err)
	}
	originalID := asString(original["id"])
	copyName := fmt.Sprintf("legacy-acceptance-%s-result.bin", originalID)
	copied, err := db.ArtifactSave("homelab-platform", copyName, legacyData)
	if err != nil {
		t.Fatal(err)
	}
	experiment, err := db.ArtifactSave("homelab-platform", "experiment-0001.ipynb", []byte("{\"cells\":[],\"nbformat\":4}"))
	if err != nil {
		t.Fatal(err)
	}
	contribution, err := db.ContributeProject("homelab-platform", ContributionRequest{
		Kind: "finding", Title: "Experiment and legacy provenance",
		Body: "Experimental notebook and immutable acceptance source: " + originalID,
		Repository: "example/research", Revision: strings.Repeat("a", 40),
		ArtifactID: asString(experiment["id"]),
	}, "controller-owner")
	if err != nil {
		t.Fatal(err)
	}
	task, err := db.CreatePersonalTask("controller-owner", "Experiment review", "Retain every file revision", []string{"research"})
	if err != nil {
		t.Fatal(err)
	}
	taskID := asString(task["id"])
	for _, version := range []struct{ body string; expected int }{
		{"experimental hypothesis", 0}, {"experimental result and corrections", 1},
	} {
		if _, err := db.SavePersonalTaskText("controller-owner", taskID, "notes.txt", version.body, version.expected); err != nil {
			t.Fatal(err)
		}
	}
	job, err := db.QueueKueueJob("homelab-platform", "synthetic-restore-request",
		"printf completed", "example/image@sha256:"+strings.Repeat("b", 64), 0.2, 128)
	if err != nil {
		t.Fatal(err)
	}
	jobID := asString(job["id"])
	if err := db.updateKueueJob(jobID, "synthetic-job-uid", "succeeded", "recovered computation output"); err != nil {
		t.Fatal(err)
	}
	originalManifest := filepath.Join(root, "source-manifest.json")
	sourceCount := writeRecoveryInventory(t, db, originalManifest)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	snapshot, err := db.BackupOffsite(ctx, repository)
	if err != nil || !resticSnapshotID.MatchString(snapshot) {
		t.Fatalf("no exact encrypted snapshot receipt: %q %v", snapshot, err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	if err := FullCheckOffsite(ctx, repository); err != nil {
		t.Fatalf("full encrypted repository data verification: %v", err)
	}
	recoveredPath := filepath.Join(root, "independent", "state.db")
	if err := RestoreOffsite(ctx, repository, snapshot, recoveredPath); err != nil {
		t.Fatal(err)
	}
	recovered, err := OpenDatabase(recoveredPath)
	if err != nil {
		t.Fatal(err)
	}
	defer recovered.Close()
	projects, err := recovered.Projects()
	if err != nil || len(projects) != 3 {
		t.Fatalf("restored project identities: %v %v", projects, err)
	}
	contextRecord, err := recovered.ProjectContext("homelab-platform")
	if err != nil {
		t.Fatal(err)
	}
	projectNote, ok := contextRecord["project_document"].(map[string]any)
	if !ok || asString(projectNote["body"]) != "Revisioned research context" || asInt(projectNote["version"]) != 2 {
		t.Fatalf("knowledge revision changed: %v", contextRecord)
	}
	restoredContribution, err := recovered.ProjectContribution("homelab-platform", asString(contribution["id"]))
	if err != nil || restoredContribution["revision"] != strings.Repeat("a", 40) ||
		restoredContribution["artifact_id"] != asString(experiment["id"]) {
		t.Fatalf("immutable experiment provenance lost: %v %v", restoredContribution, err)
	}
	restoredJob, err := recovered.KueueJob("homelab-platform", jobID)
	if err != nil || restoredJob["project"] != "homelab-platform" ||
		restoredJob["request_id"] != "synthetic-restore-request" ||
		restoredJob["status"] != "succeeded" ||
		restoredJob["result"] != "recovered computation output" {
		t.Fatalf("job/project/durable result relationship lost: %v %v", restoredJob, err)
	}
	for revision, want := range []string{"experimental hypothesis", "experimental result and corrections"} {
		data, err := recovered.PersonalTaskFileBase64("controller-owner", taskID, "notes.txt", revision+1)
		if err != nil {
			t.Fatalf("file revision %d unavailable: %v", revision+1, err)
		}
		bytes, err := base64.StdEncoding.DecodeString(asString(data["data_base64"]))
		if err != nil || string(bytes) != want ||
			data["sha256"] != checksum([]byte(want)) {
			t.Fatalf("file revision %d changed: %v %v", revision+1, data, err)
		}
	}
	for _, entry := range []struct {
		saved   map[string]any
		project string
	}{
		{original, "acceptance"},
		{copied, "homelab-platform"},
		{experiment, "homelab-platform"},
	} {
		saved, project := entry.saved, entry.project
		id := asString(saved["id"])
		restoredMeta, err := recovered.ArtifactInfo(project, id)
		if err != nil || asString(restoredMeta["sha256"]) != asString(saved["sha256"]) {
			t.Fatalf("restored artifact metadata lost: %s %v %v", id, restoredMeta, err)
		}
		if digest, size, err := hashFile(recovered.artifactPath(id)); err != nil ||
			digest != asString(saved["sha256"]) || size != int64(asFloat(saved["size"])) {
			t.Fatalf("restored artifact checksum or size mismatch: %s %v", id, err)
		}
	}
	restoredManifest := filepath.Join(root, "restored-manifest.json")
	if count := writeRecoveryInventory(t, recovered, restoredManifest); count != sourceCount {
		t.Fatalf("artifact inventory changed: original=%d restored=%d", sourceCount, count)
	}
	audit := func() ([]byte, error) {
		command := exec.CommandContext(ctx, "go", "run", "../../cmd/legacy-reconcile",
			"--source-root", filepath.Join(root, "source", "artifacts"),
			"--manifest", originalManifest,
			"--restore-root", filepath.Join(root, "independent", "artifacts"),
			"--restored-manifest", restoredManifest)
		return command.CombinedOutput()
	}
	report, err := audit()
	if err != nil {
		t.Fatalf("independently generated manifest comparison failed: %v\n%s", err, report)
	}
	var results struct {
		SourceVerified  int   `json:"source_verified"`
		RestoreVerified int   `json:"restore_verified"`
		Findings        []any `json:"findings"`
	}
	if err := json.Unmarshal(report, &results); err != nil || results.SourceVerified != sourceCount ||
		results.RestoreVerified != sourceCount || len(results.Findings) != 0 {
		t.Fatalf("incomplete legacy recovery: %v %s", err, report)
	}
	// An incomplete restored inventory must never be reported as certified.
	unexpected := filepath.Join(root, "independent", "artifacts", "undocumented")
	if err := os.WriteFile(unexpected, []byte("unclassified"), 0600); err != nil {
		t.Fatal(err)
	}
	if output, err := audit(); err == nil || !strings.Contains(string(output), "unmapped_restored_file") {
		t.Fatalf("extra restored content went undetected: %v %s", err, output)
	}
}
