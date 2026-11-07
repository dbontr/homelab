package devices

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"regexp"
	"sort"
)

type Device struct {
	ID                 string   `json:"id"`
	Kind               string   `json:"kind"`
	Ownership          string   `json:"ownership"`
	Connection         string   `json:"connection"`
	Node               string   `json:"node,omitempty"`
	AllowedOperations  []string `json:"allowedOperations"`
	EligibleForCompute bool     `json:"eligibleForCompute"`
}

type Catalog struct{ items []Device }

var identifier = regexp.MustCompile(`^[a-z][a-z0-9-]{0,62}$`)

func New(items []Device) (*Catalog, error) {
	known := map[string]bool{}
	result := &Catalog{items: make([]Device, 0, len(items))}
	for _, d := range items {
		if !identifier.MatchString(d.ID) || known[d.ID] {
			return nil, fmt.Errorf("invalid or duplicate device identifier: %q", d.ID)
		}
		known[d.ID] = true
		if d.Kind != "fpga" && d.Kind != "gpu" && d.Kind != "npu" && d.Kind != "other" {
			return nil, fmt.Errorf("unsupported device kind for %s", d.ID)
		}
		if d.Ownership != "owned" && d.Ownership != "borrowed" {
			return nil, fmt.Errorf("invalid device ownership for %s", d.ID)
		}
		if d.Connection != "ssh" && d.Connection != "usb" && d.Connection != "pcie" && d.Connection != "local" {
			return nil, fmt.Errorf("invalid device connection for %s", d.ID)
		}
		if d.Node != "" && !identifier.MatchString(d.Node) {
			return nil, fmt.Errorf("invalid device node for %s", d.ID)
		}
		if len(d.AllowedOperations) > 16 {
			return nil, errors.New("too many device permissions")
		}
		operations := make(map[string]bool, len(d.AllowedOperations))
		for _, op := range d.AllowedOperations {
			if !identifier.MatchString(op) || operations[op] {
				return nil, fmt.Errorf("invalid or repeated operation on %s", d.ID)
			}
			operations[op] = true
		}
		if d.EligibleForCompute && (d.Node == "" || !operations["allocate"]) {
			return nil, fmt.Errorf("compute-eligible device %s requires a node and allocate permission", d.ID)
		}
		d.AllowedOperations = append([]string(nil), d.AllowedOperations...)
		result.items = append(result.items, d)
	}
	sort.Slice(result.items, func(i, j int) bool { return result.items[i].ID < result.items[j].ID })
	return result, nil
}

func Load(path string) (*Catalog, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var items []Device
	if err := json.Unmarshal(raw, &items); err != nil {
		return nil, err
	}
	return New(items)
}

func (c *Catalog) List() []Device {
	if c == nil {
		return []Device{}
	}
	out := make([]Device, len(c.items))
	for i, d := range c.items {
		d.AllowedOperations = append([]string(nil), d.AllowedOperations...)
		out[i] = d
	}
	return out
}
