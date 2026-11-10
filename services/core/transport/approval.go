package transport

import (
 "context"
 "encoding/json"
 "strings"
 "time"

 "github.com/dbontr/homelab-new/system/approval"
)

// Privilege targets are derived from canonical tool arguments, never from a
// model-provided independent security decision.
func privilegeTarget(name string, args json.RawMessage) string {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(args, &fields); err != nil {
		return "platform"
	}
	priority := []string{"target", "node_id", "device_id", "project_id", "repository", "host", "id"}
	if strings.HasPrefix(name, "workspace__github_") || strings.HasPrefix(name, "workspace__git_") {
		priority = []string{"repository", "project_id", "target"}
	} else if strings.HasPrefix(name, "workspace__") {
		priority = []string{"project_id", "node_id", "device_id", "repository", "target"}
	} else if strings.HasPrefix(name, "cluster__") || strings.HasPrefix(name, "infrastructure__") {
		priority = []string{"host", "node_id", "target", "device_id"}
	}
	for _, key := range priority {
		raw, ok := fields[key]
		if !ok {
			continue
		}
		var value string
		if err := json.Unmarshal(raw, &value); err == nil && value != "" {
			return value
		}
	}
	return "platform"
}

func (s *Server) checkPrivilege(name string,args json.RawMessage)(map[string]any,bool){
 return s.checkPrivilegeContext(context.Background(),name,args)
}

// Sensitive operations require an independent owner identity and operation
// policy. Missing or unavailable security services cannot grant authority.
func (s *Server) checkPrivilegeContext(ctx context.Context,name string,args json.RawMessage)(map[string]any,bool){
 scope:=approval.ScopeFor(name)
 if scope==""{return nil,true}
 if s.privilegeClient==nil{
  return toolError("independent security authorization unavailable"),false
 }
 ctx,cancel:=context.WithTimeout(ctx,3*time.Second)
 defer cancel()
 decision,err:=s.privilegeClient.Authorize(ctx,name,privilegeTarget(name,args),args)
 if err!=nil{return toolError("independent security authorization unavailable"),false}
 if decision.Allowed{return nil,true}
 return result(map[string]any{
  "authorization_required":true,
  "capability":scope,
  "reason":decision.Reason,
  "confirmation_id":decision.ConfirmationID,
  "confirmation_expires_at":decision.ConfirmationExpiresAt,
  "instructions":"Authenticate with the trusted local homelab-admin program. For destructive operations, review and confirm the listed operation locally; then retry the same arguments. Never enter passwords or OTPs in chat.",
 }),false
}
