package transport

import (
    "net/http"
    "strings"

    "github.com/dbontr/homelab-new/system/agentflow"
)

// Delegated work is an explicit relationship between a personal task and a
// confirmed engineering run. Neither absent logs nor a completed worker job
// automatically establishes completion of the personal task.
type TaskDelegation struct {
    TaskID string `json:"task_id"`
    TaskTitle string `json:"task_title"`
    TaskStatus string `json:"task_status"`
    RunID string `json:"run_id"`
    RunStatus string `json:"run_status"`
    Project string `json:"project,omitempty"`
    State string `json:"state"`
    Note string `json:"note,omitempty"`
}
type DelegationReport struct {
    Delegations []TaskDelegation `json:"delegations"`
    Limitations []string `json:"limitations"`
}
type runObservation struct {
    Run agentflow.RunSummary
    Available bool
}

func (s *Server) taskDelegations() DelegationReport {
    report:=DelegationReport{Delegations:[]TaskDelegation{},Limitations:[]string{}}
    if s.engineering==nil {
        report.Limitations=append(report.Limitations,"Engineering is not configured; delegated run outcomes are unknown")
    }
    cache:=map[string]runObservation{}
    for _,task:=range s.db.Tasks("") {
        projects:=map[string]bool{}
        for _,link:=range task.Links {
            kind,id,err:=parseResourceReference(link)
            if err==nil && kind=="project"{projects[id]=true}
        }
        observed:=map[string]bool{}
        for _,link:=range task.Links {
            kind,id,err:=parseResourceReference(link)
            if err!=nil||kind!="engineering_run"||observed[id]{continue}
            observed[id]=true
            entry:=TaskDelegation{
                TaskID:task.ID,TaskTitle:task.Title,TaskStatus:task.Status,
                RunID:id,State:"unknown",Note:"Run outcome has not been confirmed",
            }
            item,known:=cache[id]
            if !known && s.engineering!=nil {
                run,err:=s.engineering.Get(id)
                item=runObservation{Run:run,Available:err==nil&&run.ID==id}
                cache[id]=item
            }
            if item.Available {
                entry.RunStatus=string(item.Run.Status)
                entry.Project=item.Run.Project
                switch {
                case len(projects)>0 && !projects[entry.Project]:
                    entry.State="scope_conflict"
                    entry.Note="Task project links do not include the run's confirmed project"
                case task.Status=="completed" && item.Run.Status!=agentflow.Succeeded:
                    entry.State="inconsistent"
                    entry.Note="Personal task was completed without a successful linked engineering run"
                case task.Status=="completed":
                    entry.State="aligned"
                    entry.Note="Task and linked engineering run both record completion"
                case item.Run.Status==agentflow.Succeeded:
                    entry.State="ready_for_review"
                    entry.Note="Engineering completed; personal task remains open pending owner review"
                case item.Run.Status==agentflow.Failed:
                    entry.State="blocked"
                    entry.Note="Engineering reported a failed run"
                case item.Run.Status==agentflow.Cancelled:
                    entry.State="stopped"
                    entry.Note="Engineering run was cancelled"
                default:
                    entry.State="active"
                    entry.Note="Engineering has not confirmed successful completion"
                }
            }
            report.Delegations=append(report.Delegations,entry)
            if len(report.Delegations)==100 {
                report.Limitations=append(report.Limitations,"Only the first 100 delegated links are included")
                return report
            }
        }
    }
    if len(report.Delegations)==0{
        report.Limitations=append(report.Limitations,"No personal task has an explicit engineering-run link")
    }
    return report
}
func (s *Server) uiTaskDelegations(w http.ResponseWriter,r *http.Request) {
    writeJSON(w,http.StatusOK,s.taskDelegations())
}

func engineeringRunRef(id string) string {
    return "homelab:engineering_run:"+strings.TrimSpace(id)
}
