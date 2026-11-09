package devices

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"regexp"
	"sort"
)

const (
	ModeSSH      = "ssh"
	ModeEnrolled = "enrolled"
)

// SSHConfig identifies an ordinary user login. Authentication and host-key
// verification belong to the separately authorized connection executor.
type SSHConfig struct {
	Host string `json:"host"`
	User string `json:"user"`
	Port int    `json:"port,omitempty"`
}

type Device struct {
	ID                 string     `json:"id"`
	Kind               string     `json:"kind"`
	Mode               string     `json:"mode"`
	Connection         string     `json:"connection"`
	Node               string     `json:"node,omitempty"`
	SSH                *SSHConfig `json:"ssh,omitempty"`
	AllowedOperations  []string   `json:"allowedOperations"`
	EligibleForCompute bool       `json:"eligibleForCompute"`
}

type Catalog struct{ items []Device }

var identifier = regexp.MustCompile(`^[a-z][a-z0-9-]{0,62}$`)
var sshHost = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:-]{0,252}$`)
var sshUser = regexp.MustCompile(`^[a-z_][a-z0-9_-]{0,31}$`)

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
		if d.Mode != ModeSSH && d.Mode != ModeEnrolled {
			return nil, fmt.Errorf("invalid management mode for %s", d.ID)
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
		if d.Mode == ModeSSH {
			if d.Connection != "ssh" {
				return nil, fmt.Errorf("SSH device %s requires an SSH connection", d.ID)
			}
			if d.EligibleForCompute || operations["allocate"] || operations["passthrough"] {
				return nil, fmt.Errorf("SSH device %s cannot receive managed compute leases", d.ID)
			}
		}
		if d.SSH != nil {
			if d.Mode != ModeSSH {
				return nil, fmt.Errorf("SSH login is not applicable to enrolled device %s", d.ID)
			}
			ssh := *d.SSH
			if !sshHost.MatchString(ssh.Host) || !sshUser.MatchString(ssh.User) || ssh.User == "root" {
				return nil, fmt.Errorf("invalid unprivileged SSH target for %s", d.ID)
			}
			if ssh.Port == 0 {
				ssh.Port = 22
			}
			if ssh.Port < 1 || ssh.Port > 65535 {
				return nil, fmt.Errorf("invalid SSH port for %s", d.ID)
			}
			d.SSH = &ssh
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
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	var items []Device
	if err := decoder.Decode(&items); err != nil {
		return nil, err
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		return nil, errors.New("device inventory must contain one JSON array")
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
		if d.SSH != nil {
			ssh := *d.SSH
			d.SSH = &ssh
		}
		out[i] = d
	}
	return out
}
