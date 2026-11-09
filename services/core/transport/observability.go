package transport

import "github.com/dbontr/homelab-new/system/observability"

func(s *Server) SetObservability(client *observability.Client){s.observability=client}
