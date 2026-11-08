package transport

import "net/http"

func (s *Server) recipeHistory(w http.ResponseWriter, r *http.Request) {
	if s.recipes == nil {
		writeError(w, http.StatusServiceUnavailable, "recipe storage is not configured")
		return
	}
	versions, err := s.recipes.History(r.PathValue("id"))
	if err != nil {
		recipeFailure(w, err)
		return
	}
	writeJSON(w, http.StatusOK, versions)
}

func (s *Server) recipeRestore(w http.ResponseWriter, r *http.Request) {
	if s.recipes == nil {
		writeError(w, http.StatusServiceUnavailable, "recipe storage is not configured")
		return
	}
	var request struct {
		SourceRevision int `json:"source_revision"`
		ExpectedRevision int `json:"expected_revision"`
	}
	if err := decode(r, &request); err != nil {
		writeError(w, http.StatusBadRequest, "invalid recipe restore request")
		return
	}
	version, err := s.recipes.Restore(r.PathValue("id"), request.SourceRevision, request.ExpectedRevision)
	if err != nil {
		recipeFailure(w, err)
		return
	}
	writeJSON(w, http.StatusOK, version)
}

