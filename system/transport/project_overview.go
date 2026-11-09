package transport

import (
    "context"
    "encoding/json"
    "errors"
    "net/http"

    "github.com/dbontr/homelab-new/system/agentflow"
    "github.com/dbontr/homelab-new/system/store"
)

// ProjectOverview joins records through verified project identity without
// transferring ownership or persisting a copy of another service's state.
type ProjectOverview struct {
    Reference string `json:"reference"`
    Project any `json:"project"`
    Repositories any `json:"repositories"`
    Libraries any `json:"libraries"`
    Tasks []store.Task `json:"tasks"`
    Goals []store.Goal `json:"goals"`
    EngineeringRuns []agentflow.RunSummary `json:"engineering_runs"`
    Links []ResourceLink `json:"links"`
    Unavailable []string `json:"unavailable,omitempty"`
}

func (s *Server) projectContext(ctx context.Context, project string) (map[string]any,error) {
    if !consoleProjectID.MatchString(project) { return nil,errResourceInvalid }
    if !s.hasBackend("workspace") { return nil,errResourceUnavailable }
    raw,_:=json.Marshal(map[string]string{"project_id":project})
    response:=s.invoke(ctx,"workspace__knowledge_context",raw,json.RawMessage("1"))
    if failed,_:=response["isError"].(bool);failed {return nil,errResourceUnavailable}
    parts,ok:=response["content"].([]any)
    if !ok || len(parts)!=1 {return nil,errResourceUnavailable}
    part,ok:=parts[0].(map[string]any)
    if !ok {return nil,errResourceUnavailable}
    encoded,ok:=part["text"].(string)
    if !ok || len(encoded)>2<<20 {return nil,errResourceUnavailable}
    var value map[string]any
    if json.Unmarshal([]byte(encoded),&value)!=nil{return nil,errResourceUnavailable}
    identity,ok:=value["project"].(map[string]any)
    if !ok || identity["id"]!=project {return nil,errResourceUnavailable}
    return value,nil
}

func (s *Server) projectOverview(ctx context.Context, project string) (ProjectOverview,error) {
    projectContext,err:=s.projectContext(ctx,project)
    if err!=nil{return ProjectOverview{},err}
    ref:=resourceRef("project",project)
    overview:=ProjectOverview{
        Reference:ref,Project:projectContext["project"],
        Repositories:projectContext["repositories"],Libraries:projectContext["libraries"],
        Tasks:[]store.Task{},Goals:[]store.Goal{},
        EngineeringRuns:[]agentflow.RunSummary{},Links:[]ResourceLink{},
    }
    goals:=map[string]bool{}
    for _,task:=range s.db.Tasks("") {
        linked:=false
        for _,candidate:=range task.Links {if candidate==ref {linked=true;break}}
        if !linked {continue}
        overview.Tasks=append(overview.Tasks,task)
        overview.Links=append(overview.Links,ResourceLink{Relation:"task",Reference:resourceRef("task",task.ID)})
        for _,link:=range taskLinks(task) {
            if link.Relation!="related"{continue}
            kind,_,err:=parseResourceReference(link.Reference)
            if err==nil && kind!="project" {overview.Links=append(overview.Links,link)}
        }
        if task.GoalID!="" {goals[task.GoalID]=true}
    }
    for _,goal:=range s.db.Goals() {
        if !goals[goal.ID] {continue}
        overview.Goals=append(overview.Goals,goal)
        overview.Links=append(overview.Links,ResourceLink{Relation:"goal",Reference:resourceRef("goal",goal.ID)})
    }
    if s.engineering!=nil {
        runs,err:=s.engineering.List()
        if err!=nil {overview.Unavailable=append(overview.Unavailable,"engineering")} else {
            for _,run:=range runs {
                if run.Project==project {
                    overview.EngineeringRuns=append(overview.EngineeringRuns,run)
                    overview.Links=append(overview.Links,ResourceLink{Relation:"engineering_run",Reference:resourceRef("engineering_run",run.ID)})
                }
            }
        }
    } else {overview.Unavailable=append(overview.Unavailable,"engineering")}
    // Avoid duplicate relations from multiple linked tasks without dropping
    // distinct owner-managed edges.
    unique:=make([]ResourceLink,0,len(overview.Links))
    seen:=map[string]bool{}
    for _,link:=range overview.Links {
        key:=link.Relation+"\x00"+link.Reference
        if !seen[key]{unique=append(unique,link);seen[key]=true}
    }
    overview.Links=sortedResourceLinks(unique)
    return overview,nil
}

func (s *Server) uiProjectOverview(w http.ResponseWriter,r *http.Request) {
    overview,err:=s.projectOverview(r.Context(),r.PathValue("project"))
    switch {
    case errors.Is(err,errResourceInvalid):writeError(w,400,err.Error())
    case err!=nil:writeError(w,503,"project context unavailable")
    default:writeJSON(w,200,overview)
    }
}
