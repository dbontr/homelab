package transport

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/dbontr/homelab-new/platform/approval"
)

// AdminHandler must only be served over an operator-restricted Unix socket.
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

func (s *Server) checkPrivilege(name string, args json.RawMessage) (map[string]any, bool) {
	scope := approval.ScopeFor(name)
	if scope == "" {
		return nil, true
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
		"capability": scope,
		"challenge": challenge,
		"instructions": "Approve this exact operation through the protected local administrator interface and retry with identical arguments. Do not enter administrator credentials in chat.",
	}), false
}

func (s *Server) privilegeStatus(id string) (any, error) {
	if id == "" {
		return nil, errors.New("approval ID required")
	}
	return s.gate.Status(id)
}
