package devices

import (
	"encoding/json"
	"errors"
	"fmt"
)

// ValidateAllocation checks configured device eligibility before dispatch.
// The compute executor must independently enforce the actual device lease.
// ValidateAllocation checks concrete compute requests against the declared device inventory.
func (c *Catalog) ValidateAllocation(raw json.RawMessage) error {
    return c.validateAllocation(raw, false)
}

// ValidatePlan permits unpinned resource discovery without granting a lease.
// Actual allocation must still name an authorized enrolled worker.
func (c *Catalog) ValidatePlan(raw json.RawMessage) error {
    return c.validateAllocation(raw, true)
}

func (c *Catalog) validateAllocation(raw json.RawMessage, advisory bool) error {
	var request struct {
		Hardware       map[string]int `json:"hardware"`
		NodeID         string         `json:"node_id"`
		USBPassthrough bool           `json:"usb_passthrough"`
		USBDeviceID    string         `json:"usb_device_id"`
		USBNode        string         `json:"usb_node"`
	}
	if len(raw) != 0 {
		if err := json.Unmarshal(raw, &request); err != nil {
			return errors.New("invalid hardware allocation arguments")
		}
	}
	for kind, count := range request.Hardware {
		if count < 0 {
			return fmt.Errorf("hardware count for %s must not be negative", kind)
		}
		if kind != "fpga" && kind != "gpu" && kind != "npu" && kind != "usb" {
			return fmt.Errorf("unsupported hardware kind: %s", kind)
		}
	}
	if request.NodeID != "" && request.USBNode != "" && request.NodeID != request.USBNode {
		return errors.New("USB passthrough node does not match the requested worker")
	}
	if n := request.Hardware["fpga"]; n > 0 {
        if request.NodeID == "" && !advisory {
            return errors.New("FPGA allocation requires a target node")
        }
        counts := make(map[string]int)
        if c != nil {
            for _, device := range c.items {
                if device.Kind != "fpga" || device.Node == "" ||
                    device.Mode != ModeEnrolled || !device.EligibleForCompute {
                    continue
                }
                counts[device.Node]++
            }
        }
        if request.NodeID != "" {
            if counts[request.NodeID] < n {
                return fmt.Errorf("FPGA allocation is not permitted by the device inventory on %s", request.NodeID)
            }
        } else {
            eligible := false
            for _, count := range counts {
                if count >= n { eligible = true; break }
            }
            if !eligible { return errors.New("no compute node can satisfy the requested FPGA plan") }
        }
    }
	if request.USBPassthrough || request.USBDeviceID != "" || request.USBNode != "" || request.Hardware["usb"] > 0 {
		if !request.USBPassthrough || request.USBDeviceID == "" || request.USBNode == "" || request.Hardware["usb"] > 1 {
			return errors.New("USB passthrough requires an explicit device ID and node")
		}
		permitted := false
		if c != nil {
			for _, device := range c.items {
				if device.ID != request.USBDeviceID || device.Node != request.USBNode || device.Connection != "usb" || device.Mode != ModeEnrolled || !device.EligibleForCompute {
					continue
				}
				for _, operation := range device.AllowedOperations {
					if operation == "passthrough" {
						permitted = true
						break
					}
				}
			}
		}
		if !permitted {
			return errors.New("USB passthrough is not permitted by the device inventory")
		}
	}
	return nil
}
