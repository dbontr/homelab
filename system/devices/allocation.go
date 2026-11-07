package devices

import (
	"encoding/json"
	"errors"
	"fmt"
)

// ValidateAllocation checks configured device eligibility before dispatch.
// The compute executor must independently enforce the actual device lease.
func (c *Catalog) ValidateAllocation(raw json.RawMessage) error {
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
	if n := request.Hardware["fpga"]; n > 0 {
		if request.NodeID == "" {
			return errors.New("FPGA allocation requires a target node")
		}
		count := 0
		if c != nil {
			for _, device := range c.items {
				if device.Kind == "fpga" && device.Node == request.NodeID && device.EligibleForCompute {
					count++
				}
			}
		}
		if count < n {
			return fmt.Errorf("FPGA allocation is not permitted by the device inventory on %s", request.NodeID)
		}
	}
	if request.USBPassthrough || request.USBDeviceID != "" || request.USBNode != "" || request.Hardware["usb"] > 0 {
		if !request.USBPassthrough || request.USBDeviceID == "" || request.USBNode == "" || request.Hardware["usb"] > 1 {
			return errors.New("USB passthrough requires an explicit device ID and node")
		}
		permitted := false
		if c != nil {
			for _, device := range c.items {
				if device.ID != request.USBDeviceID || device.Node != request.USBNode || device.Connection != "usb" || !device.EligibleForCompute {
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
