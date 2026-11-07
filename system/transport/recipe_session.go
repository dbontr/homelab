package transport

import (
	"crypto/subtle"
	"mime"
	"net/http"
	"strings"
)

// Recipe writes are owner-session operations, not cluster or MCP privileges.
func (s *Server) sessionRecipeWriteAllowed(r *http.Request) bool {
	if s.sessions == nil || r.Header.Get("Origin") == "" { return false }
	path := r.URL.Path
	allowed := false
	switch {
	case r.Method == http.MethodPost && (path == "/v1/recipes" || path == "/v1/recipes/import" || path == "/v1/meals"):
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
