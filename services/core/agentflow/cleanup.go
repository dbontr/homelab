package agentflow

import (
	"context"
	"errors"
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
	if workflow.Cleanup.Status == "stopping" && !workflow.CleanupStarted.IsZero() &&
		s.now().Sub(workflow.CleanupStarted) < 60*time.Second {
		return claim, false, nil
	}
	workspace, err := workspaceID(workflow)
	if err != nil {
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

// StopWorkspace checks controller state before sending a destructive request.
// A resumed cleanup does not repeat a stop that is already in progress.
func StopWorkspace(ctx context.Context, compute ToolCaller, project, id string) error {
	requested := false
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()
	for {
		response, err := compute.Call(ctx, "workspace_list", map[string]any{"project_id": project})
		if err != nil {
			return err
		}
		records, ok := response["result"].([]any)
		if !ok {
			return errors.New("invalid workspace listing")
		}
		status := ""
		for _, raw := range records {
			entry, ok := raw.(map[string]any)
			if !ok {
				return errors.New("invalid workspace status entry")
			}
			if entry["id"] != id {
				continue
			}
			value, ok := entry["status"].(string)
			if !ok || value == "" {
				return errors.New("invalid workspace status")
			}
			status = value
			break
		}
		switch status {
		case "", "stopped", "destroyed":
			return nil
		case "ready", "starting":
			if !requested {
				if _, err := compute.Call(ctx, "workspace_destroy", map[string]any{
					"project_id": project, "workspace_id": id,
				}); err != nil {
					return err
				}
				requested = true
			}
		case "stopping":
			// The controller has already accepted a teardown request.
		default:
			return errors.New("unexpected workspace state during cleanup")
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
	}
}

func (e *Engine) cleanupWorkspace(ctx context.Context, claim CleanupClaim) error {
	return StopWorkspace(ctx, e.Compute, claim.Project, claim.WorkspaceID)
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
