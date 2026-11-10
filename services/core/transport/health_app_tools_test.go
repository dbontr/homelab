package transport

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestOwnerHealthToolPathsAreExactAndNarrow(t *testing.T) {
	for name := range ownerHealthAppTools {
		path := "/v1/health/app-tools/" + name + "/call"
		if !ownerHealthAppToolPathAllowed(path) {
			t.Fatalf("approved Health operation not available: %s", path)
		}
	}
	for _, path := range []string{
		"/v1/tools/health__health_goal_set/call",
		"/v1/health/app-tools/health_record_add/call",
		"/v1/health/app-tools/health_goal_set/call/extra",
		"/v1/health/app-tools/health_goal_set/call/",
		"/v1/health/app-tools/health_goal_set//call",
		"/v1/health/app-tools/security_session_open/call",
		"/v1/health/app-tools/health_goal_set%2Fsecurity_session_open/call",
		"/v1/health/app-tools/health_goal_set",
		"/v1/health/app-tools/../health_goal_set/call",
	} {
		if ownerHealthAppToolPathAllowed(path) {
			t.Errorf("unexpected browser tool authorization: %q", path)
		}
	}
}

func TestOwnerHealthToolHandlerRejectsUnapprovedNames(t *testing.T) {
	server := &Server{}
	for _, name := range []string{"health_record_add", "security_session_open", "health_goal_set/other"} {
		req := httptest.NewRequest(http.MethodPost, "/v1/health/app-tools/"+name+"/call",
			strings.NewReader(`{"arguments":{}}`))
		req.SetPathValue("name", name)
		rec := httptest.NewRecorder()
		server.uiHealthAppToolCall(rec, req)
		if rec.Code != http.StatusNotFound {
			t.Fatalf("%q returned %d, want 404", name, rec.Code)
		}
	}
}
