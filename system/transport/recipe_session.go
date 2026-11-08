package transport

import (
	"crypto/subtle"
	"mime"
	"net/http"
	"strings"
)

// Owner-session mutations are limited to personal records; cluster privileges
// and direct MCP tools always require independent authorization.
func (s *Server) sessionOwnerWriteAllowed(r *http.Request) bool {
	if s.sessions == nil || r.Header.Get("Origin") == "" { return false }
	path := r.URL.Path
	allowed := false
	switch {
	case r.Method == http.MethodPost && (path == "/v1/recipes" || path == "/v1/recipes/import" || path == "/v1/meals" || path == "/v1/tasks" || path == "/v1/tasks/capture" || path == "/v1/goals"):
        allowed = true
    case r.Method == http.MethodPost && strings.HasPrefix(path, "/v1/tasks/"):
        allowed = len(strings.Split(path, "/")) == 5 && strings.HasSuffix(path, "/complete")
    case r.Method == http.MethodPut && strings.HasPrefix(path, "/v1/tasks/"):
        allowed = len(strings.Split(path, "/")) == 4
		allowed = true
	case (r.Method == http.MethodPut || r.Method == http.MethodDelete) && strings.HasPrefix(path, "/v1/recipes/"):
		allowed = len(strings.Split(path, "/")) == 4 && path != "/v1/recipes/export"
	case r.Method == http.MethodDelete && strings.HasPrefix(path, "/v1/meals/"):
		allowed = len(strings.Split(path, "/")) == 4
	}
	if !allowed { return false }
	if r.Method != http.MethodDelete {
		mediaType, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
		if err != nil || mediaType != "application/json" { return false }
	}
	csrf, _, valid := s.sessions.Check(s.sessionCookie(r))
	supplied := r.Header.Get("X-CSRF-Token")
	return valid && len(csrf) == len(supplied) &&
		subtle.ConstantTimeCompare([]byte(csrf), []byte(supplied)) == 1
}
