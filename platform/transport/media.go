package transport

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"

	"github.com/dbontr/homelab-new/platform/media"
)

func (s *Server) ConfigureMedia(client *media.Client) {
	s.media = client
}

func (s *Server) mediaSearch(w http.ResponseWriter, r *http.Request) {
	if s.media == nil {
		writeError(w, http.StatusServiceUnavailable, "media is not configured")
		return
	}
	data, err := s.media.Search(r.Context(), r.URL.Query().Get("query"))
	if err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, data)
}

func (s *Server) mediaRequests(w http.ResponseWriter, r *http.Request) {
	if s.media == nil {
		writeError(w, http.StatusServiceUnavailable, "media is not configured")
		return
	}
	if r.Method == http.MethodGet {
		data, err := s.media.Requests(r.Context())
		if err != nil {
			writeError(w, http.StatusBadGateway, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, data)
		return
	}
	var input media.MediaRequest
	if err := decode(r, &input); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	data, err := s.media.AddRequest(r.Context(), input)
	if err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, data)
}

func (s *Server) mediaTool(ctx context.Context, name string, raw json.RawMessage) (any, error) {
	if s.media == nil {
		return nil, errors.New("media is not configured")
	}
	var input struct {
		Query string `json:"query"`
		media.MediaRequest
	}
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &input); err != nil {
			return nil, err
		}
	}
	switch name {
	case "media_search":
		return s.media.Search(ctx, input.Query)
	case "media_requests":
		return s.media.Requests(ctx)
	case "media_request":
		return s.media.AddRequest(ctx, input.MediaRequest)
	}
	return nil, errors.New("unknown media operation")
}
