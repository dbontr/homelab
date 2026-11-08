package devices

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"regexp"
	"sort"
)

type Node struct {
	ID           string `json:"id"`
	Mode         string `json:"mode"`
	Role         string `json:"role,omitempty"`
	System       string `json:"system,omitempty"`
	NodeIP       string `json:"nodeIP,omitempty"`
	LANInterface string `json:"lanInterface,omitempty"`
}

type NodeCatalog struct {
	items []Node
}

var networkInterface = regexp.MustCompile(`^[a-zA-Z][a-zA-Z0-9_.-]{0,31}$`)

func NewNodes(nodes []Node) (*NodeCatalog, error) {
	seen := make(map[string]bool, len(nodes))
	ips := make(map[string]bool, len(nodes))
	result := &NodeCatalog{items: make([]Node, 0, len(nodes))}
	for _, node := range nodes {
		if !identifier.MatchString(node.ID) || seen[node.ID] {
			return nil, fmt.Errorf("invalid or duplicate node identifier: %q", node.ID)
		}
		seen[node.ID] = true
		switch node.Mode {
		case ModeEnrolled:
			if node.Role != "controller" && node.Role != "compute" {
				return nil, fmt.Errorf("enrolled node %s requires a supported role", node.ID)
			}
			if node.System != "x86_64-linux" && node.System != "aarch64-linux" {
				return nil, fmt.Errorf("enrolled node %s requires a supported NixOS system", node.ID)
			}
			if node.Role == "compute" {
				ip := net.ParseIP(node.NodeIP)
				if ip == nil || ip.To4() == nil || ip.IsUnspecified() || ip.IsMulticast() || !networkInterface.MatchString(node.LANInterface) {
					return nil, fmt.Errorf("enrolled compute node %s requires valid network configuration", node.ID)
				}
				if ips[node.NodeIP] {
					return nil, fmt.Errorf("duplicate enrolled compute IP: %s", node.NodeIP)
				}
				ips[node.NodeIP] = true
			} else if node.NodeIP != "" || node.LANInterface != "" {
				return nil, fmt.Errorf("controller node %s must not declare compute networking", node.ID)
			}
		case ModeSSH:
			if node.Role != "" || node.System != "" || node.NodeIP != "" || node.LANInterface != "" {
				return nil, fmt.Errorf("SSH node %s cannot specify managed compute configuration", node.ID)
			}
		default:
			return nil, fmt.Errorf("invalid management mode for node %s", node.ID)
		}
		result.items = append(result.items, node)
	}
	sort.Slice(result.items, func(i, j int) bool {
		return result.items[i].ID < result.items[j].ID
	})
	return result, nil
}

func LoadNodes(path string) (*NodeCatalog, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	var nodes []Node
	if err := decoder.Decode(&nodes); err != nil {
		return nil, err
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		return nil, errors.New("node inventory must contain exactly one JSON array")
	}
	return NewNodes(nodes)
}

func (n *NodeCatalog) List() []Node {
	if n == nil {
		return []Node{}
	}
	return append([]Node(nil), n.items...)
}

// ValidateNodes binds managed device allocations to enrolled compute nodes.
func (c *Catalog) ValidateNodes(nodes *NodeCatalog) error {
	if c == nil {
		return nil
	}
	if nodes == nil {
		return errors.New("node inventory is required")
	}
	registered := make(map[string]Node, len(nodes.items))
	for _, node := range nodes.items {
		registered[node.ID] = node
	}
	for _, device := range c.items {
		if device.Mode == ModeEnrolled && device.EligibleForCompute {
			node, ok := registered[device.Node]
			if !ok || node.Mode != ModeEnrolled || node.Role != "compute" {
				return fmt.Errorf("compute device %s requires a registered compute node", device.ID)
			}
		}
	}
	return nil
}
