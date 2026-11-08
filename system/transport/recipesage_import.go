package transport

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"

	"github.com/dbontr/homelab-new/system/recipes"
)

const recipeSageRequestLimit = (16 << 20)

type recipeSageRequest struct {
	Source string   `json:"source"`
	Digest string   `json:"digest,omitempty"`
	Keys   []string `json:"keys,omitempty"`
	Overrides map[string]recipes.Recipe `json:"overrides,omitempty"`
}

func decodeRecipeSageRequest(w http.ResponseWriter, r *http.Request) (recipeSageRequest, error) {
	var request recipeSageRequest
	defer r.Body.Close()
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, recipeSageRequestLimit))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&request); err != nil {
		return request, err
	}
	var excess any
	if err := decoder.Decode(&excess); !errors.Is(err, io.EOF) {
		return request, errors.New("request must contain one JSON object")
	}
	if len(request.Source) == 0 || len(request.Source) > recipes.MaxRecipeSageExportBytes {
		return request, errors.New("RecipeSage export exceeds 8 MiB or is empty")
	}
	return request, nil
}

func (s *Server) recipeSagePreview(w http.ResponseWriter, r *http.Request) {
	if s.recipes == nil {
		writeError(w, http.StatusServiceUnavailable, "recipe storage is not configured")
		return
	}
	request, err := decodeRecipeSageRequest(w, r)
	if err != nil || request.Digest != "" || len(request.Keys) > 0 || len(request.Overrides) > 0 {
		writeError(w, http.StatusBadRequest, "invalid RecipeSage preview request")
		return
	}
	preview, err := s.recipes.PreviewRecipeSage([]byte(request.Source))
	if err != nil {
		writeError(w, http.StatusUnprocessableEntity, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, preview)
}

func (s *Server) recipeSageImport(w http.ResponseWriter, r *http.Request) {
	if s.recipes == nil {
		writeError(w, http.StatusServiceUnavailable, "recipe storage is not configured")
		return
	}
	request, err := decodeRecipeSageRequest(w, r)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid RecipeSage import request")
		return
	}
	result, err := s.recipes.ImportRecipeSage([]byte(request.Source), request.Digest, request.Keys, request.Overrides)
	if err != nil {
		if errors.Is(err, recipes.ErrConflict) {
			writeError(w, http.StatusConflict, "one or more selected recipes have already been imported")
		} else {
			writeError(w, http.StatusUnprocessableEntity, err.Error())
		}
		return
	}
	writeJSON(w, http.StatusCreated, result)
}
