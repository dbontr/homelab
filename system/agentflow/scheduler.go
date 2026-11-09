package agentflow

import (
	"context"
	"errors"
	"time"
)

// Independent engineering runs can advance concurrently because each run
// owns an isolated workspace. Steps within a run remain dependency-ordered.
const maxConcurrentEngineeringRuns = 4

type engineeringStepResult struct {
	advanced bool
	err      error
}

// RunAvailable advances at most one eligible step per workflow. A bounded
// worker pool prevents a slow compute operation from blocking unrelated runs.
func (e *Engine) RunAvailable(ctx context.Context) (int, error) {
	if e == nil || e.Store == nil || e.Compute == nil {
		return 0, errors.New("engineering service not configured")
	}
	runs, err := e.List()
	if err != nil {
		return 0, err
	}
	slots := make(chan struct{}, maxConcurrentEngineeringRuns)
	results := make(chan engineeringStepResult, len(runs))
	launched := 0
dispatch:
	for _, run := range runs {
		if run.Status == Succeeded || run.Status == Failed || run.Status == Cancelled {
			continue
		}
		if err := ctx.Err(); err != nil {
			break
		}
		select {
		case slots <- struct{}{}:
		case <-ctx.Done():
			break dispatch
		}

		e.activeMu.Lock()
		if e.active == nil {
			e.active = make(map[string]context.CancelFunc)
		}
		if _, alreadyRunning := e.active[run.ID]; alreadyRunning {
			e.activeMu.Unlock()
			<-slots
			continue
		}
		runCtx, cancel := context.WithCancel(ctx)
		e.active[run.ID] = cancel
		e.activeMu.Unlock()

		launched++
		go func(runID string) {
			runner := Runner{Store: e.Store, Worker: e, Lease: 30 * time.Minute}
			advanced, err := runner.RunOne(runCtx, runID)
			e.activeMu.Lock()
			delete(e.active, runID)
			e.activeMu.Unlock()
			cancel()
			<-slots
			results <- engineeringStepResult{advanced: advanced, err: err}
		}(run.ID)
	}
	processed := 0
	var firstErr error
	for i := 0; i < launched; i++ {
		result := <-results
		if result.advanced {
			processed++
		}
		if result.err != nil && firstErr == nil {
			firstErr = result.err
		}
	}
	if firstErr != nil {
		return processed, firstErr
	}
	return processed, ctx.Err()
}

// Serve periodically advances eligible runs. Failure or cancellation of one
// run does not stop independent workflows.
func (e *Engine) Serve(ctx context.Context) {
	ticker := time.NewTicker(3 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
		_, _ = e.RunAvailable(ctx)
	}
}
