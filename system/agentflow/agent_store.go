package agentflow

import (
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"time"
)

func (s *Store) activeClaim(w Workflow, c Claim) (int, error) {
	if w.Cancelled || !validID(c.WorkflowID) || !validID(c.StepID) || len(c.Token) != 64 || c.Generation == 0 {
		return -1, errors.New("claim is not authorized")
	}
	for i := range w.Steps {
		step := w.Steps[i]
		if step.ID != c.StepID {
			continue
		}
		if step.Status != Running || step.Generation != c.Generation || !s.now().Before(step.LeaseUntil) {
			return -1, errors.New("stale agent claim")
		}
		digest := sha256.Sum256([]byte(c.Token))
		token, err := hex.DecodeString(step.LeaseDigest)
		if err != nil || subtle.ConstantTimeCompare(token, digest[:]) != 1 {
			return -1, errors.New("claim token mismatch")
		}
		return i, nil
	}
	return -1, errors.New("unknown agent step")
}

// CheckpointAgent writes the exact committed transcript under the current
// lease token. Cancelled or superseded workers cannot persist a decision.
func (s *Store) CheckpointAgent(claim Claim, session AgentSession) error {
	if claim.Kind != "agent" || session.Turns < 0 || session.Turns > maxAgentTurns || len(session.Events) > maxAgentTurns || len(session.Changed) > 16 || len(session.Knowledge)>8 {
		return errors.New("invalid agent checkpoint")
	}
	for path, digest := range session.Changed {
		if !validSourcePath(path) || !hexDigest(digest) {
			return errors.New("invalid changed-source identity")
		}
	}
	data, err := json.Marshal(session)
	if err != nil || len(data) > 512<<10 {
		return errors.New("agent transcript exceeds persistence budget")
	}
	unlock, err := s.lock()
	if err != nil {
		return err
	}
	defer unlock()
	w, err := s.read(claim.WorkflowID)
	if err != nil {
		return err
	}
	if _, err := s.activeClaim(w, claim); err != nil {
		return err
	}
	var snapshot AgentSession
	if err := json.Unmarshal(data, &snapshot); err != nil {
		return err
	}
	w.Agent = &snapshot
	w.Revision++
	return s.save(w)
}

// Yield atomically frees a completed agent turn, preserving its durable
// transcript without treating a deliberate checkpoint as a failed attempt.
func (s *Store) Yield(claim Claim) error {
	if claim.Kind != "agent" {
		return errors.New("only agent steps may yield")
	}
	unlock, err := s.lock()
	if err != nil {
		return err
	}
	defer unlock()
	w, err := s.read(claim.WorkflowID)
	if err != nil {
		return err
	}
	index, err := s.activeClaim(w, claim)
	if err != nil {
		return err
	}
	if w.Agent == nil || w.Agent.Pending != nil || w.Agent.Completed {
		return errors.New("agent checkpoint is incomplete")
	}
	step := &w.Steps[index]
	step.Status = Pending
	step.Attempts = 0
	step.LeaseDigest = ""
	step.LeaseUntil = time.Time{}
	w.Revision++
	return s.save(w)
}

// FailPermanent terminates a step after a decision that must not be retried.
// It preserves the failure reason and uses the same lease fencing as a checkpoint.
func (s *Store) FailPermanent(claim Claim, reason string) error {
	if len(reason) > 1024 {
		reason = reason[:1024]
	}
	unlock, err := s.lock()
	if err != nil {
		return err
	}
	defer unlock()
	w, err := s.read(claim.WorkflowID)
	if err != nil {
		return err
	}
	index, err := s.activeClaim(w, claim)
	if err != nil {
		return err
	}
	step := &w.Steps[index]
	step.Status = Failed
	step.LastError = reason
	step.LeaseUntil = time.Time{}
	step.LeaseDigest = ""
	w.Revision++
	return s.save(w)
}
