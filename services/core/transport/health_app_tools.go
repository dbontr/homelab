package transport

import (
	"encoding/json"
	"net/http"
	"strings"
)

// The owner-session Health interface can invoke only these domain operations.
// General MCP tool invocation remains unavailable to browser sessions.
var ownerHealthAppTools = map[string]struct{}{
	"health_metric_series":     {},
	"health_nutrient_daily":    {},
	"health_goal_set":          {},
	"health_plan_list":         {},
	"health_plan_save":         {},
	"health_grocery_list":      {},
	"health_grocery_save":      {},
	"health_grocery_from_plan": {},
	"health_training_set_list": {},
	"health_training_set_log":  {},
	"health_workout_log":       {},
}

func ownerHealthAppToolAllowed(name string) bool {
	_, ok := ownerHealthAppTools[name]
	return ok
}

func ownerHealthAppToolPathAllowed(path string) bool {
	const prefix = "/v1/health/app-tools/"
	const suffix = "/call"
	if !strings.HasPrefix(path, prefix) || !strings.HasSuffix(path, suffix) {
		return false
	}
	name := strings.TrimSuffix(strings.TrimPrefix(path, prefix), suffix)
	return !strings.Contains(name, "/") && ownerHealthAppToolAllowed(name)
}

func (s *Server) uiHealthAppToolCall(w http.ResponseWriter, r *http.Request) {
	s.ownerDomainToolCall(w,r,"health__",ownerHealthAppTools)
}

// Domain tool routes have distinct, exact browser allowlists. A session
// cannot acquire general MCP authority through this internal transport.
func (s *Server) ownerDomainToolCall(
	w http.ResponseWriter,r *http.Request,namespace string,allowlist map[string]struct{},
) {
	name:=r.PathValue("name")
	if _,allowed:=allowlist[name];!allowed{
		http.NotFound(w,r)
		return
	}
	r.Body=http.MaxBytesReader(w,r.Body,1<<20)
	var input struct{
		Arguments json.RawMessage `json:"arguments"`
	}
	if err:=decode(r,&input);err!=nil||len(input.Arguments)==0{
		writeError(w,http.StatusBadRequest,"invalid domain tool arguments")
		return
	}
	output:=s.invoke(r.Context(),namespace+name,input.Arguments,json.RawMessage("1"))
	if failed,_:=output["isError"].(bool);failed{
		writeError(w,http.StatusBadGateway,"domain operation unavailable or rejected")
		return
	}
	writeJSON(w,http.StatusOK,output)
}
