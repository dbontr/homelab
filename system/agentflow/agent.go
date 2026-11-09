package agentflow

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"path"
	"path/filepath"
	"sort"
	"strings"
)

const maxAgentTurns = 40

var ErrAgentYield = errors.New("agent turn checkpointed")
var ErrAgentRejected = errors.New("agent authorization or certainty boundary failed")

// AgentBrief is the immutable scope of an autonomous engineering run.
type AgentBrief struct {
	Objective    string   `json:"objective"`
	Repository   string   `json:"repository"`
	AllowedPaths []string `json:"allowed_paths"`
	Checks       []string `json:"checks"`
	ContextPaths []string `json:"context_paths,omitempty"`
	LinkedRepositories []RepositoryPin `json:"linked_repositories,omitempty"`
}

type PatchHunk struct {
	Find    string `json:"find"`
	Replace string `json:"replace"`
}

// AgentAction is the complete, untrusted proposal made by the model.
// Only the runtime can turn it into a scoped compute operation.
type AgentAction struct {
	Action         string      `json:"action"`
	Repository     string      `json:"repository,omitempty"`
	Reason         string      `json:"reason,omitempty"`
	Plan           []string    `json:"plan,omitempty"`
	Path           string      `json:"path,omitempty"`
	Query          string      `json:"query,omitempty"`
	ExpectedSHA256 string      `json:"expected_sha256,omitempty"`
	Hunks          []PatchHunk `json:"hunks,omitempty"`
	Content        string      `json:"content,omitempty"`
	Command        string      `json:"command,omitempty"`
	Summary        string      `json:"summary,omitempty"`
}

type AgentEvent struct {
	Action      string `json:"action"`
	Reason      string `json:"reason,omitempty"`
	Target      string `json:"target,omitempty"`
	Observation string `json:"observation"`
}

type AgentPending struct {
	Action       AgentAction `json:"action"`
	BeforeSHA256 string      `json:"before_sha256,omitempty"`
	AfterSHA256  string      `json:"after_sha256,omitempty"`
	Contents     string      `json:"contents,omitempty"`
}

type AgentSession struct {
	Plan         []string          `json:"plan,omitempty"`
	Events       []AgentEvent      `json:"events,omitempty"`
	Changed      map[string]string `json:"changed,omitempty"`
	ChecksPassed map[string]bool   `json:"checks_passed,omitempty"`
	Pending      *AgentPending     `json:"pending,omitempty"`
	Turns        int               `json:"turns"`
	Completed    bool              `json:"completed"`
	Summary      string            `json:"summary,omitempty"`
}

type AgentProgress struct {
	Plan         []string `json:"plan,omitempty"`
	Turns        int      `json:"turns"`
	LastAction   string   `json:"last_action,omitempty"`
	ChangedPaths []string `json:"changed_paths,omitempty"`
	Completed    bool     `json:"completed"`
	Summary      string   `json:"summary,omitempty"`
}

func publicAgentProgress(state *AgentSession) *AgentProgress {
	if state == nil {
		return nil
	}
	progress := &AgentProgress{Plan: append([]string(nil), state.Plan...), Turns: state.Turns, Completed: state.Completed, Summary: state.Summary}
	for path := range state.Changed {
		progress.ChangedPaths = append(progress.ChangedPaths, path)
	}
	// Deliberately exclude repository contents and model observations from the gateway.
	sort.Strings(progress.ChangedPaths)
	if n := len(state.Events); n > 0 {
		progress.LastAction = state.Events[n-1].Action
	}
	return progress
}

func shaHex(s string) string { sum := sha256.Sum256([]byte(s)); return hex.EncodeToString(sum[:]) }
func hexDigest(s string) bool {
	if len(s) != 64 {
		return false
	}
	for _, r := range s {
		if !('0' <= r && r <= '9' || 'a' <= r && r <= 'f') {
			return false
		}
	}
	return true
}

// DecisionAuthor reasons over the bounded persisted transcript. Model output
// is validated independently before any compute action is issued.
type DecisionAuthor interface {
	Decide(context.Context, AgentBrief, AgentSession) (AgentAction, error)
}

func (a AgentAction) validate(brief AgentBrief) error {
	if a.Repository != "" && a.Repository != brief.Repository {
		found := false
		for _, linked := range brief.LinkedRepositories {
			if a.Repository == linked.Repository { found = true; break }
		}
		if !found { return errors.New("repository is not pinned to this engineering run") }
		if a.Action != "list" && a.Action != "search" && a.Action != "map" && a.Action != "symbols" && a.Action != "references" && a.Action != "read" {
			return errors.New("linked repository access is read-only")
		}
	}
	if len(a.Reason) > 400 || len(a.Summary) > 400 || len(a.Path) > 255 || len(a.Query) > 160 || len(a.Hunks) > 8 || len(a.Content) > 24000 || len(a.Plan) > 8 {
		return errors.New("agent decision exceeds limits")
	}
	switch a.Action {
	case "plan":
		if len(a.Plan) == 0 {
			return errors.New("agent plan cannot be empty")
		}
		for _, task := range a.Plan {
			if strings.TrimSpace(task) == "" || len(task) > 240 {
				return errors.New("invalid plan task")
			}
		}
	case "list":
		if a.Path != "" && !validSourcePath(a.Path) {
			return errors.New("invalid directory path")
		}
	case "search":
		if strings.TrimSpace(a.Query) == "" || strings.ContainsAny(a.Query, "\x00\r\n") {
			return errors.New("invalid repository search query")
		}
	case "map":
		if !validCodeScope(a.Path) {
			return errors.New("invalid source map scope")
		}
	case "symbols":
		if !validSourcePath(a.Path) || sensitiveSourcePath(a.Path) || sourceLanguage(a.Path) == "" {
			return errors.New("invalid source outline path")
		}
	case "references":
		if !codeIdentifier.MatchString(a.Query) || !validCodeScope(a.Path) {
			return errors.New("invalid source reference query")
		}
	case "read":
		if !validSourcePath(a.Path) || sensitiveSourcePath(a.Path) {
			return errors.New("invalid or protected repository file path")
		}
	case "patch", "create":
		approved := false
		for _, p := range brief.AllowedPaths {
			if p == a.Path {
				approved = true
				break
			}
		}
		if !approved || !validSourcePath(a.Path) || sensitiveSourcePath(a.Path) {
			return errors.New("edit path is outside approved scope")
		}
		if a.Action == "patch" {
			if !hexDigest(a.ExpectedSHA256) || len(a.Hunks) == 0 {
				return errors.New("patch needs a source digest and hunks")
			}
			for _, h := range a.Hunks {
				if h.Find == "" || len(h.Find) > 16000 || len(h.Replace) > 16000 {
					return errors.New("invalid patch hunk")
				}
			}
		} else if len(a.Content) == 0 {
			return errors.New("created file must contain source")
		}
	case "check":
		permitted := false
		for _, check := range brief.Checks {
			if check == a.Command {
				permitted = true
				break
			}
		}
		if !permitted {
			return errors.New("agent command is not among owner-approved checks")
		}
	case "finish":
		// Completion also requires all owner checks to have passed after the last edit.
	default:
		return errors.New("unsupported agent decision")
	}
	return nil
}

// applyPatch enforces non-overlapping, unambiguous changes against a pinned
// source digest. It never applies a fuzzy patch or changes unrelated files.
func applyPatch(content string, hunks []PatchHunk) (string, error) {
	if len(hunks) == 0 || len(hunks) > 8 {
		return "", errors.New("invalid patch hunk count")
	}
	type replacement struct {
		start, end int
		text       string
	}
	edits := make([]replacement, 0, len(hunks))
	for _, h := range hunks {
		if h.Find == "" || strings.Count(content, h.Find) != 1 {
			return "", errors.New("patch context is missing or ambiguous")
		}
		start := strings.Index(content, h.Find)
		edits = append(edits, replacement{start: start, end: start + len(h.Find), text: h.Replace})
	}
	sort.Slice(edits, func(i, j int) bool { return edits[i].start < edits[j].start })
	var out strings.Builder
	last := 0
	for _, edit := range edits {
		if edit.start < last {
			return "", errors.New("overlapping patch hunks are not permitted")
		}
		out.WriteString(content[last:edit.start])
		out.WriteString(edit.text)
		last = edit.end
	}
	out.WriteString(content[last:])
	if out.Len() == 0 || out.Len() > 24000 {
		return "", errors.New("patched source exceeds size limits")
	}
	return out.String(), nil
}

// Secret-bearing configuration files are not eligible for model inspection.
// Worker isolation and project membership are still required independently.
func sensitiveSourcePath(filename string) bool {
	for _, part := range strings.Split(strings.ToLower(filename), "/") {
		switch part {
		case ".ssh", ".aws", ".gnupg", ".netrc", ".npmrc", ".pypirc", "id_rsa", "id_ed25519", "credentials.json", "secrets.json", ".env":
			return true
		}
		if strings.HasPrefix(part, ".env.") && part != ".env.example" && part != ".env.sample" {
			return true
		}
	}
	name := strings.ToLower(filepath.Base(filename))
	return strings.HasSuffix(name, ".pem") || strings.HasSuffix(name, ".key") || strings.HasSuffix(name, ".p12")
}

func shellQuote(s string) string { return "'" + strings.ReplaceAll(s, "'", "'\\''") + "'" }

func (e *Engine) runAgentTurn(ctx context.Context, claim Claim, w Workflow) (json.RawMessage, error) {
	var brief AgentBrief
	if err := json.Unmarshal(claim.Input, &brief); err != nil {
		return nil, err
	}
	model, ok := e.Author.(DecisionAuthor)
	if !ok {
		return nil, errors.New("autonomous decision provider unavailable")
	}
	state := AgentSession{}
	if w.Agent != nil {
		state = *w.Agent
	}
	if state.Changed == nil {
		state.Changed = map[string]string{}
	}
	if state.ChecksPassed == nil {
		state.ChecksPassed = map[string]bool{}
	}
	if state.Completed {
		return nil, errors.New("agent already completed")
	}
	if state.Turns >= maxAgentTurns {
		return nil, errors.New("autonomous decision budget exhausted")
	}
	if state.Pending != nil && state.Pending.Action.Action == "check" {
		return nil, fmt.Errorf("%w: check completion uncertain after interrupted execution", ErrAgentRejected)
	}
	if state.Pending == nil {
		decision, err := model.Decide(ctx, brief, state)
		if err != nil {
			return nil, err
		}
		if err := decision.validate(brief); err != nil {
			return nil, fmt.Errorf("%w: %v", ErrAgentRejected, err)
		}
		if decision.Action == "finish" {
			missing := len(state.Changed) == 0
			for _, check := range brief.Checks {
				if !state.ChecksPassed[check] {
					missing = true
				}
			}
			if missing {
				state.Events = append(state.Events, AgentEvent{Action: "finish", Reason: decision.Reason, Observation: "Completion requires at least one approved edit and all owner checks after the last edit"})
				state.Turns++
				if err := e.Store.CheckpointAgent(claim, state); err != nil {
					return nil, err
				}
				return nil, ErrAgentYield
			}
		}
		pending, err := e.prepareAgentAction(ctx, w, decision)
		if err != nil {
			if decision.Action != "patch" {
				return nil, err
			}
			// Patch context conflicts are expected when a source file changes;
			// they are evidence for replanning, not permission to force an edit.
			state.Events = append(state.Events, AgentEvent{Action: "patch", Reason: decision.Reason, Target: decision.Path, Observation: "Patch context rejected: " + err.Error()})
			state.Turns++
			if err := e.Store.CheckpointAgent(claim, state); err != nil {
				return nil, err
			}
			return nil, ErrAgentYield
		}
		state.Pending = pending
		if err := e.Store.CheckpointAgent(claim, state); err != nil {
			return nil, err
		}
	}
	observation, err := e.executeAgentAction(ctx, w, brief, &state)
	if err != nil {
		return nil, err
	}
	action := state.Pending.Action
	target := action.Path
	if target == "" {
		if action.Action == "check" {
			target = action.Command
		} else if action.Action == "search" || action.Action == "references" {
			target = action.Query
		}
	}
	if len(target) > 160 {
		target = target[:160]
	}
	if len(observation) > 16000 {
		observation = observation[:16000]
	}
	state.Events = append(state.Events, AgentEvent{Action: action.Action, Reason: action.Reason, Target: target, Observation: observation})
	state.Turns++
	state.Pending = nil
	if action.Action == "finish" {
		state.Completed = true
		state.Summary = action.Summary
	}
	if err := e.Store.CheckpointAgent(claim, state); err != nil {
		return nil, err
	}
	if state.Completed {
		out, _ := json.Marshal(map[string]any{"completed": true, "turns": state.Turns, "changed_paths": sortedPaths(state.Changed)})
		return out, nil
	}
	return nil, ErrAgentYield
}

func (e *Engine) prepareAgentAction(ctx context.Context, w Workflow, action AgentAction) (*AgentPending, error) {
	pending := &AgentPending{Action: action}
	if action.Action != "patch" && action.Action != "create" {
		return pending, nil
	}
	if action.Action == "create" {
		pending.Contents = action.Content
		pending.AfterSHA256 = shaHex(action.Content)
		return pending, nil
	}
	// Read the exact worker contents, not a potentially stale model observation.
	checkout, err := resultObject(w, "checkout")
	if err != nil {
		return nil, err
	}
	result, err := e.workspaceAction(ctx, w, "read_file", map[string]any{"path": path.Join(repoRoot(str(checkout["repository"])), action.Path)})
	if err != nil {
		return nil, err
	}
	before := str(result["content"])
	if shaHex(before) != action.ExpectedSHA256 {
		return nil, errors.New("patch source digest conflict; read the file again")
	}
	after, err := applyPatch(before, action.Hunks)
	if err != nil {
		return nil, err
	}
	pending.BeforeSHA256 = shaHex(before)
	pending.AfterSHA256 = shaHex(after)
	pending.Contents = after
	return pending, nil
}

func (e *Engine) executeAgentAction(ctx context.Context, w Workflow, brief AgentBrief, state *AgentSession) (string, error) {
	pending := state.Pending
	if pending == nil {
		return "", errors.New("agent has no pending action")
	}
	action := pending.Action
	scope := brief
	if action.Repository != "" { scope.Repository = action.Repository }
	root := repoRoot(scope.Repository)
	switch action.Action {
	case "plan":
		state.Plan = append([]string(nil), action.Plan...)
		return "Plan recorded", nil
	case "list":
		target := root
		if action.Path != "" {
			target = path.Join(root, action.Path)
		}
		result, err := e.workspaceAction(ctx, w, "list_directory", map[string]any{"path": target})
		if err != nil {
			return "", err
		}
		return boundObservation(result, 8000), nil
	case "search":
		// Quote the model-supplied term and scope grep to the checked-out repository.
		command := "cd " + shellQuote(root) + " && LC_ALL=C grep -r -n -F -m 10 --exclude-dir=.git --exclude-dir=.ssh --exclude-dir=.aws --exclude-dir=.gnupg --exclude-dir=node_modules --exclude-dir=vendor --exclude=.env --exclude=.env.* --exclude=*.pem --exclude=*.key --exclude=*.p12 --exclude=.netrc --exclude=.npmrc --exclude=.pypirc --exclude=id_rsa --exclude=id_ed25519 --exclude=credentials.json --exclude=secrets.json -- " + shellQuote(action.Query) + " . | head -n 80"
		result, err := e.workspaceAction(ctx, w, "command", map[string]any{"command": command})
		if err != nil {
			return "", err
		}
		if code, ok := result["exit_code"].(float64); !ok || code > 1 {
			return "", errors.New("repository search failed")
		}
		return boundObservation(result, 8000), nil
	case "map":
		return e.repositoryMap(ctx, w, scope, action.Path)
	case "symbols":
		return e.repositorySymbols(ctx, w, scope, action.Path)
	case "references":
		return e.repositoryReferences(ctx, w, scope, action.Query, action.Path)
	case "read":
		result, err := e.workspaceAction(ctx, w, "read_file", map[string]any{"path": path.Join(root, action.Path)})
		if err != nil {
			return "", err
		}
		contents := str(result["content"])
		if len(contents) > 16000 {
			return "", errors.New("repository file exceeds agent read limit")
		}
		return fmt.Sprintf("sha256=%s\n%s", shaHex(contents), contents), nil
	case "patch", "create":
		if err := e.executeAgentWrite(ctx, w, brief, state); err != nil {
			return "", err
		}
		return "File contents confirmed at the target digest", nil
	case "check":
		// A pending check must not be replayed after a crash: an approved command
		// can have effects that are not reversible or externally observable.
		result, err := e.workspaceAction(ctx, w, "command", map[string]any{"command": "cd " + shellQuote(root) + " && " + action.Command})
		if err != nil {
			return "", err
		}
		code, ok := result["exit_code"].(float64)
		if !ok {
			return "", errors.New("check has no confirmed exit code")
		}
		state.ChecksPassed[action.Command] = code == 0
		return fmt.Sprintf("check=%q exit_code=%.0f\n%s", action.Command, code, boundObservation(result, 5000)), nil
	case "finish":
		if len(state.Changed) == 0 {
			return "", errors.New("agent cannot finish without source changes")
		}
		for _, check := range brief.Checks {
			if !state.ChecksPassed[check] {
				return "", errors.New("agent cannot finish until all approved checks pass after its last edit")
			}
		}
		if len(action.Summary) == 0 {
			return "", errors.New("completion summary required")
		}
		return "Checks passed; changes ready for final verification", nil
	}
	return "", errors.New("unsupported agent action")
}

func (e *Engine) executeAgentWrite(ctx context.Context, w Workflow, brief AgentBrief, state *AgentSession) error {
	pending := state.Pending
	action := pending.Action
	root := repoRoot(brief.Repository)
	filename := path.Join(root, action.Path)
	if pending.AfterSHA256 == "" || len(pending.Contents) == 0 {
		return errors.New("incomplete pending edit")
	}
	// A previously issued write can be reconciled from the sandbox contents.
	var oldSHA string
	current, err := e.workspaceAction(ctx, w, "read_file", map[string]any{"path": filename})
	if err == nil {
		oldSHA = shaHex(str(current["content"]))
	} else if action.Action != "create" {
		return err
	}
	if oldSHA != pending.AfterSHA256 {
		if oldSHA != pending.BeforeSHA256 {
			return errors.New("source changed during autonomous edit")
		}
		result, err := e.workspaceAction(ctx, w, "write_file", map[string]any{
			"path": filename, "content": pending.Contents, "expected_sha256": oldSHAForCAS(action, oldSHA),
		})
		if err != nil {
			return err
		}
		if str(result["sha256"]) != pending.AfterSHA256 {
			return errors.New("worker did not confirm target source digest")
		}
		confirm, err := e.workspaceAction(ctx, w, "read_file", map[string]any{"path": filename})
		if err != nil || shaHex(str(confirm["content"])) != pending.AfterSHA256 {
			return errors.New("source readback verification failed")
		}
	}
	state.Changed[action.Path] = pending.AfterSHA256
	state.ChecksPassed = map[string]bool{}
	return nil
}

func oldSHAForCAS(action AgentAction, hash string) string {
	if action.Action == "create" {
		return ""
	}
	return hash
}

func boundObservation(result map[string]any, limit int) string {
	stdout := str(result["stdout"])
	stderr := str(result["stderr"])
	if stdout == "" && stderr == "" {
		raw, _ := json.Marshal(result)
		stdout = string(raw)
	}
	if len(stdout) > limit {
		stdout = stdout[:limit]
	}
	if stderr != "" {
		if len(stderr) > 512 {
			stderr = stderr[:512]
		}
		stdout += "\nstderr: " + stderr
	}
	return stdout
}
