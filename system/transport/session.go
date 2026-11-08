package transport

import (
	"crypto/subtle"
	"encoding/json"
	"net/http"
	"strings"
	"time"
)

const uiCookie = "__Host-homelab-ui"

func (s *Server) sessionCookie(r *http.Request) string {
	cookie, err := r.Cookie(uiCookie)
	if err != nil { return "" }
	return cookie.Value
}

// Browser reads are restricted to allowlisted routes. Owner writes require
// independent path authorization, a valid session, Origin and CSRF token.
func (s *Server) sessionReadAllowed(r *http.Request) bool {
	if r.Method != http.MethodGet || s.sessions == nil { return false }
	p := r.URL.Path
	permitted := map[string]bool{
		"/v1/nodes":true,
		"/v1/devices":true,
		"/v1/tasks":true,
        "/v1/tasks/agenda":true,
        "/v1/tasks/conflicts":true,
        "/v1/groceries/board":true,
        "/v1/groceries":true,
        "/v1/recipes":true,
        "/v1/recipes/health-goals":true,
        "/v1/recipes/export":true,
        "/v1/meals":true,
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
		"/v1/calendar":true,
        "/v1/calendar/occurrences":true,
		"/v1/compute/profiles":true,
		"/v1/software":true,
		"/v1/books/search":true,
        "/v1/books/requests":true,
        "/v1/media/search":true,
		"/v1/media/requests":true,
		"/v1/tools":true,
		"/v1/workspace/projects":true,
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
    !(strings.HasPrefix(p,"/v1/workspace/projects/") && len(strings.Split(p,"/"))==6 && (strings.HasSuffix(p,"/jobs")||strings.HasSuffix(p,"/workspaces"))) &&
    !(strings.HasPrefix(p,"/v1/learning/sessions/") && (len(strings.Split(p,"/"))==5 || len(strings.Split(p,"/"))==6 && strings.HasSuffix(p,"/pending"))) {
		return false
	}
	_,_,ok := s.sessions.Check(s.sessionCookie(r))
	return ok
}

func (s *Server) uiSession(w http.ResponseWriter,r *http.Request) {
	if s.sessions == nil {
		writeError(w,http.StatusServiceUnavailable,"browser sessions are not configured")
		return
	}
	cookie := s.sessionCookie(r)
	csrf,expiry,valid := s.sessions.Check(cookie)
	switch r.Method {
	case http.MethodGet:
		if !valid { writeJSON(w,200,map[string]any{"authenticated":false});return }
		writeJSON(w,200,map[string]any{"authenticated":true,"expires_at":expiry,"csrf_token":csrf})
	case http.MethodPost:
		if r.Header.Get("Origin")=="" {writeError(w,http.StatusForbidden,"origin required");return}
		if valid {writeError(w,http.StatusConflict,"browser session already exists");return}
		var input struct { Passphrase string `json:"passphrase"` }
		if err:=decode(r,&input);err!=nil{writeError(w,400,"invalid login request");return}
		pass:=[]byte(input.Passphrase)
		input.Passphrase=""
		defer func(){for i:=range pass{pass[i]=0}}()
		token,_,until,err:=s.sessions.Login(pass)
		if err!=nil {writeError(w,http.StatusUnauthorized,"authentication failed");return}
		http.SetCookie(w,&http.Cookie{
			Name:uiCookie,Value:token,Path:"/",HttpOnly:true,Secure:true,
			SameSite:http.SameSiteStrictMode,Expires:until,
		})
		writeJSON(w,200,map[string]any{"authenticated":true,"expires_at":until})
	case http.MethodDelete:
		if r.Header.Get("Origin")=="" {writeError(w,http.StatusForbidden,"origin required");return}
		if !valid || len(r.Header.Get("X-CSRF-Token"))!=len(csrf) ||
			subtle.ConstantTimeCompare([]byte(r.Header.Get("X-CSRF-Token")),[]byte(csrf))!=1 {
			writeError(w,http.StatusForbidden,"session authorization required");return
		}
		if !s.sessions.Logout(cookie,csrf) {writeError(w,http.StatusForbidden,"session expired");return}
		http.SetCookie(w,&http.Cookie{
			Name:uiCookie,Value:"",Path:"/",HttpOnly:true,Secure:true,
			SameSite:http.SameSiteStrictMode,MaxAge:-1,Expires:time.Unix(0,0),
		})
		w.WriteHeader(http.StatusNoContent)
	default:
		writeError(w,http.StatusMethodNotAllowed,"method not allowed")
	}
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
