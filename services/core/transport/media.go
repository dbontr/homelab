package transport

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/dbontr/homelab-new/system/media"
)

func (s *Server) ConfigureMedia(client *media.Client) {
	s.media = client
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
