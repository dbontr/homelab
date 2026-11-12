package transport

import (
		"encoding/json"
	"net/http"
	"strings"
	)

// Browser reads are restricted to allowlisted routes. Owner writes require
// independent path authorization, a valid session, Origin and CSRF token.
func (s *Server) sessionReadAllowed(r *http.Request) bool {
	if r.Method != http.MethodGet || s.sessions == nil { return false }
	p := r.URL.Path
	permitted := map[string]bool{
		"/v1/nodes":true,
		"/v1/devices":true,
		"/v1/tasks":true,
        "/v1/motivation/state":true,
        "/v1/motivation/metrics":true,
        "/v1/tasks/delegations":true,
        "/v1/tasks/agenda":true,
        "/v1/tasks/conflicts":true,
        "/v1/groceries/board":true,
        "/v1/groceries":true,
        "/v1/groceries/needs":true,
        "/v1/recipes":true,
        "/v1/pantry":true,
        "/v1/recipes/health-goals":true,
        "/v1/recipes/export":true,
        "/v1/recipes/bundle":true,
        "/v1/meals":true,
        "/v1/engineering/projects":true,
        "/v1/engineering/nodes":true,
        "/v1/learning/summary":true,
        "/v1/learning/sessions":true,
        "/v1/learning/reviews":true,
        "/v1/health/plans":true,
        "/v1/health/records":true,
        "/v1/health/training-sets":true,
        "/v1/health/training-progress":true,
        "/v1/health/goals":true,
        "/v1/health/food-proposals":true,
		"/v1/goals":true,
		"/v1/rules":true,
		"/v1/recommendations":true,
		"/v1/reports/weekly":true,
        "/v1/planning/week":true,
        "/v1/planning/review":true,
		"/v1/calendar":true,
        "/v1/calendar/occurrences":true,
		"/v1/compute/profiles":true,
		"/v1/software":true,
		"/v1/books/search":true,
        "/v1/books/lending":true,
        "/v1/books/requests":true,
        "/v1/media/search":true,
		"/v1/media/requests":true,
		"/v1/tools":true,
		"/v1/workspace/projects":true,
        "/v1/workflows":true,
        "/v1/automations":true,
        "/v1/automations/alerts":true,
		"/v1/workspace/nodes":true,
        "/v1/activity":true,
        "/v1/monitoring/summary":true,
        "/v1/monitoring/targets":true,
        "/v1/monitoring/alerts":true,
        "/v1/monitoring/series":true,
        "/v1/monitoring/logs":true,
	}
	if !permitted[p] && !strings.HasPrefix(p,"/v1/compute/profiles/") &&
    !(strings.HasPrefix(p,"/v1/recipes/") && ((len(strings.Split(p,"/"))==5 && strings.HasPrefix(p,"/v1/recipes/images/")) ||
      (len(strings.Split(p,"/"))==4 && p!="/v1/recipes/import" && p!="/v1/recipes/parse")||
      (len(strings.Split(p,"/"))==5 && (strings.HasSuffix(p,"/nutrition") || strings.HasSuffix(p,"/history"))))) &&
    !(strings.HasPrefix(p,"/v1/resources/") && len(strings.Split(p,"/"))==5) &&
    !(strings.HasPrefix(p,"/v1/projects/") && len(strings.Split(p,"/"))==5 && strings.HasSuffix(p,"/overview")) &&
    !(strings.HasPrefix(p,"/v1/workflows/") && len(strings.Split(p,"/"))==4) &&
    !(strings.HasPrefix(p,"/v1/automations/") && len(strings.Split(p,"/"))==4) &&
    !(strings.HasPrefix(p,"/v1/workspace/projects/") && len(strings.Split(p,"/"))==6 && (strings.HasSuffix(p,"/jobs")||strings.HasSuffix(p,"/workspaces"))) &&
    !(strings.HasPrefix(p,"/v1/engineering/projects/") && len(strings.Split(p,"/"))==6 &&
        (strings.HasSuffix(p,"/jobs") || strings.HasSuffix(p,"/workspaces") || strings.HasSuffix(p,"/artifacts") ||
         strings.HasSuffix(p,"/context") || strings.HasSuffix(p,"/repositories") ||
         strings.HasSuffix(p,"/research") || strings.HasSuffix(p,"/research-sources"))) &&
    !ownerLearningAttachmentReadPath(p) &&
    !(strings.HasPrefix(p,"/v1/learning/sessions/") && (len(strings.Split(p,"/"))==5 || len(strings.Split(p,"/"))==6 && (strings.HasSuffix(p,"/pending") || strings.HasSuffix(p,"/next") || strings.HasSuffix(p,"/probe")))) {
		return false
	}
	_, ok := s.sessions.Check(r.Header.Get("Cf-Access-Jwt-Assertion"))
	return ok
}

func (s *Server) uiSession(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	if s.sessions == nil {
		writeError(w, http.StatusServiceUnavailable, "console identity is not configured")
		return
	}
	csrf, ok := s.sessions.Check(r.Header.Get("Cf-Access-Jwt-Assertion"))
	if !ok {
		writeJSON(w, http.StatusOK, map[string]any{"authenticated": false})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"authenticated": true,
		"csrf_token": csrf,
	})
}

func (s *Server) uiBackendRead(w http.ResponseWriter,r *http.Request,backend,tool string) {
	if !s.hasBackend(backend) {writeError(w,http.StatusServiceUnavailable,"service not configured");return}
	value:=s.invoke(r.Context(),backend+"__"+tool,json.RawMessage("{}"),json.RawMessage("1"))
	if failed,_:=value["isError"].(bool);failed {
		writeError(w,http.StatusBadGateway,"integration unavailable");return
	}
	parts,ok:=value["content"].([]any)
	if !ok||len(parts)==0 {writeError(w,http.StatusBadGateway,"integration returned invalid data");return}
	part,ok:=parts[0].(map[string]any)
	if !ok {writeError(w,http.StatusBadGateway,"workspace returned invalid data");return}
	raw,ok:=part["text"].(string)
	if !ok||len(raw)>2<<20 {writeError(w,http.StatusBadGateway,"workspace returned invalid data");return}
	var payload any
	if err:=json.Unmarshal([]byte(raw),&payload);err!=nil {
		writeError(w,http.StatusBadGateway,"integration returned invalid JSON");return
	}
	writeJSON(w,200,payload)
}
func (s *Server) uiProjects(w http.ResponseWriter,r *http.Request) {
	s.uiBackendRead(w,r,"workspace","list_projects")
}
func (s *Server) uiComputeNodes(w http.ResponseWriter,r *http.Request) {
	s.uiBackendRead(w,r,"workspace","list_compute_nodes")
}

func (s *Server) uiHealthGoals(w http.ResponseWriter,r *http.Request) {
	s.uiBackendRead(w,r,"health","health_goal_list")
}
