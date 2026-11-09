package transport

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/dbontr/homelab-new/system/approval"
)

// AdminHandler is retained only for installations using the local legacy gate.
// It must never be served when the independent security socket is configured.
func (s *Server) AdminHandler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /approvals", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, s.gate.Pending())
	})
	mux.HandleFunc("POST /approvals/{id}", func(w http.ResponseWriter, r *http.Request) {
		if r.ContentLength > 0 {
			writeError(w, http.StatusBadRequest, "approval does not accept a body")
			return
		}
		challenge, err := s.gate.Approve(r.PathValue("id"))
		if err != nil {
			writeError(w, http.StatusNotFound, "approval not found or expired")
			return
		}
		writeJSON(w, http.StatusOK, challenge)
	})
	return mux
}

// Privilege targets are derived from canonical tool arguments, never from a
// model-provided independent security decision.
func privilegeTarget(name string, args json.RawMessage) string {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(args, &fields); err != nil {
		return "platform"
	}
	priority := []string{"target", "node_id", "device_id", "project_id", "repository", "host", "id"}
	if strings.HasPrefix(name, "workspace__github_") || strings.HasPrefix(name, "workspace__git_") {
		priority = []string{"repository", "project_id", "target"}
	} else if strings.HasPrefix(name, "workspace__") {
		priority = []string{"project_id", "node_id", "device_id", "repository", "target"}
	} else if strings.HasPrefix(name, "cluster__") || strings.HasPrefix(name, "infrastructure__") {
		priority = []string{"host", "node_id", "target", "device_id"}
	}
	for _, key := range priority {
		raw, ok := fields[key]
		if !ok {
			continue
		}
		var value string
		if err := json.Unmarshal(raw, &value); err == nil && value != "" {
			return value
		}
	}
	return "platform"
}

func (s *Server) checkPrivilege(name string,args json.RawMessage)(map[string]any,bool){
 return s.checkPrivilegeContext(context.Background(),name,args)
}
func (s *Server) checkPrivilegeContext(ctx context.Context,name string,args json.RawMessage)(map[string]any,bool){
	scope := approval.ScopeFor(name)
	if scope == "" {
		return nil, true
	}
	if s.privilegeClient != nil {
		ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
		defer cancel()
		decision, err := s.privilegeClient.Authorize(ctx, name, privilegeTarget(name, args), args)
		if err != nil {
			return toolError("independent security authorization unavailable"), false
		}
		if decision.Allowed {
			return nil, true
		}
		return result(map[string]any{
			"authorization_required":  true,
			"capability":              scope,
			"reason":                  decision.Reason,
			"confirmation_id":         decision.ConfirmationID,
			"confirmation_expires_at": decision.ConfirmationExpiresAt,
			"instructions":            "Authenticate with the trusted local homelab-admin program. For destructive operations, review and confirm the listed operation locally; then retry the same arguments. Never enter passwords or OTPs in chat.",
		}), false
	}
	granted, challenge, err := s.gate.Check(name, args)
	if err != nil {
		return toolError(err.Error()), false
	}
	if granted {
		return nil, true
	}
	return result(map[string]any{
		"approval_required": true,
		"capability":        scope,
		"challenge":         challenge,
		"instructions":      "Approve the exact operation through the protected local administrator interface and retry with identical arguments. Do not enter administrator credentials in chat.",
	}), false
}

func (s *Server) privilegeStatus(id string) (any, error) {
	if id == "" {
		return nil, errors.New("approval ID required")
	}
	return s.gate.Status(id)
}
