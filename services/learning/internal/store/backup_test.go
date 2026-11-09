package store

import (
	"encoding/base64"
	"os"
	"path/filepath"
	"testing"
)

func TestDatabaseSnapshotPreservesPrivateKnowledgeAndOriginals(t *testing.T) {
	dir := t.TempDir()
	s, err := Open(filepath.Join(dir, "learning.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	good := func(v Value, e error) Value {
		t.Helper()
		if e != nil {
			t.Fatal(e)
		}
		return v
	}
	good(s.AddConcept("schematics", "Schematics", "Physical circuit diagrams", "engineering"))
	session := good(s.StartLearningSession("Interpret a real circuit", "schematics", "learn", nil))
	original := []byte("\x89PNG\r\n\x1a\n" + "original-binary-data")
	evidence := good(s.UploadAttachment("session", session["id"].(string), "circuit.png", "image/png", base64.StdEncoding.EncodeToString(original), "Circuit notes"))
	out := filepath.Join(dir, "snapshot.db")
	if err := s.Backup(out); err != nil {
		t.Fatal(err)
	}
	if mode, err := os.Stat(out); err != nil || mode.Mode().Perm() != 0600 {
		t.Fatalf("backup permissions %v %v", mode, err)
	}
	if err := s.Backup(out); err == nil {
		t.Fatal("backup overwrote existing snapshot")
	}
	if err := s.Backup(filepath.Join(dir, "learning.db")); err == nil {
		t.Fatal("backup was allowed to replace the live database")
	}
	good(s.AddConcept("logic", "Logic", "Deduction", "mathematics"))
	snapshot, err := Open(out)
	if err != nil {
		t.Fatal(err)
	}
	defer snapshot.Close()
	got := good(snapshot.GetAttachment(evidence["id"].(string), true))
	if got["base64"] != base64.StdEncoding.EncodeToString(original) {
		t.Fatal("original image lost from backup")
	}
	if _, err := snapshot.Concept("schematics"); err != nil {
		t.Fatal(err)
	}
	if _, err := snapshot.Concept("logic"); err == nil {
		t.Fatal("backup did not preserve time-of-snapshot consistency")
	}
}
