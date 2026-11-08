package transport

import (
	"errors"
	"io"
	"net/http"

	"github.com/dbontr/homelab-new/system/recipes"
)

func (s *Server) recipeBundle(w http.ResponseWriter, r *http.Request) {
	if s.recipes == nil {
		writeError(w, http.StatusServiceUnavailable, "recipe storage is not configured")
		return
	}
	if r.Method == http.MethodGet {
		archive, err := s.recipes.ExportBundle()
		if err != nil {
			if errors.Is(err, recipes.ErrBundleTooLarge) {
				writeError(w, http.StatusRequestEntityTooLarge, "recipe image archive exceeds 96 MiB")
			} else {
				writeError(w, http.StatusInternalServerError, "recipe archive could not be exported")
			}
			return
		}
		w.Header().Set("Content-Type", "application/zip")
		w.Header().Set("Content-Disposition", "attachment; filename=\"recipes-with-images.zip\"")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(archive)
		return
	}
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, recipes.MaxPortableBundleBytes))
	if err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			writeError(w, http.StatusRequestEntityTooLarge, "recipe image archive exceeds 96 MiB")
		} else {
			writeError(w, http.StatusBadRequest, "could not read recipe image archive")
		}
		return
	}
	count, err := s.recipes.ImportBundle(body)
	switch {
	case errors.Is(err, recipes.ErrBundleTooLarge):
		writeError(w, http.StatusRequestEntityTooLarge, "recipe image archive exceeds 96 MiB")
	case errors.Is(err, recipes.ErrConflict):
		writeError(w, http.StatusConflict, "recipe archive contains existing records")
	case err != nil:
		writeError(w, http.StatusUnprocessableEntity, "invalid recipe image archive")
	default:
		writeJSON(w, http.StatusCreated, map[string]int{"imported":count})
	}
}
