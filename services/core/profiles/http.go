package profiles

import (
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"strconv"

	"github.com/dbontr/homelab-new/system/internal/httpjson"
)

type profileInput struct {
	Spec            Spec `json:"spec"`
	ExpectedVersion int  `json:"expected_version"`
}

type handler struct {
	registry *Registry
}

// Register binds profile resources to the authenticated service router.
// The caller owns admission; all callers share the same registry.
func Register(mux *http.ServeMux, registry *Registry) {
	if mux == nil {
		panic("profile routes require a router")
	}
	h := &handler{registry: registry}
	mux.HandleFunc("GET /v1/compute/profiles", h.serve)
	mux.HandleFunc("GET /v1/compute/profiles/{id}", h.serve)
	mux.HandleFunc("PUT /v1/compute/profiles/{id}", h.serve)
}

func (h *handler) serve(w http.ResponseWriter, r *http.Request) {
	if h.registry == nil {
		httpjson.Error(w, http.StatusServiceUnavailable, "compute profiles are not configured")
		return
	}
	id := r.PathValue("id")
	if r.Method == http.MethodGet && id == "" {
		httpjson.Write(w, http.StatusOK, h.registry.List())
		return
	}
	if r.Method == http.MethodGet {
		version := 0
		if value := r.URL.Query().Get("version"); value != "" {
			var err error
			version, err = strconv.Atoi(value)
			if err != nil || version < 0 {
				httpjson.Error(w, http.StatusBadRequest, "invalid profile version")
				return
			}
		}
		profile, err := h.registry.Get(id, version)
		switch {
		case errors.Is(err, os.ErrNotExist):
			httpjson.Error(w, http.StatusNotFound, "profile not found")
		case err != nil:
			httpjson.Error(w, http.StatusInternalServerError, "profile retrieval failed")
		default:
			httpjson.Write(w, http.StatusOK, profile)
		}
		return
	}

	var input profileInput
	if err := httpjson.Decode(r, &input); err != nil {
		httpjson.Error(w, http.StatusBadRequest, err.Error())
		return
	}
	if input.Spec.ID != id {
		httpjson.Error(w, http.StatusBadRequest, "profile identifier does not match URL")
		return
	}
	profile, err := h.registry.Save(input.Spec, input.ExpectedVersion)
	if err != nil {
		httpjson.Error(w, http.StatusBadRequest, err.Error())
		return
	}
	httpjson.Write(w, http.StatusOK, profile)
}

// Execute handles the profile operations shared by native tools and MCP.
// Resource planning remains in the orchestrator because it calls Workspace.
func Execute(registry *Registry, name string, raw json.RawMessage) (any, error) {
	if registry == nil {
		return nil, errors.New("compute profiles are not configured")
	}
	switch name {
	case "compute_profile_list":
		return registry.List(), nil
	case "compute_profile_get":
		var input struct {
			ID      string `json:"id"`
			Version int    `json:"version"`
		}
		if err := json.Unmarshal(raw, &input); err != nil {
			return nil, err
		}
		return registry.Get(input.ID, input.Version)
	case "compute_profile_save":
		var input profileInput
		if err := json.Unmarshal(raw, &input); err != nil {
			return nil, err
		}
		return registry.Save(input.Spec, input.ExpectedVersion)
	default:
		return nil, errors.New("unknown profile operation")
	}
}
