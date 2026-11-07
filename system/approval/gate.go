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

	"github.com/dbontr/homelab-new/system/canonicaljson"
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
	canon, doc, err := canonicaljson.Object(args)
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
    if strings.HasPrefix(name, "health__") || strings.HasPrefix(name, "learning__") {
        tool := name[strings.Index(name, "__")+2:]
        for _, prefix := range []string{"delete_", "purge_", "reset_", "admin_", "credential_", "grant_", "revoke_"} {
            if strings.HasPrefix(tool, prefix) { return "personal.data.manage" }
        }
        return ""
    }
    if !strings.HasPrefix(name, "workspace__") {
        return ""
    }
    tool := strings.TrimPrefix(name, "workspace__")
    // Routine personal operations and inspection are available without a second unlock.
    switch tool {
    case "list_projects", "project_published_library", "read_project_note",
        "search_project_knowledge", "knowledge_policy", "knowledge_git_read",
        "knowledge_context", "knowledge_contributions", "knowledge_contribution",
        "knowledge_search_contributions", "list_compute_nodes",
        "task_worker_access", "list_project_jobs", "git_provider_capabilities",
        "git_repository_head", "git_read_file", "list_github_repositories",
        "task_list", "task_details", "task_files", "task_file_info",
        "task_read_base64", "task_compute_jobs", "personal_task_claim_list",
        "personal_task_claim_status", "task_claim_list", "task_claim_status",
        "resource_plan", "workspace_list", "workspace_operation", "list_hardware",
        "environment_list", "pool_list_images", "pool_list_software",
        "pool_list_programs", "pool_entry_info", "pool_events",
        "workspace_snapshot_list", "workspace_checkpoint_stop_status",
        "external_resource_list", "external_resource_leases",
        "github_account_search", "github_account_repositories",
        "github_account_repository", "github_account_settings",
        "github_pat_readiness", "github_repository", "github_read_file",
        "github_list_issues", "github_list_pull_requests", "github_get_issue",
        "github_list_branches", "github_get_pull_request",
        "github_verify_pull_request", "list_project_artifacts",
        "artifact_info", "artifact_read_base64", "artifact_view_image",
        "github_project_board", "github_project_items", "github_project_fields",
        "repository_memberships", "research_sources", "research_search",
        "research_topics", "research_browse_topic", "research_read_passage",
        "library_list", "library_sources", "library_resolve", "library_read_file",
        "create_project", "create_library_project", "write_project_note",
        "knowledge_configure", "knowledge_contribute", "link_github_repository",
        "git_link_repository", "git_checkout_repository", "task_create",
        "task_update", "task_save_text", "task_attach_base64",
        "task_study_pack_import", "task_send_base64", "upload_begin",
        "upload_chunk", "upload_status", "upload_finish", "upload_cancel",
        "task_export_project", "study_pack_prepare", "research_register_source",
        "research_ingest_passages", "library_create", "library_add_source",
        "library_link", "library_import_object", "library_checkout_source":
        return ""
    }
    // Core infrastructure, upstream software catalogs, and GitHub account changes
    // are never authorized solely by possession of the gateway bearer token.
    switch tool {
    case "worker_revoke", "task_worker_authorize", "task_hardware_recover",
        "external_resource_register", "external_resource_reserve",
        "external_resource_release":
        return "cluster.modify"
    case "pool_register_image", "pool_register_software", "pool_propose_program",
        "pool_review_program", "pool_update_entry", "environment_define",
        "environment_define_from_pool", "environment_build",
        "environment_import", "environment_export":
        return "software.manage"
    case "workspace_call", "workspace_create", "workspace_destroy",
        "workspace_capture_display", "workspace_import_artifact",
        "workspace_snapshot", "workspace_checkpoint_stop",
        "workspace_prune_volume", "workspace_restore", "submit_compute_job",
        "task_compute_submit", "personal_task_claim", "personal_task_claim_renew",
        "personal_task_claim_finish", "task_claim", "task_claim_renew",
        "task_claim_finish":
        return "compute.execute"
    }
    if strings.HasPrefix(tool, "github_") ||
        strings.HasPrefix(tool, "github_account_") ||
        strings.HasPrefix(tool, "git_publish") ||
        strings.HasPrefix(tool, "knowledge_git_publish") {
        return "source.modify"
    }
    if strings.HasPrefix(tool, "pool_") || strings.HasPrefix(tool, "environment_") {
        return "software.manage"
    }
    if strings.HasPrefix(tool, "external_resource_") || strings.HasPrefix(tool, "worker_") {
        return "cluster.modify"
    }
    // Unrecognized workspace operations require explicit approval by default.
    return "workspace.modify"
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
