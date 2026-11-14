package transport

import (
    "net/http"
    "strings"
)

var learningPhase = map[string]bool{
    "diagnose": true, "plan": true, "teach": true,
    "practice": true, "reassess": true, "complete": true,
}

// Learning-session mutations are owner-authored, revision-fenced requests.
// Grading and evidence judgment remain in the dedicated Learning service.
func ownerLearningSessionWritePath(path string) bool {
    parts := strings.Split(path, "/")
    return len(parts) == 6 && parts[1] == "v1" && parts[2] == "learning" &&
        parts[3] == "sessions" && learningID.MatchString(parts[4]) &&
        (parts[5] == "plan" || parts[5] == "phase")
}

func (s *Server) uiLearningNext(w http.ResponseWriter, r *http.Request) {
    id := r.PathValue("id")
    if !learningID.MatchString(id) {
        writeError(w, http.StatusBadRequest, "invalid learning session identifier")
        return
    }
    s.learningCall(w, r, "next_learning_action", map[string]string{"session_id": id}, http.StatusOK)
}

func (s *Server) uiLearningProbe(w http.ResponseWriter, r *http.Request) {
    id := r.PathValue("id")
    if !learningID.MatchString(id) {
        writeError(w, http.StatusBadRequest, "invalid learning session identifier")
        return
    }
    s.learningCall(w, r, "recommend_session_probe", map[string]string{"session_id": id}, http.StatusOK)
}

func (s *Server) uiLearningPlan(w http.ResponseWriter, r *http.Request) {
    id := r.PathValue("id")
    if !learningID.MatchString(id) {
        writeError(w, http.StatusBadRequest, "invalid learning session identifier")
        return
    }
    r.Body = http.MaxBytesReader(w, r.Body, 1024)
    var request struct {
        ExpectedRevision *int `json:"expected_revision"`
    }
    if err := decode(r, &request); err != nil || request.ExpectedRevision == nil ||
        *request.ExpectedRevision < 0 {
        writeError(w, http.StatusBadRequest, "expected nonnegative learning session revision")
        return
    }
    s.learningCall(w, r, "build_learning_plan", map[string]any{
        "session_id": id, "expected_revision": *request.ExpectedRevision,
    }, http.StatusOK)
}

func (s *Server) uiLearningPhase(w http.ResponseWriter, r *http.Request) {
    id := r.PathValue("id")
    if !learningID.MatchString(id) {
        writeError(w, http.StatusBadRequest, "invalid learning session identifier")
        return
    }
    r.Body = http.MaxBytesReader(w, r.Body, 1024)
    var request struct {
        ExpectedRevision *int `json:"expected_revision"`
        NextPhase string `json:"next_phase"`
    }
    if err := decode(r, &request); err != nil || request.ExpectedRevision == nil ||
        *request.ExpectedRevision < 0 || !learningPhase[request.NextPhase] {
        writeError(w, http.StatusBadRequest, "invalid phase or missing learning session revision")
        return
    }
    s.learningCall(w, r, "advance_learning_session", map[string]any{
        "session_id": id, "next_phase": request.NextPhase,
        "expected_revision": *request.ExpectedRevision,
    }, http.StatusOK)
}
