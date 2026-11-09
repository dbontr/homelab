package devices

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestConfiguredManagementModes(t *testing.T) {
	ssh := Device{ID: "kv260", Kind: "fpga", Mode: ModeSSH, Connection: "ssh", AllowedOperations: []string{"connect", "inspect"}}
	catalog, err := New([]Device{ssh})
	if err != nil {
		t.Fatal(err)
	}
	items := catalog.List()
	if len(items) != 1 || items[0].Mode != ModeSSH || items[0].EligibleForCompute {
		t.Fatalf("unexpected catalog: %+v", items)
	}
	items[0].AllowedOperations[0] = "program"
	if catalog.List()[0].AllowedOperations[0] == "program" {
		t.Fatal("catalog permissions are mutable")
	}
	enrolled := Device{ID: "kv260-owned", Kind: "fpga", Mode: ModeEnrolled, Connection: "usb", Node: "venus", EligibleForCompute: true, AllowedOperations: []string{"allocate", "program"}}
	if _, err := New([]Device{enrolled}); err != nil {
		t.Fatal(err)
	}
	ssh.SSH = &SSHConfig{Host: "device.example", User: "xlnx"}
	catalog, err = New([]Device{ssh})
	if err != nil {
		t.Fatal(err)
	}
	if catalog.List()[0].SSH.Port != 22 || ssh.SSH.Port != 0 {
		t.Fatal("SSH port not defaulted independently")
	}
	catalog.List()[0].SSH.Host = "changed"
	if catalog.List()[0].SSH.Host != "device.example" {
		t.Fatal("SSH target is mutable")
	}
}

func TestManagementPolicyRejectsInvalidEntries(t *testing.T) {
	base := Device{ID: "kv260", Kind: "fpga", Mode: ModeSSH, Connection: "ssh", AllowedOperations: []string{"connect", "inspect"}}
	for _, test := range []struct {
		name string
		edit func(*Device)
	}{
		{"missing mode", func(d *Device) { d.Mode = "" }},
		{"unknown mode", func(d *Device) { d.Mode = "other" }},
		{"agentless USB", func(d *Device) { d.Connection = "usb" }},
		{"agentless lease", func(d *Device) { d.EligibleForCompute = true; d.Node = "venus"; d.AllowedOperations = append(d.AllowedOperations, "allocate") }},
		{"agentless allocate", func(d *Device) { d.AllowedOperations = append(d.AllowedOperations, "allocate") }},
		{"agentless passthrough", func(d *Device) { d.AllowedOperations = append(d.AllowedOperations, "passthrough") }},
		{"root SSH", func(d *Device) { d.SSH = &SSHConfig{Host: "device.example", User: "root"} }},
		{"invalid SSH port", func(d *Device) { d.SSH = &SSHConfig{Host: "device.example", User: "xlnx", Port: 65536} }},
		{"missing SSH user", func(d *Device) { d.SSH = &SSHConfig{Host: "device.example"} }},
		{"enrolled with SSH login", func(d *Device) { d.Mode = ModeEnrolled; d.SSH = &SSHConfig{Host: "device.example", User: "xlnx"} }},
		{"compute without node", func(d *Device) { d.Mode = ModeEnrolled; d.EligibleForCompute = true; d.AllowedOperations = append(d.AllowedOperations, "allocate") }},
	} {
		t.Run(test.name, func(t *testing.T) {
			d := base
			d.AllowedOperations = append([]string(nil), base.AllowedOperations...)
			test.edit(&d)
			if _, err := New([]Device{d}); err == nil {
				t.Fatalf("invalid entry accepted: %+v", d)
			}
		})
	}
}

func TestDeclarativeInventoryCanBeRemoved(t *testing.T) {
	path := filepath.Join(t.TempDir(), "devices.json")
	encoded, err := json.Marshal([]Device{{ID: "kv260", Kind: "fpga", Mode: ModeSSH, Connection: "ssh", AllowedOperations: []string{"connect", "inspect"}}})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, encoded, 0600); err != nil {
		t.Fatal(err)
	}
	catalog, err := Load(path)
	if err != nil || len(catalog.List()) != 1 {
		t.Fatalf("load: %v", err)
	}
	if err := os.WriteFile(path, []byte("[]"), 0600); err != nil {
		t.Fatal(err)
	}
	catalog, err = Load(path)
	if err != nil || len(catalog.List()) != 0 {
		t.Fatalf("remove: %v", err)
	}
	if _, err := New([]Device{{ID: "dup", Mode: ModeEnrolled, Kind: "other", Connection: "local"}, {ID: "dup", Mode: ModeEnrolled, Kind: "other", Connection: "local"}}); err == nil {
		t.Fatal("accepted duplicate")
	}
	for _, raw := range []string{
		`[{"id":"kv260","kind":"fpga","mode":"ssh","connection":"ssh","ownership":"borrowed"}]`,
		"[] []",
	} {
		if err := os.WriteFile(path, []byte(raw), 0600); err != nil {
			t.Fatal(err)
		}
		if _, err := Load(path); err == nil {
			t.Fatalf("invalid inventory accepted: %s", raw)
		}
	}
}
