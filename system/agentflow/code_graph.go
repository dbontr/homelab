package agentflow

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
)

type DependencyNode struct {
	ID       string `json:"id"`
	Kind     string `json:"kind"`
	Language string `json:"language,omitempty"`
}
type DependencyEdge struct {
	From string `json:"from"`
	To   string `json:"to"`
	Kind string `json:"kind"`
}
type DependencyGraph struct {
	Scope     string           `json:"scope"`
	Nodes     []DependencyNode `json:"nodes"`
	Edges     []DependencyEdge `json:"edges"`
	Truncated bool             `json:"truncated,omitempty"`
}

func graphFromMap(source CodeMap) DependencyGraph {
	graph := DependencyGraph{Scope: source.Scope, Truncated: source.Truncated}
	nodes := map[string]DependencyNode{}
	edges := map[string]DependencyEdge{}
	for _, file := range source.Files {
		if !validSourcePath(file.Path) || sensitiveSourcePath(file.Path) {
			continue
		}
		nodes[file.Path] = DependencyNode{ID: file.Path, Kind: "source", Language: file.Language}
		for _, imported := range file.Imports {
			if imported == "" || len(imported) > 180 {
				continue
			}
			target := "import:" + imported
			nodes[target] = DependencyNode{ID: target, Kind: "import"}
			edge := DependencyEdge{From: file.Path, To: target, Kind: "declares_import"}
			edges[file.Path+"\x00"+target] = edge
			if len(nodes) > 250 || len(edges) > 320 {
				graph.Truncated = true
				break
			}
		}
	}
	for _, node := range nodes {
		graph.Nodes = append(graph.Nodes, node)
	}
	for _, edge := range edges {
		graph.Edges = append(graph.Edges, edge)
	}
	sort.Slice(graph.Nodes, func(i, j int) bool { return graph.Nodes[i].ID < graph.Nodes[j].ID })
	sort.Slice(graph.Edges, func(i, j int) bool {
		if graph.Edges[i].From == graph.Edges[j].From {
			return graph.Edges[i].To < graph.Edges[j].To
		}
		return graph.Edges[i].From < graph.Edges[j].From
	})
	if len(graph.Nodes) > 120 {
		graph.Nodes = graph.Nodes[:120]
		graph.Truncated = true
	}
	if len(graph.Edges) > 160 {
		graph.Edges = graph.Edges[:160]
		graph.Truncated = true
	}
	return graph
}

// repositoryGraph describes declared file imports. It does not imply a
// compiler-resolved dependency relationship.
func (e *Engine) repositoryGraph(ctx context.Context, workflow Workflow, brief AgentBrief, scope string) (string, error) {
	raw, err := e.repositoryMap(ctx, workflow, brief, scope)
	if err != nil {
		return "", err
	}
	var source CodeMap
	if err := json.Unmarshal([]byte(raw), &source); err != nil {
		return "", errors.New("invalid bounded source map")
	}
	graph := graphFromMap(source)
	result, err := json.Marshal(graph)
	if err != nil {
		return "", err
	}
	if len(result) > 16<<10 {
		return "", errors.New("dependency graph exceeds observation limit")
	}
	return string(result), nil
}

// goLanguageServer dispatches read-only navigation to the approved compute
// workspace. It does not install tools, spawn an HTTP service, or edit files.
func (e *Engine) goLanguageServer(ctx context.Context, workflow Workflow, brief AgentBrief, action AgentAction) (string, error) {
	if sourceLanguage(action.Path) != "go" || !validSourcePath(action.Path) ||
		sensitiveSourcePath(action.Path) || action.Line < 1 || action.Line > 100000 ||
		action.Column < 1 || action.Column > 500 ||
		(action.Operation != "definition" && action.Operation != "references" && action.Operation != "hover") {
		return "", errors.New("invalid language-server request")
	}
	root := repoRoot(brief.Repository)
	location := fmt.Sprintf("%s:%d:%d", action.Path, action.Line, action.Column)
	command := "cd " + shellQuote(root) +
		" && if command -v gopls >/dev/null 2>&1; then GOPROXY=off GOSUMDB=off gopls " +
		action.Operation + " " + shellQuote(location) +
		"; else printf 'language server unavailable\\n'; exit 127; fi"
	reply, err := e.workspaceAction(ctx, workflow, "command", map[string]any{"command": command})
	if err != nil {
		return "", err
	}
	exit, ok := reply["exit_code"].(float64)
	if !ok {
		return "", errors.New("language-server completion is uncertain")
	}
	if exit == 127 {
		return "gopls is not installed in the isolated workspace; use source navigation", nil
	}
	if exit != 0 {
		return "", fmt.Errorf("language server returned exit code %.0f", exit)
	}
	output := str(reply["stdout"])
	if len(output) > 8000 {
		output = output[:8000]
	}
	if strings.TrimSpace(output) == "" {
		return "No language-server results", nil
	}
	return output, nil
}

