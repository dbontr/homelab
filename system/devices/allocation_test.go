package devices

import (
	"encoding/json"
	"testing"
)

func TestFPGAAllocationFollowsInventory(t *testing.T) {
	kv := Device{ID: "kv260", Kind: "fpga", Ownership: "borrowed", Connection: "ssh", AllowedOperations: []string{"connect", "inspect"}}
	catalog, err := New([]Device{kv})
	if err != nil { t.Fatal(err) }
	for _, request := range []string{
		`{"hardware":{"fpga":1},"node_id":"venus"}`,
		`{"hardware":{"fpga":1}}`,
	} { if err := catalog.ValidateAllocation(json.RawMessage(request)); err == nil { t.Fatalf("disallowed FPGA: %s", request) } }
	owned := Device{ID: "kv260-owned", Kind: "fpga", Ownership: "owned", Connection: "usb", Node: "venus", EligibleForCompute: true, AllowedOperations: []string{"allocate", "program"}}
	catalog, err = New([]Device{kv, owned})
	if err != nil { t.Fatal(err) }
	if err := catalog.ValidateAllocation(json.RawMessage(`{"hardware":{"fpga":1},"node_id":"venus"}`)); err != nil { t.Fatal(err) }
	for _, request := range []string{`{"hardware":{"fpga":2},"node_id":"venus"}`, `{"hardware":{"fpga":1},"node_id":"janus"}`} {if err := catalog.ValidateAllocation(json.RawMessage(request)); err == nil {t.Fatalf("unauthorized: %s", request)}}
	if err := catalog.ValidateAllocation(json.RawMessage(`{"hardware":{"gpu":1}}`)); err != nil { t.Fatal(err) }
}

func TestUSBPassthroughRequiresDevicePolicy(t *testing.T) {
	usb := Device{ID: "usb-1", Kind: "other", Ownership: "owned", Connection: "usb", Node: "janus", EligibleForCompute: true, AllowedOperations: []string{"allocate", "passthrough"}}
	catalog, err := New([]Device{usb})
	if err != nil { t.Fatal(err) }
	if err := catalog.ValidateAllocation(json.RawMessage(`{"usb_passthrough":true,"usb_device_id":"usb-1","usb_node":"janus"}`)); err != nil { t.Fatal(err) }
	for _, req := range []string{`{"usb_passthrough":true,"usb_device_id":"unknown","usb_node":"janus"}`, `{"usb_passthrough":true,"usb_device_id":"usb-1","usb_node":"venus"}`, `{"hardware":{"usb":1}}`} { if err := catalog.ValidateAllocation(json.RawMessage(req)); err == nil {t.Fatalf("unexpected USB permit: %s",req)} }
	if err := catalog.ValidateAllocation(nil); err != nil { t.Fatal(err) }
}

func TestAllocationRejectsMalformedHardwareRequests(t *testing.T) {
	catalog, err := New([]Device{
		{ID: "usb-1", Kind: "other", Ownership: "owned", Connection: "usb", Node: "janus", EligibleForCompute: true, AllowedOperations: []string{"allocate", "passthrough"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, request := range []string{
		`{"hardware":{"fpga":-1}}`,
		`{"hardware":{"gpu":-1}}`,
		`{"hardware":{"usb":-1}}`,
		`{"hardware":{"unknown":1}}`,
		`{"hardware":{"fpga":1.5}}`,
		`{"node_id":"venus","usb_passthrough":true,"usb_device_id":"usb-1","usb_node":"janus"}`,
	} {
		if err := catalog.ValidateAllocation(json.RawMessage(request)); err == nil {
			t.Fatalf("invalid hardware allocation accepted: %s", request)
		}
	}
	if err := catalog.ValidateAllocation(json.RawMessage(`{"node_id":"janus","usb_passthrough":true,"usb_device_id":"usb-1","usb_node":"janus"}`)); err != nil {
		t.Fatalf("matching worker and authorized USB device rejected: %v", err)
	}
}
