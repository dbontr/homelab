package transport

import (
    "errors"
    "net"
    "net/http"
    "net/url"
    "strconv"
    "strings"
)

func loopbackHost(host string) bool {
    if strings.EqualFold(host, "localhost") { return true }
    ip := net.ParseIP(host)
    return ip != nil && ip.IsLoopback()
}

// canonicalOrigin accepts only exact HTTP(S) origins, not arbitrary URL patterns.
func canonicalOrigin(raw string) (string, error) {
    if raw == "" || strings.TrimSpace(raw) != raw || strings.Contains(raw, ",") {
        return "", errors.New("invalid origin")
    }
    u, err := url.Parse(raw)
    if err != nil || u.Opaque != "" || u.User != nil || u.Hostname() == "" ||
        u.Path != "" || u.RawQuery != "" || u.ForceQuery || u.Fragment != "" ||
        (u.Scheme != "https" && u.Scheme != "http") {
        return "", errors.New("origin must be an HTTP(S) scheme and host")
    }
    if u.Scheme == "http" && !loopbackHost(u.Hostname()) {
        return "", errors.New("non-loopback origins must use HTTPS")
    }
    if port := u.Port(); port != "" {
        value, err := strconv.Atoi(port)
        if err != nil || value < 1 || value > 65535 {
            return "", errors.New("invalid origin port")
        }
    }
    return strings.ToLower(u.Scheme) + "://" + strings.ToLower(u.Host), nil
}

// SetAllowedOrigins configures exact external origins, never wildcard matching.
func (s *Server) SetAllowedOrigins(origins []string) error {
    allowed := make(map[string]struct{}, len(origins))
    for _, raw := range origins {
        origin, err := canonicalOrigin(raw)
        if err != nil { return err }
        if _, exists := allowed[origin]; exists { return errors.New("duplicate origin") }
        allowed[origin] = struct{}{}
    }
    s.allowedOrigins = allowed
    return nil
}

func (s *Server) originAllowed(r *http.Request) bool {
    values, exists := r.Header["Origin"]
    if !exists { return true } // Non-browser MCP and CLI clients do not send Origin.
    if len(values) != 1 { return false }
    origin, err := canonicalOrigin(values[0])
    if err != nil { return false }
    if _, ok := s.allowedOrigins[origin]; ok { return true }
    u, err := url.Parse(origin)
    if err != nil { return false }
    // An unconfigured same-origin request is accepted only for loopback hosts.
    if !loopbackHost(u.Hostname()) || !strings.EqualFold(u.Host, r.Host) {
        return false
    }
    if r.TLS != nil { return u.Scheme == "https" }
    return u.Scheme == "http"
}
