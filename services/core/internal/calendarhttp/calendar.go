// Package calendarhttp exposes a restricted, read-only calendar service.
package calendarhttp

import (
	"bytes"
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/dbontr/homelab-new/system/calendar"
	"github.com/dbontr/homelab-new/system/internal/httpjson"
)

const maxFeedBytes = 12 << 20

type Client struct {
	endpoint string
	token    string
	http     *http.Client
}

func validToken(token string) bool {
	return len(token) >= 32 && len(token) <= 4096 && !strings.ContainsAny(token, "\x00\r\n\t ")
}

// NewClient accepts only a literal loopback HTTP origin and a dedicated
// service credential. It cannot be redirected to a caller-selected endpoint.
func NewClient(raw, token string) (*Client, error) {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "http" || u.User != nil || u.Opaque != "" ||
		u.RawQuery != "" || u.Fragment != "" || (u.Path != "" && u.Path != "/") ||
		u.Host == "" || u.Port() == "" || !validToken(token) {
		return nil, errors.New("calendar service requires a protected token and loopback HTTP origin")
	}
	ip := net.ParseIP(u.Hostname())
	if ip == nil || !ip.IsLoopback() {
		return nil, errors.New("calendar service must use a literal loopback IP")
	}
	return &Client{
		endpoint: strings.TrimSuffix(u.String(), "/"),
		token:    token,
		http: &http.Client{
			Timeout: 20 * time.Second,
			Transport: &http.Transport{
				Proxy: nil,
				ResponseHeaderTimeout: 15 * time.Second,
			},
			CheckRedirect: func(*http.Request, []*http.Request) error {
				return errors.New("calendar service redirects are disabled")
			},
		},
	}, nil
}

// Fetch returns validated calendar events without transferring the external
// calendar sharing URL or its credentials to the platform process.
func (c *Client) Fetch(ctx context.Context) (calendar.Feed, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.endpoint+"/v1/calendar", nil)
	if err != nil {
		return calendar.Feed{}, err
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Accept", "application/json")
	response, err := c.http.Do(req)
	if err != nil {
		return calendar.Feed{}, errors.New("calendar service unavailable")
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK || response.ContentLength > maxFeedBytes {
		return calendar.Feed{}, fmt.Errorf("calendar service returned HTTP %d or oversized feed", response.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, maxFeedBytes+1))
	if err != nil || len(body) > maxFeedBytes {
		return calendar.Feed{}, errors.New("calendar service response exceeds size limit")
	}
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.DisallowUnknownFields()
	var feed calendar.Feed
	if err := decoder.Decode(&feed); err != nil {
		return calendar.Feed{}, errors.New("invalid calendar service response")
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		return calendar.Feed{}, errors.New("calendar service response exceeds its JSON envelope")
	}
	return feed, nil
}

// NewHandler provides a single authenticated calendar resource; neither
// credentials nor arbitrary provider URLs are accepted in request arguments.
func NewHandler(feedURL, token string, upstream *http.Client) (http.Handler, error) {
	if err := calendar.ValidateURL(feedURL); err != nil {
		return nil, err
	}
	if !validToken(token) || upstream == nil {
		return nil, errors.New("calendar service requires a protected token and HTTP client")
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		if r.Method == http.MethodGet && r.URL.Path == "/healthz" {
			httpjson.Write(w, http.StatusOK, map[string]string{"status": "ok"})
			return
		}
		if r.Method != http.MethodGet || r.URL.Path != "/v1/calendar" ||
			r.URL.RawQuery != "" || r.Header.Get("Origin") != "" {
			http.NotFound(w, r)
			return
		}
		if subtle.ConstantTimeCompare([]byte(r.Header.Get("Authorization")), []byte("Bearer "+token)) != 1 {
			w.Header().Set("WWW-Authenticate", `Bearer realm="calendar"`)
			httpjson.Error(w, http.StatusUnauthorized, "calendar service authorization required")
			return
		}
		feed, err := calendar.Fetch(r.Context(), upstream, feedURL)
		if err != nil {
			httpjson.Error(w, http.StatusBadGateway, "calendar feed unavailable")
			return
		}
		httpjson.Write(w, http.StatusOK, feed)
	}), nil
}
