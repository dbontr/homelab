package agentflow

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"
)

func archiveSample(id string) Workflow {
	return Workflow{
		ID: id, Project: "project-1", CapabilityPlan: true,
		Steps: []Step{{ID: "context", Kind: "project_context", Input: json.RawMessage(`{}`), MaxAttempts: 1}},
	}
}

func ageArchiveCandidate(t *testing.T, s *Store, id string) {
	t.Helper()
	aged := s.now().Add(-2 * time.Hour)
	if err := os.Chtimes(s.filename(id), aged, aged); err != nil {
		t.Fatal(err)
	}
}

func TestArchiveRetainsEvidenceAcrossRestartAndPreventsIDReuse(t *testing.T) {
	s := testStore(t)
	w := archiveSample("cap-evidence")
	if err := s.Create(w); err != nil {
		t.Fatal(err)
	}
	claim, err := s.ClaimReady(w.ID, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	evidence := json.RawMessage(`{"citation":"source-1","result":"verified"}`)
	if err := s.Complete(claim, evidence, ""); err != nil {
		t.Fatal(err)
	}
	ageArchiveCandidate(t, s, w.ID)
	moved, err := s.ArchiveTerminal(time.Hour, 10)
	if err != nil || !reflect.DeepEqual(moved, []string{w.ID}) {
		t.Fatalf("unexpected archival result: %v %v", moved, err)
	}
	active, err := s.List()
	if err != nil || len(active) != 0 {
		t.Fatalf("archived workflow still active: %v %v", active, err)
	}
	archived, err := s.ListArchived(10)
	if err != nil || !reflect.DeepEqual(archived, []string{w.ID}) {
		t.Fatalf("archived workflow not listed: %v %v", archived, err)
	}
	reopened, err := Open(s.dir)
	if err != nil {
		t.Fatal(err)
	}
	record, err := reopened.Get(w.ID)
	if err != nil || len(record.Steps) != 1 || !reflect.DeepEqual(record.Steps[0].Result, evidence) {
		t.Fatalf("archived evidence changed: %+v %v", record, err)
	}
	if err := reopened.Create(w); !errors.Is(err, os.ErrExist) {
		t.Fatalf("archived workflow ID reused: %v", err)
	}
	if _, err := reopened.ClaimReady(w.ID, time.Second); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("archived workflow was claimable: %v", err)
	}
	if err := reopened.Cancel(w.ID, record.Revision); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("archived workflow was mutable: %v", err)
	}
}

func TestArchiveSkipsEngineeringRunsAndActiveLeases(t *testing.T) {
	s := testStore(t)
	capability := archiveSample("cap-running")
	engineering := sample()
	if err := s.Create(capability); err != nil {
		t.Fatal(err)
	}
	if err := s.Create(engineering); err != nil {
		t.Fatal(err)
	}
	claim, err := s.ClaimReady(capability.ID, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	ageArchiveCandidate(t, s, capability.ID)
	ageArchiveCandidate(t, s, engineering.ID)
	moved, err := s.ArchiveTerminal(time.Hour, 10)
	if err != nil || len(moved) != 0 {
		t.Fatalf("active or engineering run archived: %v %v", moved, err)
	}
	if err := s.Complete(claim, json.RawMessage(`{"done":true}`), ""); err != nil {
		t.Fatal(err)
	}
	ageArchiveCandidate(t, s, capability.ID)
	moved, err = s.ArchiveTerminal(time.Hour, 10)
	if err != nil || !reflect.DeepEqual(moved, []string{capability.ID}) {
		t.Fatalf("terminal capability not archived: %v %v", moved, err)
	}
	if _, err := s.Get(engineering.ID); err != nil {
		t.Fatalf("engineering record was displaced: %v", err)
	}
}

func TestArchiveWaitsForModelContinuation(t *testing.T) {
	s := testStore(t)
	w := archiveSample("cap-model")
	w.AutoReplan = true
	if err := s.Create(w); err != nil {
		t.Fatal(err)
	}
	claim, err := s.ClaimReady(w.ID, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Complete(claim, json.RawMessage(`{}`), ""); err != nil {
		t.Fatal(err)
	}
	ageArchiveCandidate(t, s, w.ID)
	moved, err := s.ArchiveTerminal(time.Hour, 10)
	if err != nil || len(moved) != 0 {
		t.Fatalf("unresolved model continuation archived: %v %v", moved, err)
	}
	current, err := s.Get(w.ID)
	if err != nil {
		t.Fatal(err)
	}
	current.PlanComplete = true
	current.Revision++
	if err := s.save(current); err != nil {
		t.Fatal(err)
	}
	ageArchiveCandidate(t, s, w.ID)
	moved, err = s.ArchiveTerminal(time.Hour, 10)
	if err != nil || len(moved) != 1 || moved[0] != w.ID {
		t.Fatalf("settled model workflow not archived: %v %v", moved, err)
	}
}

func TestArchiveRejectsUnsafeEntriesAndDirectories(t *testing.T) {
	s := testStore(t)
	if err := os.Symlink(filepath.Join(s.dir, ".lock"), filepath.Join(s.dir, "unsafe.json")); err != nil {
		t.Fatal(err)
	}
	if _, err := s.ArchiveTerminal(time.Hour, 10); err == nil {
		t.Fatal("accepted a symlink workflow entry")
	}
	if err := os.Remove(filepath.Join(s.dir, "unsafe.json")); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(s.dir, "archive")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(s.dir, filepath.Join(s.dir, "archive")); err != nil {
		t.Fatal(err)
	}
	if _, err := s.ArchiveTerminal(time.Hour, 10); err == nil {
		t.Fatal("accepted a symlink archive directory")
	}
}
