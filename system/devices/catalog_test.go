package devices

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestBorrowedKV260IsSSHOnly(t *testing.T) {
	kv := Device{ID: "kv260", Kind: "fpga", Ownership: "borrowed", Connection: "ssh", AllowedOperations: []string{"connect", "inspect"}}
	catalog, err := New([]Device{kv})
	if err != nil {
		t.Fatal(err)
	}
	entries := catalog.List()
	if len(entries) != 1 || entries[0].Connection != "ssh" || entries[0].EligibleForCompute {
		t.Fatalf("unexpected catalog: %+v", entries)
	}
	entries[0].AllowedOperations[0] = "flash"
	if catalog.List()[0].AllowedOperations[0] == "flash" {
		t.Fatal("catalog list is mutable")
	}
	for _, changed := range []Device{
		{ID: "kv260", Kind: "fpga", Ownership: "borrowed", Connection: "usb"},
		{ID: "kv260", Kind: "fpga", Ownership: "borrowed", Connection: "ssh", EligibleForCompute: true},
		{ID: "kv260", Kind: "fpga", Ownership: "borrowed", Connection: "ssh", AllowedOperations: []string{"flash"}},
		{ID: "kv260", Kind: "fpga", Ownership: "borrowed", Connection: "ssh", AllowedOperations: []string{"jtag"}},
	} {
		if _, err := New([]Device{changed}); err == nil {
			t.Fatalf("unexpected permission: %+v", changed)
		}
	}
}

func TestLoadsDeclarativeDevices(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "devices.json")
	data, err := json.Marshal([]Device{{ID: "kv260", Kind: "fpga", Ownership: "borrowed", Connection: "ssh", AllowedOperations: []string{"connect", "inspect"}}})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, data, 0600); err != nil {
		t.Fatal(err)
	}
	catalog, err := Load(p)
	if err != nil || len(catalog.List()) != 1 {
		t.Fatalf("load: %v", err)
	}
	if _, err := New([]Device{{ID: "duplicate", Kind: "other", Ownership: "owned", Connection: "local"}, {ID: "duplicate", Kind: "other", Ownership: "owned", Connection: "local"}}); err == nil {
		t.Fatal("accepted duplicate")
	}
}
