package agentflow

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"sort"
	"time"
)

// AutomationAlert is an owner-private reference to an observed change.
// It contains no source excerpts, health data, model prompts, or result bodies.
type AutomationAlert struct {
	WorkflowID   string    `json:"workflow_id"`
	ObservedAt   time.Time `json:"observed_at"`
	Acknowledged bool      `json:"acknowledged"`
}

type AutomationAlertView struct {
	AutomationID string    `json:"automation_id"`
	Project      string    `json:"project"`
	WorkflowID   string    `json:"workflow_id"`
	ObservedAt   time.Time `json:"observed_at"`
	Acknowledged bool      `json:"acknowledged"`
	Revision     uint64    `json:"revision"`
}

// observedDigest excludes claim tokens, retry counts and timestamps. A stable,
// canonical JSON encoding makes semantically equal observations comparable.
func observedDigest(w Workflow) (string, bool, error) {
	if w.Cancelled || w.PlanError != "" || len(w.Steps) == 0 {
		return "", false, nil
	}
	type observedStep struct {
		ID     string          `json:"id"`
		Kind   string          `json:"kind"`
		Result json.RawMessage `json:"result"`
	}
	results := make([]observedStep, 0, len(w.Steps))
	for _, step := range w.Steps {
		if step.Status != Succeeded {
			return "", false, nil
		}
		results = append(results, observedStep{ID: step.ID, Kind: step.Kind, Result: step.Result})
	}
	data, err := json.Marshal(results)
	if err != nil {
		return "", false, err
	}
	digest := sha256.Sum256(data)
	return hex.EncodeToString(digest[:]), true, nil
}

// ReconcileAutomationObservations durably processes completed scheduled reads.
// Change-monitoring schedules establish a baseline and alert when successful
// results differ. Weekly-summary schedules alert for each successful run.
// Failed or cancelled runs cannot generate a success notification.
func (s *Store) ReconcileAutomationObservations(limit int) (int, error) {
	if limit < 1 || limit > 100 {
		return 0, errors.New("invalid automation observation limit")
	}
	unlock, err := s.lock()
	if err != nil {
		return 0, err
	}
	defer unlock()
	automations, err := s.listAutomationsLocked()
	if err != nil {
		return 0, err
	}
	processed := 0
	for _, a := range automations {
		if processed >= limit {
			break
		}
		if (!a.NotifyOnChange && !a.NotifyOnEveryRun) || a.LastWorkflowID == "" ||
			a.LastWorkflowID == a.LastObservedWorkflowID {
			continue
		}
		w, err := s.read(a.LastWorkflowID)
		if errors.Is(err, os.ErrNotExist) {
			w, err = s.readArchive(a.LastWorkflowID)
		}
		if err != nil {
			return processed, err
		}
		if !w.CapabilityPlan || w.AutomationID != a.ID ||
			!w.AutomationDue.Equal(a.LastAt) || w.Project != a.Project ||
			w.PersonalWrite || w.AutoReplan {
			return processed, errors.New("automation observation scope mismatch")
		}
		if !terminalCapabilityWorkflow(w, s.now().UTC()) {
			continue
		}
		digest, successful, err := observedDigest(w)
		if err != nil {
			return processed, err
		}
		if successful {
			if a.NotifyOnEveryRun || (a.ObservationDigest != "" && a.ObservationDigest != digest) {
				a.Alerts = append(a.Alerts, AutomationAlert{
					WorkflowID: w.ID,
					ObservedAt: s.now().UTC(),
				})
				if len(a.Alerts) > 32 {
					a.Alerts = a.Alerts[len(a.Alerts)-32:]
				}
			}
			a.ObservationDigest = digest
		}
		a.LastObservedWorkflowID = w.ID
		a.Revision++
		a.UpdatedAt = s.now().UTC()
		if err := s.saveAutomationLocked(a); err != nil {
			return processed, err
		}
		processed++
	}
	return processed, nil
}

// ListAutomationAlerts exposes bounded metadata rather than observation bodies.
func (s *Store) ListAutomationAlerts(limit int) ([]AutomationAlertView, error) {
	if limit < 1 || limit > 100 {
		return nil, errors.New("invalid alert limit")
	}
	unlock, err := s.lock()
	if err != nil {
		return nil, err
	}
	defer unlock()
	automations, err := s.listAutomationsLocked()
	if err != nil {
		return nil, err
	}
	alerts := make([]AutomationAlertView, 0)
	for _, a := range automations {
		for _, alert := range a.Alerts {
			alerts = append(alerts, AutomationAlertView{
				AutomationID: a.ID,
				Project: a.Project,
				WorkflowID: alert.WorkflowID,
				ObservedAt: alert.ObservedAt,
				Acknowledged: alert.Acknowledged,
				Revision: a.Revision,
			})
		}
	}
	sort.Slice(alerts, func(i, j int) bool {
		if !alerts[i].ObservedAt.Equal(alerts[j].ObservedAt) {
			return alerts[i].ObservedAt.After(alerts[j].ObservedAt)
		}
		if alerts[i].AutomationID != alerts[j].AutomationID {
			return alerts[i].AutomationID < alerts[j].AutomationID
		}
		return alerts[i].WorkflowID < alerts[j].WorkflowID
	})
	if len(alerts) > limit {
		alerts = alerts[:limit]
	}
	return alerts, nil
}

// AcknowledgeAutomationAlert requires the current automation revision.
// It cannot alter an underlying workflow or its recorded evidence.
func (s *Store) AcknowledgeAutomationAlert(automationID, workflowID string, revision uint64) (AutomationAlertView, error) {
	var result AutomationAlertView
	if !validID(automationID) || !validID(workflowID) || revision == 0 {
		return result, errors.New("invalid alert identity or revision")
	}
	unlock, err := s.lock()
	if err != nil {
		return result, err
	}
	defer unlock()
	a, err := s.readAutomationLocked(automationID)
	if err != nil {
		return result, err
	}
	if a.Revision != revision {
		return result, errors.New("automation revision conflict")
	}
	for i := range a.Alerts {
		if a.Alerts[i].WorkflowID != workflowID {
			continue
		}
		if !a.Alerts[i].Acknowledged {
			a.Alerts[i].Acknowledged = true
			a.Revision++
			a.UpdatedAt = s.now().UTC()
			if err := s.saveAutomationLocked(a); err != nil {
				return result, err
			}
		}
		return AutomationAlertView{
			AutomationID: a.ID,
			Project: a.Project,
			WorkflowID: workflowID,
			ObservedAt: a.Alerts[i].ObservedAt,
			Acknowledged: a.Alerts[i].Acknowledged,
			Revision: a.Revision,
		}, nil
	}
	return result, os.ErrNotExist
}
