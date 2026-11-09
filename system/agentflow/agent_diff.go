package agentflow

import (
	"errors"
	"strings"
)

type SourceDiff struct {
	Path string `json:"path"`
	Diff string `json:"diff"`
}

// SourceDiffs returns only explicitly prepared patch previews from private
// workflow history. A preview is not evidence of a committed final change;
// the reviewable GitHub draft remains the authoritative publication diff.
func (e *Engine) SourceDiffs(runID string) ([]SourceDiff, error) {
	if e == nil || e.Store == nil {
		return nil, errors.New("engineering service unavailable")
	}
	workflow, err := e.Store.Get(runID)
	if err != nil {
		return nil, err
	}
	if !e.AllowedProjects[workflow.Project] || workflow.Agent == nil {
		return nil, errors.New("engineering source previews unavailable")
	}
	entries := make([]SourceDiff, 0, 8)
	for _, event := range workflow.Agent.Events {
		if event.Action != "preview" || !validSourcePath(event.Target) ||
			sensitiveSourcePath(event.Target) || !strings.HasPrefix(event.Observation, "@@ ") {
			continue
		}
		if len(event.Observation) > 8000 {
			continue
		}
		entries = append(entries, SourceDiff{Path: event.Target, Diff: event.Observation})
		if len(entries) > 8 {
			entries = entries[len(entries)-8:]
		}
	}
	return entries, nil
}

type DiffManager interface {
	SourceDiffs(string) ([]SourceDiff, error)
}
