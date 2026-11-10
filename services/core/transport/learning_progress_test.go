package transport

import (
    "net/http"
    "net/http/httptest"
    "strings"
    "testing"
)

func TestLearningSessionWriteRoutesAreExact(t *testing.T) {
    for _, path := range []string{
        "/v1/learning/sessions/session-1/plan",
        "/v1/learning/sessions/session_1/phase",
    } {
        if !ownerLearningSessionWritePath(path) {
            t.Fatalf("expected learning session route: %s", path)
        }
    }
    for _, path := range []string{
        "/v1/learning/sessions/abc/grade",
        "/v1/learning/sessions/abc/plan/extra",
        "/v1/learning/sessions/abc/get_problem_solution",
        "/v1/learning/sessions/abc/../../../health",
        "/v1/learning/sessions/abc/phase/",
        "/v1/learning/sessions/abc%2Fdef/phase",
        "/v1/learning/sessions/abc/answer",
        "/v1/learning/sessions//plan",
    } {
        if ownerLearningSessionWritePath(path) {
            t.Errorf("unsafe session route accepted: %q", path)
        }
    }
}

func TestLearningSessionMutationRequiresRevisionAndValidPhase(t *testing.T) {
    server := &Server{}
    for _, tc := range []struct {
        kind string
        body string
    }{
        {"plan", "{}"},
        {"plan", "{\"expected_revision\":-1}"},
        {"plan", "{\"expected_revision\":\"0\"}"},
        {"plan", "{\"expected_revision\":null}"},
        {"phase", "{}"},
        {"phase", "{\"expected_revision\":2,\"next_phase\":\"grade\"}"},
        {"phase", "{\"expected_revision\":2,\"next_phase\":\"../complete\"}"},
    } {
        req := httptest.NewRequest(http.MethodPost, "/v1/learning/sessions/abc/"+tc.kind,
            strings.NewReader(tc.body))
        req.SetPathValue("id", "abc")
        response := httptest.NewRecorder()
        if tc.kind == "plan" {
            server.uiLearningPlan(response, req)
        } else {
            server.uiLearningPhase(response, req)
        }
        if response.Code != http.StatusBadRequest {
            t.Errorf("%s: invalid request returned %d", tc.kind, response.Code)
        }
    }
}
