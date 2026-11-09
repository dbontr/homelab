package agentflow

import (
	"context"
	"errors"
	"os"
	"time"
)

type CleanupClaim struct {
	RunID       string
	Project     string
	WorkspaceID string
	Attempt     int
}

// startCleanup records a bounded cleanup lease for a terminal run.
// A workspace is never destroyed while its workflow can still execute steps.
func (s *Store) startCleanup(id string) (CleanupClaim, bool, error) {
	var claim CleanupClaim
	unlock, err := s.lock()
	if err != nil {
		return claim, false, err
	}
	defer unlock()
	workflow, err := s.read(id)
	if err != nil {
		return claim, false, err
	}
	switch Summarize(workflow).Status {
	case Succeeded, Failed, Cancelled:
	default:
		return claim, false, nil
	}
	if workflow.Cleanup.Status == "stopped" || workflow.Cleanup.Attempts >= 3 {
		return claim, false, nil
	}
	if workflow.Cleanup.Status == "stopping" &&
		s.now().Sub(workflow.CreatedAt) < 0 {
		return claim, false, errors.New("invalid cleanup clock")
	}
	if workflow.Cleanup.Status == "stopping" && !workflow.CleanupStarted.IsZero() &&
		s.now().Sub(workflow.CleanupStarted) < 60*time.Second {
		return claim, false, nil
	}
	workspace, err := workspaceID(workflow)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return claim, false, nil
		}
		// There is no worker identity to destroy when provisioning failed.
		return claim, false, nil
	}
	workflow.Cleanup.Attempts++
	workflow.Cleanup.Status = "stopping"
	workflow.Cleanup.LastError = ""
	workflow.CleanupStarted = s.now().UTC()
	workflow.Revision++
	if err := s.save(workflow); err != nil {
		return claim, false, err
	}
	return CleanupClaim{RunID: id, Project: workflow.Project, WorkspaceID: workspace, Attempt: workflow.Cleanup.Attempts}, true, nil
}

func (s *Store) confirmCleanup(claim CleanupClaim, state string, lastError string) error {
	unlock, err := s.lock()
	if err != nil {
		return err
	}
	defer unlock()
	workflow, err := s.read(claim.RunID)
	if err != nil {
		return err
	}
	if workflow.Cleanup.Attempts != claim.Attempt || workflow.Cleanup.Status != "stopping" {
		return errors.New("cleanup claim superseded")
	}
	workflow.Cleanup.Status = state
	if len(lastError) > 240 {
		lastError = lastError[:240]
	}
	workflow.Cleanup.LastError = lastError
	workflow.Revision++
	return s.save(workflow)
}

func (e *Engine) cleanupWorkspace(ctx context.Context, claim CleanupClaim) error {
	list, err := e.Compute.Call(ctx, "workspace_list", map[string]any{"project_id": claim.Project})
	if err != nil {
		return err
	}
	entries, ok := list["result"].([]any)
	if !ok {
		return errors.New("invalid workspace listing")
	}
	visible := false
	for _, item := range entries {
		record := obj(item)
		if str(record["id"]) != claim.WorkspaceID {
			continue
		}
		visible = true
		switch str(record["status"]) {
		case "stopped", "destroyed":
			return nil
		case "ready", "starting", "stopping":
		default:
			return errors.New("unexpected workspace state during cleanup")
		}
	}
	if !visible {
		return nil
	}
	if _, err := e.Compute.Call(ctx, "workspace_destroy", map[string]any{
		"project_id": claim.Project, "workspace_id": claim.WorkspaceID,
	}); err != nil {
		return err
	}
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()
	for {
		list, err := e.Compute.Call(ctx, "workspace_list", map[string]any{"project_id": claim.Project})
		if err != nil {
			return err
		}
		entries, ok := list["result"].([]any)
		if !ok {
			return errors.New("invalid workspace listing")
		}
		visible := false
		for _, item := range entries {
			record := obj(item)
			if str(record["id"]) != claim.WorkspaceID {
				continue
			}
			visible = true
			switch str(record["status"]) {
			case "stopped", "destroyed":
				return nil
			case "stopping", "ready", "starting":
			default:
				return errors.New("unexpected workspace state")
			}
		}
		if !visible {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
	}
}

// ReconcileWorkspaces attempts one terminal-run cleanup per pass. Pending
// cleanup survives service restarts and never modifies active workspaces.
func (e *Engine) ReconcileWorkspaces(ctx context.Context) error {
	if e == nil || e.Store == nil || e.Compute == nil {
		return errors.New("engineering service unavailable")
	}
	runs, err := e.List()
	if err != nil {
		return err
	}
	for _, run := range runs {
		switch run.Status {
		case Succeeded, Failed, Cancelled:
		default:
			continue
		}
		claim, ready, err := e.Store.startCleanup(run.ID)
		if err != nil {
			return err
		}
		if !ready {
			continue
		}
		cleanupCtx, cancel := context.WithTimeout(ctx, 45*time.Second)
		err = e.cleanupWorkspace(cleanupCtx, claim)
		cancel()
		if err != nil {
			if saveErr := e.Store.confirmCleanup(claim, "retry", "workspace cleanup not confirmed"); saveErr != nil {
				return saveErr
			}
			return err
		}
		return e.Store.confirmCleanup(claim, "stopped", "")
	}
	return nil
}
