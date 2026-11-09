package agentflow

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"path"
	"regexp"
	"sort"
	"strings"
)

var repositoryName = regexp.MustCompile(`^[A-Za-z0-9_.-]{1,100}/[A-Za-z0-9_.-]{1,100}$`)
var commitName = regexp.MustCompile(`^[0-9a-f]{40}$`)
var branchName = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._/-]{0,150}$`)

type RepositoryPin struct {
	Repository string `json:"repository"`
	Revision string `json:"revision"`
}

type Program struct {
	ID              string            `json:"id,omitempty"`
	Project         string            `json:"project"`
	Repository      string            `json:"repository"`
	Revision        string            `json:"revision"`
	BaseBranch      string            `json:"base_branch"`
	ExpectedHead    string            `json:"expected_head"`
	Objective       string            `json:"objective"`
	ContextPaths    []string          `json:"context_paths,omitempty"`
	AllowedPaths    []string          `json:"allowed_paths,omitempty"`
	Changes         map[string]string `json:"changes,omitempty"`
	Autonomous      bool              `json:"autonomous,omitempty"`
	LinkedRepositories []RepositoryPin `json:"linked_repositories,omitempty"`
	PrepareOnly bool `json:"prepare_only,omitempty"`
	IncludeKnowledge bool `json:"include_knowledge,omitempty"`
	Review bool `json:"review,omitempty"`
	Checks          []string          `json:"checks"`
	ImagePoolID     string            `json:"image_pool_id"`
	CPU             float64           `json:"cpu"`
	MemoryMB        int               `json:"memory_mb"`
	DurationMinutes int               `json:"duration_minutes"`
	Title           string            `json:"title"`
	Description     string            `json:"description,omitempty"`
}

func validSourcePath(p string) bool {
	if len(p) == 0 || len(p) > 255 || path.IsAbs(p) || strings.ContainsAny(p, "\\\x00\r\n") || path.Clean(p) != p {
		return false
	}
	for _, segment := range strings.Split(p, "/") {
		if segment == "" || segment == "." || segment == ".." || segment == ".git" {
			return false
		}
	}
	return true
}
func (p Program) validate() error {
	if !validID(p.Project) || !repositoryName.MatchString(p.Repository) || !commitName.MatchString(p.Revision) ||
		!commitName.MatchString(p.ExpectedHead) || p.Revision != p.ExpectedHead || !branchName.MatchString(p.BaseBranch) || strings.Contains(p.BaseBranch, "..") ||
		strings.Contains(p.BaseBranch, "//") || strings.HasSuffix(p.BaseBranch, ".lock") ||
		strings.TrimSpace(p.Objective) == "" || len(p.Objective) > 4096 ||
		!validID(p.ImagePoolID) || p.CPU < 0.1 || p.CPU > 8 || p.MemoryMB < 256 || p.MemoryMB > 16384 ||
		p.DurationMinutes < 10 || p.DurationMinutes > 180 ||
		len(p.Checks) == 0 || len(p.Checks) > 8 || len(p.Changes) > 16 ||
		len(p.ContextPaths) > 16 || len(p.AllowedPaths) > 16 || len(p.Title) == 0 || len(p.Title) > 256 || len(p.Description) > 8192 {
		return errors.New("invalid engineering program or resource limits")
	}
	if len(p.LinkedRepositories) > 8 || (len(p.LinkedRepositories) > 0 && !p.Autonomous) {
		return errors.New("linked repositories require a bounded autonomous run")
	}
	seenRepositories := map[string]bool{p.Repository: true}
	for _, linked := range p.LinkedRepositories {
		if !repositoryName.MatchString(linked.Repository) || !commitName.MatchString(linked.Revision) || seenRepositories[linked.Repository] {
			return errors.New("linked repository must have a unique name and pinned commit")
		}
		seenRepositories[linked.Repository] = true
	}
	for _, s := range p.Checks {
		if strings.TrimSpace(s) == "" || len(s) > 512 || strings.ContainsAny(s, "\x00\r\n") {
			return errors.New("invalid verification command")
		}
	}
	for _, s := range p.ContextPaths {
		if !validSourcePath(s) {
			return errors.New("invalid source context path")
		}
	}
	for _, s := range p.AllowedPaths {
		if !validSourcePath(s) || (p.Autonomous && sensitiveSourcePath(s)) {
			return errors.New("invalid editable path")
		}
	}
	if len(p.Changes) == 0 && len(p.AllowedPaths) == 0 {
		return errors.New("automatic authoring requires a finite editable file list")
	}
	if p.Autonomous && (len(p.AllowedPaths) == 0 || len(p.Changes) != 0) {
		return errors.New("autonomous engineering requires allowed paths and no supplied changes")
	}
	total := 0
	for path, body := range p.Changes {
		if !validSourcePath(path) || len(body) == 0 || len(body) > 24000 {
			return errors.New("invalid source change")
		}
		total += len(path) + len(body)
	}
	if total > 56<<10 {
		return errors.New("source change set exceeds the durable result limit")
	}
	return nil
}
func encodeStep(v any) json.RawMessage { raw, _ := json.Marshal(v); return raw }
func generateProgramID() (string, error) {
	var b [12]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	return "run-" + hex.EncodeToString(b[:]), nil
}
func Compile(p Program) (Workflow, error) {
	var w Workflow
	if err := p.validate(); err != nil {
		return w, err
	}
	if p.ID == "" {
		id, err := generateProgramID()
		if err != nil {
			return w, err
		}
		p.ID = id
	}
	if !validID(p.ID) {
		return w, errors.New("invalid run ID")
	}
	w = Workflow{ID: p.ID, Project: p.Project}
	add := func(id, kind string, depends []string, input any) {
		w.Steps = append(w.Steps, Step{ID: id, Kind: kind, DependsOn: depends, Input: encodeStep(input), MaxAttempts: 1})
	}
	add("provision", "provision", nil, map[string]any{"image_pool_id": p.ImagePoolID, "cpu": p.CPU, "memory_mb": p.MemoryMB, "duration_minutes": p.DurationMinutes})
	add("checkout", "checkout", []string{"provision"}, map[string]any{"repository": p.Repository, "ref": p.Revision})
	if p.Autonomous {
		checkoutDependencies := []string{"checkout"}
		for i, linked := range p.LinkedRepositories {
			id := fmt.Sprintf("checkout-link-%d", i+1)
			add(id, "checkout", []string{"checkout"}, map[string]any{"repository": linked.Repository, "ref": linked.Revision})
			checkoutDependencies = append(checkoutDependencies, id)
		}
		add("agent", "agent", checkoutDependencies, AgentBrief{
			Objective: p.Objective, Repository: p.Repository, AllowedPaths: p.AllowedPaths,
			Checks: p.Checks, ContextPaths: p.ContextPaths, LinkedRepositories: p.LinkedRepositories, IncludeKnowledge: p.IncludeKnowledge, Revision: p.Revision,
		})
		w.Steps[len(w.Steps)-1].MaxAttempts = 5
		add("verify", "verify", []string{"agent"}, map[string]any{"commands": p.Checks})
		next := "verify"
		if p.Review {
			add("review", "review", []string{"verify"}, map[string]any{"objective":p.Objective})
			next = "review"
		}
		add("export", "export", []string{next}, map[string]any{})
		if !p.PrepareOnly {
			add("publish", "publish", []string{"export"}, map[string]any{"repository": p.Repository, "base_branch": p.BaseBranch, "expected_head": p.ExpectedHead, "title": p.Title, "body": p.Description})
		}
		if err := Validate(w); err != nil {
			return Workflow{}, fmt.Errorf("invalid compiled workflow: %w", err)
		}
		return w, nil
	}
	add("inspect", "inspect", []string{"checkout"}, map[string]any{"repository": p.Repository, "paths": p.ContextPaths})
	add("author", "author", []string{"inspect"}, map[string]any{"objective": p.Objective, "changes": p.Changes, "allowed_paths": p.AllowedPaths})
	add("apply", "apply", []string{"author"}, map[string]any{})
	add("verify", "verify", []string{"apply"}, map[string]any{"commands": p.Checks})
	next := "verify"
		if p.Review {
			add("review", "review", []string{"verify"}, map[string]any{"objective":p.Objective})
			next = "review"
		}
		add("export", "export", []string{next}, map[string]any{})
	if !p.PrepareOnly {
		add("publish", "publish", []string{"export"}, map[string]any{"repository": p.Repository, "base_branch": p.BaseBranch, "expected_head": p.ExpectedHead, "title": p.Title, "body": p.Description})
	}
	if err := Validate(w); err != nil {
		return Workflow{}, fmt.Errorf("invalid compiled workflow: %w", err)
	}
	return w, nil
}

type StepSummary struct {
	ID       string          `json:"id"`
	Kind     string          `json:"kind"`
	Status   Status          `json:"status"`
	Attempts int             `json:"attempts"`
	Error    string          `json:"error,omitempty"`
	Result   json.RawMessage `json:"result,omitempty"`
}
type RunSummary struct {
	ID        string         `json:"id"`
	Project   string         `json:"project"`
	Revision  uint64         `json:"revision"`
	Cancelled bool           `json:"cancelled"`
	Status    Status         `json:"status"`
	Steps     []StepSummary  `json:"steps"`
	Agent     *AgentProgress `json:"agent,omitempty"`
}

func Summarize(w Workflow) RunSummary {
	out := RunSummary{ID: w.ID, Project: w.Project, Revision: w.Revision, Cancelled: w.Cancelled, Status: Pending}
	out.Agent = publicAgentProgress(w.Agent)
	allComplete := true
	for _, step := range w.Steps {
		var visible json.RawMessage
		if step.Kind == "provision" || step.Kind == "checkout" || step.Kind == "verify" || step.Kind == "review" || step.Kind == "export" || step.Kind == "publish" || step.Kind == "integration_verify" || step.Kind == "integration_publish" || step.Kind == "integration_cleanup" {
			visible = append(json.RawMessage(nil), step.Result...)
		}
		out.Steps = append(out.Steps, StepSummary{ID: step.ID, Kind: step.Kind, Status: step.Status, Attempts: step.Attempts, Error: step.LastError, Result: visible})
		if step.Status == Running {
			out.Status = Running
		}
		if step.Status == Failed {
			out.Status = Failed
		}
		if step.Status != Succeeded {
			allComplete = false
		}
	}
	if allComplete {
		out.Status = Succeeded
	}
	if w.Cancelled {
		out.Status = Cancelled
	}
	return out
}
func (s *Store) Summaries() ([]RunSummary, error) {
	ids, err := s.List()
	if err != nil {
		return nil, err
	}
	if len(ids) > 1000 {
		return nil, errors.New("too many engineering runs")
	}
	runs := make([]RunSummary, 0, len(ids))
	for _, id := range ids {
		w, err := s.Get(id)
		if err != nil {
			return nil, err
		}
		runs = append(runs, Summarize(w))
	}
	sort.Slice(runs, func(i, j int) bool { return runs[i].ID < runs[j].ID })
	return runs, nil
}
