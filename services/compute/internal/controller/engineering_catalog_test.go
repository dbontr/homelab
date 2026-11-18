package controller

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestEngineeringCatalogUsesCanonicalProgramValidation(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", "catalog", "engineering-programs.json"))
	if err != nil {
		t.Fatal(err)
	}
	var proposals []ProgramProposal
	if err := json.Unmarshal(data, &proposals); err != nil {
		t.Fatal(err)
	}
	db, _, _ := fixture(t)
	for _, item := range proposals {
		result, err := db.ProposeProgram(item)
		if err != nil {
			t.Fatalf("invalid catalog program %s: %v", item.ID, err)
		}
		if result["status"] != "requested" {
			t.Fatalf("program %s was marked executable before review", item.ID)
		}
	}
	listed, err := db.Programs()
	if err != nil || len(listed) != len(proposals) {
		t.Fatalf("catalog size mismatch: %d programs, %v", len(listed), err)
	}
	if _, err := db.ProposeProgram(proposals[0]); err == nil {
		t.Fatal("catalog program identities must remain immutable")
	}
	if _, err := db.ReviewProgram("amd-vivado-2025-2", "available",
		"Vendor installation not yet qualified", "image", "missing", 1); err == nil {
		t.Fatal("catalog registration cannot bypass runtime approval")
	}
}
