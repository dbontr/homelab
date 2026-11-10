package transport

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/dbontr/homelab-new/system/profiles"
)

// Profile planning requires Workspace coordination. The registry owns
// profile resource and revision behavior for every interface.
func (s *Server) profileTool(ctx context.Context, name string, raw json.RawMessage) (any, error) {
	if s.profiles == nil {
		return nil, errors.New("compute profiles are not configured")
	}
	if name == "compute_profile_plan" {
		return s.planProfile(ctx, raw)
	}
	return profiles.Execute(s.profiles, name, raw)
}
