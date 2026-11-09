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
	case r.Method == http.MethodPost && (path == "/v1/recipes" || path == "/v1/recipes/import" || path == "/v1/recipes/parse" || path == "/v1/recipes/import-url" || path == "/v1/recipes/bundle" || path == "/v1/recipes/recipesage/preview" || path == "/v1/recipes/recipesage/import" || path == "/v1/recipes/images" || path == "/v1/meals" || path == "/v1/books/requests" || path == "/v1/tasks" || path == "/v1/tasks/capture" || path == "/v1/goals"):
        allowed = true
    case r.Method == http.MethodPost && strings.HasPrefix(path, "/v1/automations/"):
        parts := strings.Split(path, "/")
        allowed = len(parts)==7 && parts[4]=="alerts" && parts[6]=="ack"
    case r.Method == http.MethodPost && (path=="/v1/research/learning/preview" || path=="/v1/research/learning/import"):
        allowed=true
    case r.Method == http.MethodPut && path == "/v1/pantry":
        allowed=true
    case r.Method == http.MethodDelete && strings.HasPrefix(path,"/v1/pantry/items/"):
        allowed=len(strings.Split(path,"/"))==5
    case r.Method == http.MethodPost && path == "/v1/recipes/recommend":
        allowed=true
    case r.Method == http.MethodPost && path == "/v1/learning/sessions":
        allowed = true
    case r.Method == http.MethodPost && strings.HasPrefix(path, "/v1/learning/questions/"):
        allowed = len(strings.Split(path, "/"))==6 && strings.HasSuffix(path, "/answer")
    case r.Method == http.MethodPut && strings.HasPrefix(path, "/v1/books/requests/"):
        allowed = len(strings.Split(path, "/")) == 5
    case r.Method == http.MethodPost && path == "/v1/health/training-sets":
        allowed = true
    case r.Method == http.MethodPost && path == "/v1/health/records":
        allowed = true
    case r.Method == http.MethodPost && strings.HasPrefix(path, "/v1/health/food-proposals/"):
        allowed = len(strings.Split(path, "/")) == 6 && strings.HasSuffix(path, "/confirm")
    case r.Method == http.MethodPost && strings.HasPrefix(path, "/v1/recipes/"):
        allowed = len(strings.Split(path, "/")) == 5 && (strings.HasSuffix(path, "/nutrition-proposal") || strings.HasSuffix(path, "/restore"))
    case r.Method == http.MethodPost && strings.HasPrefix(path, "/v1/tasks/"):
        allowed = len(strings.Split(path, "/")) == 5 && strings.HasSuffix(path, "/complete")
    case r.Method == http.MethodPut && strings.HasPrefix(path, "/v1/groceries/items/"):
        allowed = len(strings.Split(path, "/")) == 5
    case r.Method == http.MethodPut && strings.HasPrefix(path, "/v1/tasks/"):
        allowed = len(strings.Split(path, "/")) == 4
	case (r.Method == http.MethodPut || r.Method == http.MethodDelete) && strings.HasPrefix(path, "/v1/recipes/"):
		allowed = len(strings.Split(path, "/")) == 4 && path != "/v1/recipes/export"
	case r.Method == http.MethodDelete && strings.HasPrefix(path, "/v1/meals/"):
		allowed = len(strings.Split(path, "/")) == 4
	}
	if !allowed { return false }
	if r.Method != http.MethodDelete {
		mediaType, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
		if err != nil { return false }
		if path == "/v1/recipes/images" {
			if mediaType != "image/png" && mediaType != "image/jpeg" { return false }
		} else if path == "/v1/recipes/bundle" {
			if mediaType != "application/zip" { return false }
		} else if mediaType != "application/json" { return false }
	}
	csrf, _, valid := s.sessions.Check(s.sessionCookie(r))
	supplied := r.Header.Get("X-CSRF-Token")
	return valid && len(csrf) == len(supplied) &&
		subtle.ConstantTimeCompare([]byte(csrf), []byte(supplied)) == 1
}
