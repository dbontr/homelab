package agentflow

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"path"
	"sort"
	"strings"
	"time"
)

// KnowledgeProposal holds a source-backed finding until an owner approves it.
// The model cannot publish to the canonical project knowledge store.
type KnowledgeProposal struct {
	ID             string            `json:"id"`
	Kind           string            `json:"kind"`
	Title          string            `json:"title"`
	Body           string            `json:"body"`
	Repository     string            `json:"repository"`
	Revision       string            `json:"revision"`
	EvidenceSHA256 map[string]string `json:"evidence_sha256"`
	Status         string            `json:"status"`
	ContributionID string            `json:"contribution_id,omitempty"`
}
type KnowledgeManager interface {
 Knowledge(string,string)(KnowledgeProposal,error)
 ApproveKnowledge(string,string)(KnowledgeProposal,error)
}

type KnowledgePreview struct {
	ID     string `json:"id"`
	Kind   string `json:"kind"`
	Title  string `json:"title"`
	Status string `json:"status"`
}

func validKnowledgeProposal(action AgentAction) bool {
	switch action.KnowledgeKind {
	case "decision", "finding", "note":
	default:
		return false
	}
	if strings.TrimSpace(action.KnowledgeTitle) == "" || len(action.KnowledgeTitle) > 160 ||
		strings.TrimSpace(action.KnowledgeBody) == "" || len(action.KnowledgeBody) > 1200 ||
		len(action.EvidencePaths) == 0 || len(action.EvidencePaths) > 3 {
		return false
	}
	seen := map[string]bool{}
	for _, filename := range action.EvidencePaths {
		if !validSourcePath(filename) || sensitiveSourcePath(filename) || seen[filename] {
			return false
		}
		seen[filename] = true
	}
	return true
}

func (e *Engine) proposeKnowledge(ctx context.Context, w Workflow, brief AgentBrief, state *AgentSession, action AgentAction) (string, error) {
	if !validKnowledgeProposal(action) || !brief.IncludeKnowledge ||
		!repositoryName.MatchString(brief.Repository) || !commitName.MatchString(brief.Revision) {
		return "", errors.New("invalid or unauthorized knowledge proposal")
	}
	if len(state.Knowledge) >= 8 {
		return "", errors.New("knowledge proposal budget exhausted")
	}
	evidence := map[string]string{}
	for _, filename := range action.EvidencePaths {
		result, err := e.workspaceAction(ctx, w, "read_file", map[string]any{
			"path": path.Join(repoRoot(brief.Repository), filename),
		})
		if err != nil {
			return "", err
		}
		content := str(result["content"])
		if content == "" || len(content) > 24000 {
			return "", errors.New("knowledge source unavailable or exceeds evidence limit")
		}
		digest := sha256.Sum256([]byte(content))
		evidence[filename] = hex.EncodeToString(digest[:])
	}
	ordered := make([]string, 0, len(evidence))
	for filename := range evidence {
		ordered = append(ordered, filename)
	}
	sort.Strings(ordered)
	identity := brief.Repository + "\x00" + brief.Revision + "\x00" +
		action.KnowledgeKind + "\x00" + strings.TrimSpace(action.KnowledgeTitle) + "\x00" +
		strings.TrimSpace(action.KnowledgeBody)
	for _, filename := range ordered {
		identity += "\x00" + filename + "\x00" + evidence[filename]
	}
	sum := sha256.Sum256([]byte(identity))
	id := hex.EncodeToString(sum[:])
	for _, existing := range state.Knowledge {
		if existing.ID == id {
			return fmt.Sprintf("Knowledge proposal %s already recorded", id), nil
		}
	}
	state.Knowledge = append(state.Knowledge, KnowledgeProposal{
		ID: id, Kind: action.KnowledgeKind, Title: strings.TrimSpace(action.KnowledgeTitle),
		Body: strings.TrimSpace(action.KnowledgeBody), Repository: brief.Repository,
		Revision: brief.Revision, EvidenceSHA256: evidence, Status: "proposed",
	})
	return fmt.Sprintf("Knowledge proposal %s retained for owner review", id), nil
}

func (e *Engine) Knowledge(runID, proposalID string) (KnowledgeProposal, error) {
	if e == nil || e.Store == nil {
		return KnowledgeProposal{}, errors.New("engineering service unavailable")
	}
	w, err := e.Store.Get(runID)
	if err != nil {
		return KnowledgeProposal{}, err
	}
	if !e.AllowedProjects[w.Project] || w.Agent == nil {
		return KnowledgeProposal{}, errors.New("knowledge proposal unavailable")
	}
	for _, proposal := range w.Agent.Knowledge {
		if proposal.ID == proposalID {
			return proposal, nil
		}
	}
	return KnowledgeProposal{}, errors.New("unknown knowledge proposal")
}

func (e *Engine) ApproveKnowledge(runID, proposalID string) (KnowledgeProposal, error) {
	if e == nil || e.Store == nil || e.Compute == nil || !validID(proposalID) {
		return KnowledgeProposal{}, errors.New("engineering service unavailable")
	}
	w, err := e.Store.Get(runID)
	if err != nil {
		return KnowledgeProposal{}, err
	}
	if !e.AllowedProjects[w.Project] || Summarize(w).Status != Succeeded {
		return KnowledgeProposal{}, errors.New("only completed authorized runs can contribute knowledge")
	}
	proposal, err := e.Knowledge(runID, proposalID)
	if err != nil { return KnowledgeProposal{}, err }
	for filename, evidenceHash := range proposal.EvidenceSHA256 {
		if finalHash, edited := w.Agent.Changed[filename]; edited && finalHash != evidenceHash {
			return KnowledgeProposal{}, errors.New("knowledge proposal cites a superseded source revision")
		}
	}
	proposal, err = e.Store.beginKnowledgeContribution(runID, proposalID)
	if err != nil {
		return KnowledgeProposal{}, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	result, err := e.Compute.Call(ctx, "knowledge_contribute", map[string]any{
		"project_id": w.Project, "kind": proposal.Kind, "title": proposal.Title,
		"body": proposal.Body, "repository": proposal.Repository, "revision": proposal.Revision,
	})
	if err != nil {
		return KnowledgeProposal{}, fmt.Errorf("knowledge publication requires reconciliation: %w", err)
	}
	id := str(result["id"])
	if !validID(id) || str(result["project"]) != w.Project ||
		str(result["repository"]) != proposal.Repository || str(result["revision"]) != proposal.Revision ||
		str(result["body_sha256"]) != shaHex(proposal.Body) {
		return KnowledgeProposal{}, errors.New("knowledge contribution requires reconciliation")
	}
	if err := e.Store.confirmKnowledgeContribution(runID, proposalID, id); err != nil {
		return KnowledgeProposal{}, err
	}
	return e.Knowledge(runID, proposalID)
}

func (s *Store) beginKnowledgeContribution(runID, proposalID string) (KnowledgeProposal, error) {
	unlock, err := s.lock()
	if err != nil {
		return KnowledgeProposal{}, err
	}
	defer unlock()
	w, err := s.read(runID)
	if err != nil || w.Agent == nil || Summarize(w).Status != Succeeded {
		return KnowledgeProposal{}, errors.New("completed knowledge source unavailable")
	}
	for i := range w.Agent.Knowledge {
		p := &w.Agent.Knowledge[i]
		if p.ID != proposalID {
			continue
		}
		if p.Status != "proposed" {
			return KnowledgeProposal{}, errors.New("knowledge contribution has already been attempted")
		}
		p.Status = "submitting"
		w.Revision++
		if err := s.save(w); err != nil {
			return KnowledgeProposal{}, err
		}
		return *p, nil
	}
	return KnowledgeProposal{}, errors.New("knowledge proposal not found")
}

func (s *Store) confirmKnowledgeContribution(runID, proposalID, contributionID string) error {
	unlock, err := s.lock()
	if err != nil {
		return err
	}
	defer unlock()
	w, err := s.read(runID)
	if err != nil || w.Agent == nil {
		return errors.New("knowledge run unavailable")
	}
	for i := range w.Agent.Knowledge {
		p := &w.Agent.Knowledge[i]
		if p.ID != proposalID || p.Status != "submitting" {
			continue
		}
		p.Status = "published"
		p.ContributionID = contributionID
		w.Revision++
		return s.save(w)
	}
	return errors.New("knowledge publication confirmation is stale")
}
