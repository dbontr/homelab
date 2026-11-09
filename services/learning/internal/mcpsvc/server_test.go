package mcpsvc

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dbontr/homelab-new/system/learning/internal/store"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func testService(t *testing.T) *Service {
	t.Helper()
	db, err := store.Open(filepath.Join(t.TempDir(), "learning.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return NewService(db)
}

func TestHTTPTransportAndAppResources(t *testing.T) {
	svc := testService(t)
	httpServer := httptest.NewServer(svc.Handler("127.0.0.1:*,localhost:*", ""))
	defer httpServer.Close()
	client := mcp.NewClient(&mcp.Implementation{Name: "go-compatibility-test", Version: "1.0.0"}, nil)
	session, err := client.Connect(context.Background(), &mcp.StreamableClientTransport{Endpoint: httpServer.URL + "/mcp"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()
	names := map[string]bool{}
	for tool, err := range session.Tools(context.Background(), nil) {
		if err != nil {
			t.Fatal(err)
		}
		names[tool.Name] = true
		if strings.HasPrefix(tool.Name, "show_") {
			ui, has := tool.Meta["ui"].(map[string]any)
			if !has || ui["resourceUri"] != UIURI {
				t.Fatalf("app tool %s lost UI metadata: %#v", tool.Name, tool.Meta)
			}
		}
	}
	for _, name := range []string{"add_concept", "create_problem", "show_problem_workbench", "show_learning_session", "build_learning_plan", "finish_problem", "get_graph"} {
		if !names[name] {
			t.Fatalf("missing tool: %s", name)
		}
	}
	call := func(name string, arguments map[string]any) map[string]any {
		t.Helper()
		result, err := session.CallTool(context.Background(), &mcp.CallToolParams{Name: name, Arguments: arguments})
		if err != nil {
			t.Fatal(err)
		}
		if result.IsError {
			t.Fatalf("tool %s failed: %+v", name, result)
		}
		var payload map[string]any
		if len(result.Content) == 0 {
			t.Fatalf("tool %s returned no content", name)
		}
		content := result.Content[0].(*mcp.TextContent)
		if err := json.Unmarshal([]byte(content.Text), &payload); err != nil {
			t.Fatal(err)
		}
		return payload
	}
	concept := call("add_concept", map[string]any{"identifier": "uniform-convergence", "title": "Uniform convergence", "summary": "Convergence with a common N"})
	if concept["id"] != "uniform-convergence" {
		t.Fatal(concept)
	}
	graph := call("show_knowledge_graph", map[string]any{})
	if graph["view"] != "graph" {
		t.Fatal(graph)
	}
	problem := call("create_problem", map[string]any{"title": "Compactness proof", "statement": "Prove uniform convergence", "origin": "generated", "reference_solution": "Use nested closed sets", "grading_rubric": "Check compactness"})
	id := problem["id"].(string)
	view := call("show_problem_workbench", map[string]any{"problem_id": id})
	if view["view"] != "problem" || strings.Contains(string(mustJSON(view)), "nested closed sets") {
		t.Fatal("solution leak", view)
	}
	call("save_problem_draft", map[string]any{"problem_id": id, "draft": "Assume not uniformly convergent"})
	call("finish_problem", map[string]any{"problem_id": id, "outcome": "solved", "evaluation": "Valid", "confidence": .95})
	solution := call("get_problem_solution", map[string]any{"problem_id": id})
	if solution["reference_solution"] != "Use nested closed sets" {
		t.Fatal(solution)
	}

	sessionResult := call("start_learning_session", map[string]any{"goal": "Understand uniform convergence", "target_concept_id": "uniform-convergence"})
	sessionID := sessionResult["id"].(string)
	probe := call("recommend_session_probe", map[string]any{"session_id": sessionID})
	if probe["action"] != "probe" {
		t.Fatal(probe)
	}
	committed := call("open_learning_question", map[string]any{
		"session_id": sessionID, "concept_id": "uniform-convergence", "kind": "diagnostic",
		"prompt": "Which statement describes uniform convergence?", "response_kind": "multiple_choice",
		"options":         []map[string]any{{"id": "a", "label": "Each point may have its own N"}, {"id": "b", "label": "One N suffices for all points"}},
		"expected_answer": "b", "grading_criteria": "Common index uniform in x",
	})
	questionID := committed["id"].(string)
	if strings.Contains(string(mustJSON(committed)), "Common index uniform") || strings.Contains(string(mustJSON(committed)), "expected_answer") {
		t.Fatal("committed question leaked answer")
	}
	viewQuestion := call("show_learning_question", map[string]any{"question_id": questionID})
	if viewQuestion["view"] != "learning_question" || strings.Contains(string(mustJSON(viewQuestion)), "Common index uniform") {
		t.Fatal("committed question view leaked answer")
	}
	if call("get_pending_learning_question", map[string]any{"session_id": sessionID})["pending"] != true {
		t.Fatal("pending question not persisted")
	}
	call("answer_learning_question", map[string]any{"question_id": questionID, "learner_response": "b"})
	graded := call("grade_learning_question", map[string]any{"question_id": questionID, "verdict": "supported", "evaluation": "Correct index selection", "confidence": .91})
	if graded["expected_answer"] != "b" || graded["mastery_unchanged"] != true {
		t.Fatal(graded)
	}
	call("advance_learning_session", map[string]any{"session_id": sessionID, "next_phase": "plan"})
	plan := call("build_learning_plan", map[string]any{"session_id": sessionID})
	if plan["plan_version"].(float64) != 1 {
		t.Fatal(plan)
	}
	sessionView := call("show_learning_session", map[string]any{"session_id": sessionID})
	if sessionView["view"] != "session" {
		t.Fatal(sessionView)
	}
	// The source/audit pipeline returns source-derived knowledge without learner mastery.
	source := call("ingest_source_text", map[string]any{"label": "Lecture", "kind": "notes", "mime_type": "text/plain", "text": "A limit is a foundational construction used in analysis."})
	stage := call("next_extraction", map[string]any{"source_id": source["id"]})
	if stage["action"] != "extract" {
		t.Fatal(stage)
	}
	staged := call("propose_extraction", map[string]any{"span_id": stage["span_id"], "claims": []map[string]any{{"statement": "A limit is a foundational construction", "quote": "A limit is a foundational construction", "context": "analysis", "domain": "mathematics"}}})
	call("audit_extraction", map[string]any{"batch_id": staged["batch_id"], "decisions": []map[string]any{{"index": 0, "action": "accept"}}, "chains": []any{}})
	reasoning := call("get_reasoning_graph", map[string]any{"limit": 50})
	if len(reasoning["propositions"].([]any)) != 1 {
		t.Fatal(reasoning)
	}
	match := call("search_knowledge", map[string]any{"query": "limit", "limit": 5})
	if len(match["matches"].([]any)) != 1 {
		t.Fatal(match)
	}

	// A practice demand precommits its answer and is linked to one active item.
	practice := call("start_practice_session", map[string]any{"goal": "Practice limits", "concept_ids": []string{"uniform-convergence"}})
	practiceID := practice["id"].(string)
	if call("recommend_practice", map[string]any{"session_id": practiceID})["action"] != "pose" {
		t.Fatal("missing practice suggestion")
	}
	item := call("create_practice_problem", map[string]any{"session_id": practiceID, "target_concept_id": "uniform-convergence", "method": "recall", "cue_class": "first-context", "difficulty": 2, "title": "Recall limit", "statement": "Determine the relevant theorem from an unfamiliar inequality", "kind": "proof", "reference_solution": "Use the definition of uniform convergence", "grading_rubric": "Look for a common N"})
	practiceProblem := item["exercise"].(map[string]any)
	if _, leak := practiceProblem["reference_solution"]; leak {
		t.Fatal("answer exposed", practiceProblem)
	}
	if call("recommend_practice", map[string]any{"session_id": practiceID})["action"] != "finish_active_problem" {
		t.Fatal("active practice problem not gated")
	}
	call("save_problem_draft", map[string]any{"problem_id": practiceProblem["id"], "draft": "An independent definition-based attempt"})
	call("finish_problem", map[string]any{"problem_id": practiceProblem["id"], "outcome": "incomplete", "evaluation": "Needs a common quantifier", "confidence": 0.8})
	outcome := call("record_practice_outcome", map[string]any{"practice_item_id": item["practice_item_id"], "correct": false, "miss_class": "execution", "remarks": "Wrong quantifier order"})
	if outcome["miss_class"] != "execution" {
		t.Fatal(outcome)
	}

	// Binary originals can be returned as model-visible MCP image content.
	tinyPNG := append([]byte("\x89PNG\r\n\x1a\n"), []byte("fixture")...)
	attached := call("upload_attachment", map[string]any{"owner_kind": "session", "owner_id": sessionID, "filename": "handwriting.png", "mime_type": "image/png", "base64": base64.StdEncoding.EncodeToString(tinyPNG)})
	imageResult, imageErr := session.CallTool(context.Background(), &mcp.CallToolParams{Name: "inspect_attachment", Arguments: map[string]any{"attachment_id": attached["id"]}})
	if imageErr != nil {
		t.Fatal(imageErr)
	}
	if len(imageResult.Content) != 1 {
		t.Fatal("expected original image")
	}
	original, ok := imageResult.Content[0].(*mcp.ImageContent)
	if !ok || original.MIMEType != "image/png" || string(original.Data) != string(tinyPNG) {
		t.Fatal("original image not delivered to model")
	}
	resource, err := session.ReadResource(context.Background(), &mcp.ReadResourceParams{URI: UIURI})
	if err != nil {
		t.Fatal(err)
	}
	if len(resource.Contents) != 1 || resource.Contents[0].MIMEType != "text/html;profile=mcp-app" || !strings.Contains(resource.Contents[0].Text, "sessionView") {
		t.Fatal("MCP App resource missing or malformed")
	}
}

func TestHostAndOriginFiltering(t *testing.T) {
	svc := testService(t)
	cases := []struct {
		host, origin string
		status       int
	}{
		{"attacker.invalid", "", http.StatusForbidden},
		{"127.0.0.1:8000", "https://attacker.invalid", http.StatusForbidden},
		{"127.0.0.1:8000", "", http.StatusMethodNotAllowed},
	}
	for _, tt := range cases {
		req := httptest.NewRequest(http.MethodGet, "http://127.0.0.1:8000/mcp", nil)
		req.Host = tt.host
		if tt.origin != "" {
			req.Header.Set("Origin", tt.origin)
		}
		w := httptest.NewRecorder()
		svc.Handler("127.0.0.1:*,localhost:*", "").ServeHTTP(w, req)
		if w.Code != tt.status {
			t.Fatalf("host %s origin %s: status %d wanted %d", tt.host, tt.origin, w.Code, tt.status)
		}
	}
}
func mustJSON(value any) []byte { out, _ := json.Marshal(value); return out }

func TestMCPBearerAuthentication(t *testing.T) {
	svc := testService(t)
	const token = "valid-token-0123456789abcdef0123456789"
	handler := svc.HandlerWithBearer("127.0.0.1:*", "", token)
	cases := []struct {
		name, path, authorization, host, origin string
		want                                    int
	}{
		{"health is public", "/healthz", "", "127.0.0.1:9000", "", http.StatusOK},
		{"missing bearer", "/mcp", "", "127.0.0.1:9000", "", http.StatusUnauthorized},
		{"wrong bearer", "/mcp", "Bearer wrong", "127.0.0.1:9000", "", http.StatusUnauthorized},
		{"correct bearer", "/mcp", "Bearer " + token, "127.0.0.1:9000", "", http.StatusMethodNotAllowed},
		{"invalid host", "/mcp", "Bearer " + token, "external.example", "", http.StatusForbidden},
		{"invalid origin", "/mcp", "Bearer " + token, "127.0.0.1:9000", "https://attacker.example", http.StatusForbidden},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "http://127.0.0.1:9000"+tc.path, nil)
			req.Host = tc.host
			req.Header.Set("Authorization", tc.authorization)
			if tc.origin != "" {
				req.Header.Set("Origin", tc.origin)
			}
			res := httptest.NewRecorder()
			handler.ServeHTTP(res, req)
			if res.Code != tc.want {
				t.Fatalf("status = %d; want %d", res.Code, tc.want)
			}
			if res.Code == http.StatusUnauthorized && res.Header().Get("WWW-Authenticate") == "" {
				t.Fatal("missing Bearer challenge")
			}
		})
	}
}
