package agentflow

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

type scriptedDecisions struct {
	decisions []AgentAction
	index     int
}

func (*scriptedDecisions) Propose(context.Context, string, map[string]string, []string) (map[string]string, error) {
	return nil, errors.New("direct file authorship disabled")
}
func (a *scriptedDecisions) Decide(_ context.Context, _ AgentBrief, session AgentSession) (AgentAction, error) {
	if a.index >= len(a.decisions) {
		return AgentAction{}, errors.New("decision script exhausted")
	}
	next := a.decisions[a.index]
	a.index++
	return next, nil
}

func autonomousProgram() Program {
	p := sampleProgram()
	p.Autonomous = true
	p.Changes = nil
	p.ContextPaths = []string{"README.md"}
	p.AllowedPaths = []string{"README.md"}
	return p
}
func agentDecisions() []AgentAction {
	return []AgentAction{
		{Action: "plan", Plan: []string{"Inspect documentation", "Patch source", "Verify changes"}},
		{Action: "list"},
		{Action: "search", Query: "package"},
		{Action: "read", Path: "README.md"},
		{Action: "patch", Path: "README.md", ExpectedSHA256: shaHex("package sample"), Hunks: []PatchHunk{{Find: "package", Replace: "chapter"}}},
		{Action: "check", Command: "go test ./..."},
		{Action: "finish", Summary: "Corrected the documentation and verified checks"},
	}
}
func stepRun(t *testing.T, e *Engine, id string) {
	t.Helper()
	advanced, err := (&Runner{Store: e.Store, Worker: e, Lease: time.Minute}).RunOne(context.Background(), id)
	if err != nil || !advanced {
		t.Fatalf("run failed to advance: advanced=%v error=%v", advanced, err)
	}
}
func TestAutonomousAgentExploresPlansPatchesChecksAndPublishes(t *testing.T) {
	e, compute := testEngine(t)
	model := &scriptedDecisions{decisions: agentDecisions()}
	e.Author = model
	program := autonomousProgram()
	run, err := e.Submit(program)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 12; i++ {
		current, err := e.Get(run.ID)
		if err != nil {
			t.Fatal(err)
		}
		if current.Status == Succeeded {
			break
		}
		stepRun(t, e, run.ID)
	}
	current, err := e.Get(run.ID)
	if err != nil {
		t.Fatal(err)
	}
	if current.Status != Succeeded || compute.drafts != 1 || compute.writes != 1 {
		t.Fatalf("autonomous run did not complete: %+v drafts=%d writes=%d", current, compute.drafts, compute.writes)
	}
	if current.Agent == nil || current.Agent.Turns != 7 || len(current.Agent.ChangedPaths) != 1 || current.Agent.ChangedPaths[0] != "README.md" {
		t.Fatalf("missing autonomous progress: %+v", current.Agent)
	}
	if compute.files["repos/example__repo/README.md"] != "chapter sample" {
		t.Fatal("structured patch not applied")
	}
	persisted, err := e.Store.Get(run.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(persisted.Agent.Events) != 7 || !persisted.Agent.Completed {
		t.Fatalf("agent transcript incomplete: %+v", persisted.Agent)
	}
	raw, _ := json.Marshal(current)
	if strings.Contains(string(raw), "chapter sample") || strings.Contains(string(raw), "package sample") {
		t.Fatal("source contents leaked through public run summary")
	}
}
func TestAutonomousSessionSurvivesStoreReopen(t *testing.T) {
	e, compute := testEngine(t)
	decisions := &scriptedDecisions{decisions: agentDecisions()}
	e.Author = decisions
	run, err := e.Submit(autonomousProgram())
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 5; i++ {
		stepRun(t, e, run.ID)
	} // provision, checkout, plan, list, search
	fresh, err := Open(e.Store.dir)
	if err != nil {
		t.Fatal(err)
	}
	e = &Engine{Store: fresh, Compute: compute, Author: decisions, AllowedProjects: map[string]bool{"research": true}}
	for i := 0; i < 7; i++ {
		stepRun(t, e, run.ID)
	}
	summary, err := e.Get(run.ID)
	if err != nil {
		t.Fatal(err)
	}
	if summary.Status != Succeeded || summary.Agent == nil || summary.Agent.Turns != 7 {
		t.Fatalf("session did not resume: %+v", summary)
	}
}
func TestAgentPatchRejectsAmbiguousAndStaleContext(t *testing.T) {
	if _, err := applyPatch("repeat repeat", []PatchHunk{{Find: "repeat", Replace: "fix"}}); err == nil {
		t.Fatal("ambiguous patch accepted")
	}
	if _, err := applyPatch("single", []PatchHunk{{Find: "absent", Replace: "fix"}}); err == nil {
		t.Fatal("missing patch context accepted")
	}
	decisions := agentDecisions()
	decisions[0] = AgentAction{Action: "patch", Path: "README.md", ExpectedSHA256: strings.Repeat("a", 64), Hunks: []PatchHunk{{Find: "package", Replace: "fix"}}}
	e, _ := testEngine(t)
	e.Author = &scriptedDecisions{decisions: decisions}
	run, err := e.Submit(autonomousProgram())
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 3; i++ {
		stepRun(t, e, run.ID)
	}
	w, err := e.Store.Get(run.ID)
	if err != nil {
		t.Fatal(err)
	}
	if w.Agent == nil || len(w.Agent.Events) != 1 || !strings.Contains(w.Agent.Events[0].Observation, "digest conflict") {
		t.Fatalf("patch conflict was not preserved for replanning: %+v", w.Agent)
	}
}
func TestAgentCannotFinishWithoutFreshApprovedChecks(t *testing.T) {
	e, compute := testEngine(t)
	patch := agentDecisions()[4]
	model := &scriptedDecisions{decisions: []AgentAction{patch, {Action: "finish", Summary: "premature"}, {Action: "check", Command: "go test ./..."}, {Action: "finish", Summary: "done"}}}
	e.Author = model
	run, err := e.Submit(autonomousProgram())
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 10; i++ {
		info, err := e.Get(run.ID)
		if err != nil {
			t.Fatal(err)
		}
		if info.Status == Succeeded {
			break
		}
		stepRun(t, e, run.ID)
	}
	summary, err := e.Get(run.ID)
	if err != nil {
		t.Fatal(err)
	}
	if summary.Status != Succeeded || compute.drafts != 1 || summary.Agent.Turns != 4 {
		t.Fatalf("premature finish escaped check boundary: %+v", summary)
	}
}
func TestAgentRejectsUnapprovedEditsAndShellCommands(t *testing.T) {
	cases := []AgentAction{
		{Action: "patch", Path: "secrets.txt", ExpectedSHA256: shaHex("x"), Hunks: []PatchHunk{{Find: "x", Replace: "y"}}},
		{Action: "check", Command: "curl https://example.invalid/upload"},
		{Action: "read", Path: "../sibling"},
		{Action: "create", Path: ".git/hooks/pre-commit", Content: "evil"},
	}
	for _, decision := range cases {
		t.Run(decision.Action+decision.Path+decision.Command, func(t *testing.T) {
			e, compute := testEngine(t)
			e.Author = &scriptedDecisions{decisions: []AgentAction{decision}}
			run, err := e.Submit(autonomousProgram())
			if err != nil {
				t.Fatal(err)
			}
			for i := 0; i < 3; i++ {
				stepRun(t, e, run.ID)
			}
			summary, err := e.Get(run.ID)
			if err != nil {
				t.Fatal(err)
			}
			if summary.Status != Failed || compute.writes != 0 || compute.drafts != 0 {
				t.Fatalf("unauthorized decision executed: %+v", summary)
			}
		})
	}
}
func TestCheckpointLeaseFencesInterruptedAgent(t *testing.T) {
	e, _ := testEngine(t)
	e.Author = &scriptedDecisions{decisions: agentDecisions()}
	run, err := e.Submit(autonomousProgram())
	if err != nil {
		t.Fatal(err)
	}
	stepRun(t, e, run.ID)
	stepRun(t, e, run.ID)
	clock := time.Now()
	e.Store.now = func() time.Time { return clock }
	first, err := e.Store.ClaimReady(run.ID, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	session := AgentSession{Plan: []string{"Inspect"}, Turns: 1, Events: []AgentEvent{{Action: "plan", Observation: "recorded"}}}
	if err := e.Store.CheckpointAgent(first, session); err != nil {
		t.Fatal(err)
	}
	clock = clock.Add(2 * time.Second)
	replacement, err := e.Store.ClaimReady(run.ID, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if err := e.Store.CheckpointAgent(first, session); err == nil {
		t.Fatal("stale agent checkpoint accepted")
	}
	if err := e.Store.Yield(first); err == nil {
		t.Fatal("stale agent yield accepted")
	}
	if err := e.Store.Yield(replacement); err != nil {
		t.Fatal(err)
	}
}
func TestInterruptedCheckNeverReplaysUncertainCommand(t *testing.T) {
	e, compute := testEngine(t)
	e.Author = &scriptedDecisions{decisions: agentDecisions()}
	run, err := e.Submit(autonomousProgram())
	if err != nil {
		t.Fatal(err)
	}
	stepRun(t, e, run.ID)
	stepRun(t, e, run.ID)
	clock := time.Now()
	e.Store.now = func() time.Time { return clock }
	first, err := e.Store.ClaimReady(run.ID, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	session := AgentSession{Pending: &AgentPending{Action: AgentAction{Action: "check", Command: "go test ./..."}}}
	if err := e.Store.CheckpointAgent(first, session); err != nil {
		t.Fatal(err)
	}
	clock = clock.Add(2 * time.Second)
	next, err := e.Store.ClaimReady(run.ID, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	_, err = e.Dispatch(context.Background(), next)
	if err == nil || !strings.Contains(err.Error(), "uncertain") {
		t.Fatalf("interrupted check replayed or undiagnosed: %v", err)
	}
	for _, name := range compute.calls {
		if name == "workspace_call" {
			t.Fatal("uncertain check triggered worker tool call")
		}
	}
}
func TestInterruptedPatchReconcilesConfirmedResult(t *testing.T) {
	e, compute := testEngine(t)
	e.Author = &scriptedDecisions{decisions: agentDecisions()}
	run, err := e.Submit(autonomousProgram())
	if err != nil {
		t.Fatal(err)
	}
	stepRun(t, e, run.ID)
	stepRun(t, e, run.ID)
	clock := time.Now()
	e.Store.now = func() time.Time { return clock }
	first, err := e.Store.ClaimReady(run.ID, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	w, err := e.Store.Get(run.ID)
	if err != nil {
		t.Fatal(err)
	}
	proposed := agentDecisions()[4]
	pending, err := e.prepareAgentAction(context.Background(), w, proposed)
	if err != nil {
		t.Fatal(err)
	}
	if err := e.Store.CheckpointAgent(first, AgentSession{Pending: pending}); err != nil {
		t.Fatal(err)
	}
	compute.files["repos/example__repo/README.md"] = pending.Contents // side effect completed before crash
	clock = clock.Add(2 * time.Second)
	second, err := e.Store.ClaimReady(run.ID, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	_, err = e.Dispatch(context.Background(), second)
	if !errors.Is(err, ErrAgentYield) {
		t.Fatalf("patch did not reconcile: %v", err)
	}
	if compute.writes != 0 {
		t.Fatal("previously applied patch was written twice")
	}
	if err := e.Store.Yield(second); err != nil {
		t.Fatal(err)
	}
	latest, err := e.Store.Get(run.ID)
	if err != nil {
		t.Fatal(err)
	}
	if latest.Agent == nil || latest.Agent.Changed["README.md"] != pending.AfterSHA256 {
		t.Fatal("recovered patch digest was not persisted")
	}
}

func TestAutonomousCreationRequiresExplicitFileScope(t *testing.T) {
	e, compute := testEngine(t)
	p := autonomousProgram()
	p.AllowedPaths = []string{"internal/new.go"}
	model := &scriptedDecisions{decisions: []AgentAction{
		{Action: "plan", Plan: []string{"Create module", "Verify"}},
		{Action: "create", Path: "internal/new.go", Content: "package internal\n"},
		{Action: "check", Command: "go test ./..."},
		{Action: "finish", Summary: "Added a tested module"},
	}}
	e.Author = model
	run, err := e.Submit(p)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 9; i++ {
		info, _ := e.Get(run.ID)
		if info.Status == Succeeded {
			break
		}
		stepRun(t, e, run.ID)
	}
	summary, err := e.Get(run.ID)
	if err != nil {
		t.Fatal(err)
	}
	if summary.Status != Succeeded || compute.writes != 1 || compute.files["repos/example__repo/internal/new.go"] != "package internal\n" {
		t.Fatalf("approved file creation failed: %+v", summary)
	}
}

func TestAgentCheckpointDoesNotPermitCrossRunReplay(t *testing.T) {
	e, _ := testEngine(t)
	e.Author = &scriptedDecisions{decisions: agentDecisions()}
	first, err := e.Submit(autonomousProgram())
	if err != nil {
		t.Fatal(err)
	}
	second, err := e.Submit(autonomousProgram())
	if err != nil {
		t.Fatal(err)
	}
	stepRun(t, e, first.ID)
	stepRun(t, e, first.ID)
	stepRun(t, e, second.ID)
	stepRun(t, e, second.ID)
	claim, err := e.Store.ClaimReady(first.ID, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	forged := claim
	forged.WorkflowID = second.ID
	if err := e.Store.CheckpointAgent(forged, AgentSession{}); err == nil {
		t.Fatal("claim was replayed across runs")
	}
	if err := e.Store.Yield(forged); err == nil {
		t.Fatal("claim yielded another run")
	}
}

func TestModelDecisionTransportRejectsUntrustedCommands(t *testing.T) {
	decision := `{"action":"check","command":"curl https://example.invalid"}`
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var payload map[string]any
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatal(err)
		}
		if payload["model"] != "sandbox-model" {
			t.Fatal("model endpoint contract mismatch")
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"choices": []map[string]any{{"message": map[string]string{"content": decision}, "finish_reason": "stop"}}})
	}))
	defer server.Close()
	model, err := NewModelAuthor(server.URL+"/v1/chat/completions", "sandbox-model", "")
	if err != nil {
		t.Fatal(err)
	}
	brief := AgentBrief{Objective: "Fix package", Repository: "example/repo", AllowedPaths: []string{"README.md"}, Checks: []string{"go test ./..."}}
	if _, err := model.Decide(context.Background(), brief, AgentSession{}); err == nil {
		t.Fatal("unapproved command accepted from model")
	}
	decision = `{"action":"plan","plan":["Read sources","Test"],"surprise":true}`
	if _, err := model.Decide(context.Background(), brief, AgentSession{}); err == nil {
		t.Fatal("unknown model response fields accepted")
	}
	decision = `{"action":"plan","plan":["Read sources","Test"]}`
	action, err := model.Decide(context.Background(), brief, AgentSession{})
	if err != nil || action.Action != "plan" {
		t.Fatalf("valid planning decision rejected: %+v %v", action, err)
	}
}

func TestModelDecisionIncludesPersistentObservations(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Messages []struct {
				Role    string `json:"role"`
				Content string `json:"content"`
			} `json:"messages"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		if len(body.Messages) != 2 || !strings.Contains(body.Messages[1].Content, "previous check exited 1") || !strings.Contains(body.Messages[1].Content, "Repair failing test") {
			t.Fatal("reasoning context lost prior evidence")
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"choices": []map[string]any{{"message": map[string]string{"content": `{"action":"read","path":"README.md"}`}, "finish_reason": "stop"}}})
	}))
	defer server.Close()
	model, err := NewModelAuthor(server.URL+"/v1/chat/completions", "sandbox-model", "")
	if err != nil {
		t.Fatal(err)
	}
	brief := AgentBrief{Objective: "Repair failing test", Repository: "example/repo", AllowedPaths: []string{"README.md"}, Checks: []string{"go test ./..."}}
	state := AgentSession{Plan: []string{"Investigate", "Repair"}, Events: []AgentEvent{{Action: "check", Observation: "previous check exited 1"}}, Turns: 1}
	a, err := model.Decide(context.Background(), brief, state)
	if err != nil || a.Action != "read" {
		t.Fatalf("model decision failed: %+v %v", a, err)
	}
}

func TestAgentUpdatesPlanUsingPriorVerificationEvidence(t *testing.T) {
	e, compute := testEngine(t)
	actions := []AgentAction{
		{Action: "plan", Plan: []string{"Inspect source", "Verify"}},
		{Action: "read", Path: "README.md"},
		{Action: "plan", Plan: []string{"Revise README", "Verify"}},
		agentDecisions()[4],
		{Action: "check", Command: "go test ./..."},
		{Action: "finish", Summary: "Plan revised and checks passed"},
	}
	e.Author = &scriptedDecisions{decisions: actions}
	run, err := e.Submit(autonomousProgram())
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 11; i++ {
		info, _ := e.Get(run.ID)
		if info.Status == Succeeded {
			break
		}
		stepRun(t, e, run.ID)
	}
	summary, err := e.Get(run.ID)
	if err != nil {
		t.Fatal(err)
	}
	if summary.Status != Succeeded || len(summary.Agent.Plan) != 2 || summary.Agent.Plan[0] != "Revise README" || compute.drafts != 1 {
		t.Fatalf("replanning failed to preserve final plan: %+v", summary)
	}
}

func TestSensitiveSourcePathsAreNotAvailableToModel(t *testing.T) {
	protected := []string{".env", "config/.env.production", ".aws/credentials", ".ssh/id_rsa", "secrets.json", "tls/client.key", "tls/cert.pem"}
	for _, name := range protected {
		if !sensitiveSourcePath(name) {
			t.Fatalf("sensitive source was not rejected: %q", name)
		}
	}
	for _, name := range []string{"README.md", "config/.env.example", "main.go", "pkg/schema.json"} {
		if sensitiveSourcePath(name) {
			t.Fatalf("safe source was blocked: %q", name)
		}
	}
	brief := AgentBrief{AllowedPaths: []string{"README.md"}}
	if err := (AgentAction{Action: "read", Path: ".env"}).validate(brief); err == nil {
		t.Fatal("model could read a credential file")
	}
}

func TestAgentSearchArgumentsAreQuotedAndRepositoryScoped(t *testing.T) {
	e, compute := testEngine(t)
	query := "test' && rm -rf / #"
	e.Author = &scriptedDecisions{decisions: []AgentAction{{Action: "search", Query: query}}}
	run, err := e.Submit(autonomousProgram())
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 3; i++ {
		stepRun(t, e, run.ID)
	}
	if compute.writes != 0 || compute.drafts != 0 {
		t.Fatal("read-only search produced a mutation")
	}
	if quoted := shellQuote(query); quoted != "'test'\\'' && rm -rf / #'" {
		t.Fatalf("search term not shell quoted: %q", quoted)
	}
	w, err := e.Store.Get(run.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(w.Agent.Events) != 1 || w.Agent.Events[0].Action != "search" {
		t.Fatal("search observation not persisted")
	}
}
