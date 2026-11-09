package agentflow

import (
    "context"
    "encoding/json"
    "errors"
)

// readKnowledge uses the controller's existing project policy, grants, and
// library links. No unscoped storage or provider credentials are exposed.
func (e *Engine) readKnowledge(ctx context.Context, project string, action AgentAction) (string, error) {
    var name string
    args := map[string]any{"project_id": project}
    switch action.Action {
    case "project_context":
        name = "knowledge_context"
    case "knowledge_search":
        name = "knowledge_search_contributions"
        args["query"] = action.Query
    case "research_search":
        name = "research_search"
        args["query"] = action.Query
        args["limit"] = 8
    case "library_documents":
        name = "library_documents"
        args["library_id"] = action.Query
    case "library_resolve":
        name = "library_resolve"
        args["library_id"] = action.Query
    default:
        return "", errors.New("unsupported knowledge operation")
    }
    result, err := e.Compute.Call(ctx, name, args)
    if err != nil { return "", err }
    raw, err := json.Marshal(result)
    if err != nil { return "", err }
    if len(raw) > 12<<10 {
        return "", errors.New("knowledge response exceeds agent observation limit; narrow the request")
    }
    return string(raw), nil
}
