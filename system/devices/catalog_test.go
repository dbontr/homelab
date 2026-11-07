package devices

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestDevicePolicyIsConfigured(t *testing.T) {
	borrowed := Device{ID: "kv260", Kind: "fpga", Ownership: "borrowed", Connection: "ssh", AllowedOperations: []string{"connect", "inspect"}}
	catalog, err := New([]Device{borrowed})
	if err != nil { t.Fatal(err) }
	entries := catalog.List()
	if len(entries) != 1 || entries[0].Connection != "ssh" || entries[0].EligibleForCompute { t.Fatalf("unexpected catalog: %+v", entries) }
	entries[0].AllowedOperations[0] = "flash"
	if catalog.List()[0].AllowedOperations[0] == "flash" { t.Fatal("catalog list is mutable") }

	owned := Device{ID: "kv260-owned", Kind: "fpga", Ownership: "owned", Connection: "usb", Node: "venus", EligibleForCompute: true, AllowedOperations: []string{"connect", "inspect", "allocate", "program"}}
	if _, err := New([]Device{owned}); err != nil { t.Fatalf("valid configurable device rejected: %v", err) }
	borrowed.Connection = "usb"
	borrowed.Node = "venus"
	borrowed.EligibleForCompute = true
	borrowed.AllowedOperations = []string{"allocate", "passthrough"}
	if _, err := New([]Device{borrowed}); err != nil { t.Fatalf("policy is hardcoded to ownership: %v", err) }
	for _, changed := range []Device{
		{ID: "kv260", Kind: "fpga", Ownership: "borrowed", Connection: "ssh", EligibleForCompute: true},
		{ID: "kv260", Kind: "fpga", Ownership: "owned", Connection: "usb", Node: "bad_node"},
	} { if _, err := New([]Device{changed}); err == nil { t.Fatalf("invalid config accepted: %+v", changed) } }
}

func TestDeclarativeInventoryCanBeRemoved(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "devices.json")
	data, err := json.Marshal([]Device{{ID: "kv260", Kind: "fpga", Ownership: "borrowed", Connection: "ssh", AllowedOperations: []string{"connect", "inspect"}}})
	if err != nil { t.Fatal(err) }
	if err := os.WriteFile(p, data, 0600); err != nil { t.Fatal(err) }
	catalog, err := Load(p)
	if err != nil || len(catalog.List()) != 1 { t.Fatalf("load: %v", err) }
	if err := os.WriteFile(p, []byte("[]"), 0600); err != nil { t.Fatal(err) }
	catalog, err = Load(p)
	if err != nil || len(catalog.List()) != 0 { t.Fatalf("remove: %v", err) }
	if _, err := New([]Device{{ID: "duplicate", Kind: "other", Ownership: "owned", Connection: "local"}, {ID: "duplicate", Kind: "other", Ownership: "owned", Connection: "local"}}); err == nil { t.Fatal("accepted duplicate") }
}
