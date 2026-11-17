package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func fixtureRecord(t *testing.T, source, dest string) Record {
	t.Helper()
	data := []byte("historical artifact bytes")
	for _, p := range []string{filepath.Join(source, "artifacts", "original"), filepath.Join(dest, "objects", "copied")} {
		if err := os.MkdirAll(filepath.Dir(p), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, data, 0600); err != nil {
			t.Fatal(err)
		}
	}
	digest := sha256.Sum256(data)
	return Record{LegacyID: "artifact:original", Revision: "v1", ProjectID: "research", Kind: "artifact", CreatedAt: time.Date(2025, 2, 1, 3, 4, 5, 0, time.UTC).Format(time.RFC3339Nano), SourcePath: "artifacts/original", RestorePath: "objects/copied", SHA256: hex.EncodeToString(digest[:]), Size: int64(len(data))}
}

func containsCode(report Report, code string) bool {
	for _, f := range report.Findings {
		if f.Code == code {
			return true
		}
	}
	return false
}

func TestReconciliationIndependentCopyAndProvenance(t *testing.T) {
	root := t.TempDir()
	source := filepath.Join(root, "source")
	dest := filepath.Join(root, "restore")
	record := fixtureRecord(t, source, dest)
	manifest := Manifest{Version: 1, Records: []Record{record}}
	report := audit(source, dest, manifest, &manifest, "digest")
	if len(report.Findings) != 0 || report.SourceVerified != 1 || report.RestoreVerified != 1 {
		t.Fatalf("valid archive rejected: %+v", report)
	}
	if err := os.WriteFile(filepath.Join(dest, "objects", "copied"), []byte("corrupt but same file size"), 0600); err != nil {
		t.Fatal(err)
	}
	report = audit(source, dest, manifest, &manifest, "digest")
	if report.RestoreVerified != 0 || !containsCode(report, "restore_unverified") {
		t.Fatalf("corruption undetected: %+v", report)
	}
}

func TestReconciliationRejectsMetadataConflictsAndDuplicateIdentity(t *testing.T) {
	root := t.TempDir()
	source := filepath.Join(root, "source")
	dest := filepath.Join(root, "restore")
	record := fixtureRecord(t, source, dest)
	original := Manifest{Version: 1, Records: []Record{record}}
	changed := record
	changed.CreatedAt = "2024-01-01T00:00:00Z"
	report := audit(source, dest, original, &Manifest{Version: 1, Records: []Record{changed}}, "digest")
	if !containsCode(report, "restored_metadata_conflict") {
		t.Fatalf("timestamp lost: %+v", report)
	}
	original.Records = append(original.Records, record)
	report = audit(source, dest, original, nil, "digest")
	if !containsCode(report, "duplicate_legacy_identity") {
		t.Fatalf("duplicate unnoticed: %+v", report)
	}
}

func TestReconciliationDetectsUnmappedMissingAndInvalidPaths(t *testing.T) {
	root := t.TempDir()
	source := filepath.Join(root, "source")
	dest := filepath.Join(root, "restore")
	record := fixtureRecord(t, source, dest)
	if err := os.WriteFile(filepath.Join(source, "other"), []byte("unmapped"), 0600); err != nil {
		t.Fatal(err)
	}
	record.Links = []string{"missing@v1"}
	report := audit(source, dest, Manifest{Version: 1, Records: []Record{record}}, nil, "digest")
	if !containsCode(report, "unmapped_legacy_file") || !containsCode(report, "unresolved_relationship") || !containsCode(report, "restore_not_audited") {
		t.Fatalf("missing data not reported: %+v", report)
	}
	if _, err := cleanRelative("../secret"); err == nil {
		t.Fatal("traversal accepted")
	}
	if _, err := cleanRelative("a/../b"); err == nil {
		t.Fatal("noncanonical path accepted")
	}
	if err := os.Remove(filepath.Join(source, "artifacts", "original")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(dest, "objects", "copied"), filepath.Join(source, "artifacts", "original")); err != nil {
		t.Fatal(err)
	}
	report = audit(source, dest, Manifest{Version: 1, Records: []Record{record}}, nil, "digest")
	if !containsCode(report, "source_unverified") {
		t.Fatalf("symlink read: %+v", report)
	}
}

func TestCLIEmitsMachineReadableAuditWithoutMutation(t *testing.T) {
	root := t.TempDir()
	source := filepath.Join(root, "source")
	dest := filepath.Join(root, "restore")
	record := fixtureRecord(t, source, dest)
	manifest := Manifest{Version: 1, Records: []Record{record}}
	raw, err := json.Marshal(manifest)
	if err != nil {
		t.Fatal(err)
	}
	one := filepath.Join(root, "one.json")
	two := filepath.Join(root, "two.json")
	if err := os.WriteFile(one, raw, 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(two, raw, 0600); err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	if err := run([]string{"--source-root", source, "--restore-root", dest, "--manifest", one, "--restored-manifest", two}, &output); err != nil {
		t.Fatal(err)
	}
	var report Report
	if err := json.Unmarshal(output.Bytes(), &report); err != nil {
		t.Fatal(err)
	}
	if report.RestoreVerified != 1 || len(report.Findings) != 0 {
		t.Fatalf("unexpected report: %+v", report)
	}
	if err := run([]string{"--source-root", source, "--restore-root", source, "--manifest", one, "--restored-manifest", two}, &bytes.Buffer{}); err == nil {
		t.Fatal("self-restore accepted")
	}
}
