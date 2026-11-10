package transport

import (
	"net/http"
	"strings"
)

// The owner may inspect learning records through these fixed operations.
// Mutating learning tools, sealed answers, attachments, and arbitrary MCP
// functions are not available through the learning library.
var ownerLearningReadTools = map[string]struct{}{
	"get_graph":              {},
	"get_concept":            {},
	"list_problems":          {},
	"get_problem":            {},
	"get_review_queue":      {},
	"get_reasoning_graph":    {},
	"list_learning_sources":  {},
	"list_practice_sessions": {},
	"get_practice_session":   {},
}

func ownerLearningReadPathAllowed(path string) bool {
	const prefix="/v1/learning/app-tools/"
	const suffix="/call"
	if !strings.HasPrefix(path,prefix)||!strings.HasSuffix(path,suffix){return false}
	name:=strings.TrimSuffix(strings.TrimPrefix(path,prefix),suffix)
	if strings.Contains(name,"/"){return false}
	_,ok:=ownerLearningReadTools[name]
	return ok
}

func (s *Server) uiLearningReadTool(w http.ResponseWriter,r *http.Request){
	s.ownerDomainToolCall(w,r,"learning__",ownerLearningReadTools)
}
