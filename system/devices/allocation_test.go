package devices

import (
	"encoding/json"
	"testing"
)

func TestBorrowedFPGAExcludedFromCompute(t *testing.T) {
	catalog, err := New([]Device{{ID: "kv260", Kind: "fpga", Ownership: "borrowed", Connection: "ssh", AllowedOperations: []string{"connect", "inspect"}}})
	if err != nil {
		t.Fatal(err)
	}
	requests := []string{
		`{"hardware":{"fpga":1}}`,
		`{"usb_passthrough":true}`,
		`{"usb_device_id":"device-1"}`,
		`{"hardware":{"usb":1}}`,
	}
	for _, req := range requests {
		if err := catalog.ValidateAllocation(json.RawMessage(req)); err == nil {
			t.Fatalf("allowed %s", req)
		}
	}
	if err := catalog.ValidateAllocation(json.RawMessage(`{"hardware":{"gpu":1}}`)); err != nil {
		t.Fatalf("GPU incorrectly denied: %v", err)
	}
	if err := catalog.ValidateAllocation(nil); err != nil {
		t.Fatal(err)
	}
}
