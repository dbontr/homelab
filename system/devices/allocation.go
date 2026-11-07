package devices

import (
	"encoding/json"
	"errors"
)

// ValidateAllocation denies direct FPGA and USB assignments unless a device
// is explicitly eligible for compute. Borrowed SSH-only equipment cannot be
// assigned to a workspace, even when visible in the device inventory.
func (c *Catalog) ValidateAllocation(raw json.RawMessage) error {
	var request struct {
		Hardware       map[string]int `json:"hardware"`
		USBPassthrough bool           `json:"usb_passthrough"`
		USBDeviceID    string         `json:"usb_device_id"`
		USBNode        string         `json:"usb_node"`
	}
	if len(raw) != 0 {
		if err := json.Unmarshal(raw, &request); err != nil {
			return errors.New("invalid hardware allocation arguments")
		}
	}
	if request.USBPassthrough || request.USBDeviceID != "" || request.USBNode != "" || request.Hardware["usb"] > 0 {
		return errors.New("USB devices are not available for automatic assignment")
	}
	if request.Hardware["fpga"] > 0 {
		approved := 0
		if c != nil {
			for _, d := range c.items {
				if d.Kind == "fpga" && d.Ownership == "owned" && d.EligibleForCompute {
					approved++
				}
			}
		}
		if approved < request.Hardware["fpga"] {
			return errors.New("FPGA allocation requires explicitly approved owned hardware")
		}
	}
	return nil
}
