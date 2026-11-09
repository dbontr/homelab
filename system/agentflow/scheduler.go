package agentflow

import (
	"context"
	"errors"
 "sort"
	"time"
)

// Independent engineering runs can advance concurrently because each run
// owns an isolated workspace. Steps within a run remain dependency-ordered.
const maxConcurrentEngineeringRuns = 4
const maxConcurrentProjectRuns = 2

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
	admission, err := e.teamAdmission()
	if err != nil { return 0, err }
	runs = interleaveProjects(runs)
	perProject := make(map[string]int)
	slots := make(chan struct{}, maxConcurrentEngineeringRuns)
	results := make(chan engineeringStepResult, len(runs))
	launched := 0
dispatch:
	for _, run := range runs {
		if run.Status == Succeeded || run.Status == Failed || run.Status == Cancelled || !admission[run.ID] && isMemberRun(run.ID, admission) || perProject[run.Project] >= maxConcurrentProjectRuns {
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
		perProject[run.Project]++
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

func isMemberRun(id string, admitted map[string]bool) bool {
 _,member:=admitted[id]
 return member
}

// teamAdmission gates dependencies on confirmed completion, not task order.
func (e *Engine) teamAdmission() (map[string]bool, error) {
 admitted:=map[string]bool{}
 ids,err:=e.Store.teamIDs()
 if err!=nil{return nil,err}
 for _,id:=range ids {
  record,err:=e.Store.readTeam(id)
  if err!=nil{return nil,err}
  byKey:=map[string]string{}
  for _,m:=range record.Members {byKey[m.Key]=m.RunID}
  for _,m:=range record.Members {
   ready:=true
   for _,key:=range m.DependsOn {
    previous,ok:=byKey[key]
    if !ok { return nil,errors.New("team dependency target missing") }
    run,err:=e.Get(previous)
    if err!=nil{return nil,err}
    if run.Status!=Succeeded {ready=false;break}
   }
   admitted[m.RunID]=ready
  }
 }
 return admitted,nil
}

// interleaveProjects prevents a large project from monopolizing the scheduler.
func interleaveProjects(runs []RunSummary) []RunSummary {
 groups:=map[string][]RunSummary{}
 names:=[]string{}
 for _,run:=range runs{
  if _,ok:=groups[run.Project];!ok{names=append(names,run.Project)}
  groups[run.Project]=append(groups[run.Project],run)
 }
 sort.Strings(names)
 ordered:=make([]RunSummary,0,len(runs))
 for{
  progress:=false
  for _,name:=range names {
   items:=groups[name]
   if len(items)==0{continue}
   ordered=append(ordered,items[0])
   groups[name]=items[1:]
   progress=true
  }
  if !progress {break}
 }
 return ordered
}
