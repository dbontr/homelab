package approval

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"time"
)

const (
	PendingTTL  = 5 * time.Minute
	ApprovedTTL = 2 * time.Minute
	MaxPending  = 100
)

// Challenge binds local operator approval to the exact operation and arguments.
type Challenge struct {
	ID        string    `json:"id"`
	Operation string    `json:"operation"`
	State     string    `json:"state"`
	ExpiresAt time.Time `json:"expires_at"`
	CreatedAt time.Time `json:"created_at"`
	Summary   string    `json:"summary"`
	SHA256    string    `json:"arguments_sha256"`
	digest    [32]byte
}

type Gate struct {
	mu      sync.Mutex
	entries map[string]Challenge
	now     func() time.Time
}

func New() *Gate {
	return &Gate{entries: make(map[string]Challenge), now: time.Now}
}

func digestArguments(args json.RawMessage) ([32]byte, string, error) {
	if len(args) == 0 {
		args = []byte("{}")
	}
	var doc any
	if err := json.Unmarshal(args, &doc); err != nil {
		return [32]byte{}, "", errors.New("invalid tool arguments")
	}
	canon, err := json.Marshal(doc)
	if err != nil {
		return [32]byte{}, "", err
	}
	preview, _ := json.Marshal(redact(doc))
	if len(preview) > 2048 {
		preview = append(preview[:2048], []byte("...")...)
	}
	return sha256.Sum256(canon), string(preview), nil
}

func (g *Gate) prune(now time.Time) {
	for id, challenge := range g.entries {
		if !now.Before(challenge.ExpiresAt) {
			delete(g.entries, id)
		}
	}
}

// Check consumes one approved grant or returns a pending challenge.
func (g *Gate) Check(operation string, args json.RawMessage) (bool, Challenge, error) {
	if operation == "" {
		return false, Challenge{}, errors.New("operation required")
	}
	digest, summary, err := digestArguments(args)
	if err != nil {
		return false, Challenge{}, err
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	now := g.now().UTC()
	g.prune(now)
	for id, challenge := range g.entries {
		if challenge.Operation == operation && challenge.digest == digest {
			if challenge.State == "approved" {
				delete(g.entries, id)
				return true, Challenge{}, nil
			}
			return false, public(challenge), nil
		}
	}
	if len(g.entries) >= MaxPending {
		return false, Challenge{}, errors.New("too many pending approvals")
	}
	key := make([]byte, 20)
	if _, err := rand.Read(key); err != nil {
		return false, Challenge{}, err
	}
	challenge := Challenge{
		ID: hex.EncodeToString(key),
		Operation: operation,
		State: "pending",
		CreatedAt: now,
		ExpiresAt: now.Add(PendingTTL),
		Summary: summary,
		SHA256: hex.EncodeToString(digest[:]),
		digest: digest,
	}
	g.entries[challenge.ID] = challenge
	return false, public(challenge), nil
}

// Approve must only be called by a restricted local administration listener.
func (g *Gate) Approve(id string) (Challenge, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	now := g.now().UTC()
	g.prune(now)
	challenge, ok := g.entries[id]
	if !ok {
		return Challenge{}, errors.New("approval not found or expired")
	}
	if challenge.State != "pending" {
		return Challenge{}, errors.New("approval already granted")
	}
	challenge.State = "approved"
	challenge.ExpiresAt = now.Add(ApprovedTTL)
	g.entries[id] = challenge
	return public(challenge), nil
}

func (g *Gate) Status(id string) (Challenge, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.prune(g.now().UTC())
	challenge, ok := g.entries[id]
	if !ok {
		return Challenge{}, errors.New("approval not found or expired")
	}
	return public(challenge), nil
}

func (g *Gate) Pending() []Challenge {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.prune(g.now().UTC())
	result := make([]Challenge, 0, len(g.entries))
	for _, challenge := range g.entries {
		if challenge.State == "pending" {
			result = append(result, public(challenge))
		}
	}
	return result
}

func public(challenge Challenge) Challenge {
	challenge.digest = [32]byte{}
	return challenge
}

// ScopeFor classifies cluster-changing operations that need explicit approval.
func ScopeFor(name string) string {
	if strings.HasPrefix(name, "cluster__") || strings.HasPrefix(name, "infrastructure__") {
		return "cluster.modify"
	}
	switch name {
	case "workspace__worker_revoke", "workspace__github_account_archive":
		return "cluster.modify"
	case "workspace__pool_register_image", "workspace__pool_register_software", "workspace__pool_review_program", "workspace__pool_update_entry":
		return "software.manage"
	}
	return ""
}

func redact(v any) any {
	switch typed := v.(type) {
	case map[string]any:
		out := make(map[string]any, len(typed))
		for key, value := range typed {
			lower := strings.ToLower(key)
			if strings.Contains(lower, "password") || strings.Contains(lower, "secret") || strings.Contains(lower, "token") || strings.Contains(lower, "credential") || strings.Contains(lower, "api_key") || strings.Contains(lower, "authorization") || strings.Contains(lower, "passcode") {
				out[key] = "[REDACTED]"
			} else {
				out[key] = redact(value)
			}
		}
		return out
	case []any:
		out := make([]any, len(typed))
		for i, value := range typed {
			out[i] = redact(value)
		}
		return out
	default:
		return v
	}
}
