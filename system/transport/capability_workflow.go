package transport

import (
    "context"
    "crypto/rand"
    "encoding/hex"
    "encoding/json"
    "errors"
    "fmt"
    "net/http"
    "os"
    "regexp"
    "strings"
    "time"

    "github.com/dbontr/homelab-new/system/agentflow"
    "github.com/dbontr/homelab-new/system/store"
)

var workflowStepID=regexp.MustCompile("^[a-z][a-z0-9_-]{0,79}$")

type CapabilityStage struct {
    ID string `json:"id"`
    DependsOn []string `json:"depends_on,omitempty"`
    Action string `json:"action"`
    Query string `json:"query,omitempty"`
    Limit int `json:"limit,omitempty"`
    ExpectedRevision int `json:"expected_revision,omitempty"`
}
type CapabilityProgram struct {
    Project string `json:"project"`
    Objective string `json:"objective"`
    PersonalRead bool `json:"personal_read,omitempty"`
    PersonalWrite bool `json:"personal_write,omitempty"`
    AutoReplan bool `json:"auto_replan,omitempty"`
    ShareProjectEvidence bool `json:"share_project_evidence,omitempty"`
    Steps []CapabilityStage `json:"steps"`
}
type CapabilityStepResult struct {
    ID string `json:"id"`
    Action string `json:"action"`
    DependsOn []string `json:"depends_on,omitempty"`
    Status agentflow.Status `json:"status"`
    Attempts int `json:"attempts"`
    Error string `json:"error,omitempty"`
    Result json.RawMessage `json:"result,omitempty"`
}
type CapabilityWorkflow struct {
    ID string `json:"id"`
    Project string `json:"project"`
    Objective string `json:"objective"`
    Revision uint64 `json:"revision"`
    Status string `json:"status"`
    Replans int `json:"replans,omitempty"`
    PlanError string `json:"plan_error,omitempty"`
    PersonalWrite bool `json:"personal_write,omitempty"`
    CreatedAt time.Time `json:"created_at"`
    Deadline time.Time `json:"deadline"`
    Steps []CapabilityStepResult `json:"steps"`
}

func requiresPersonalRead(kind string) bool {
    switch kind {
    case "project_tasks","learning_summary","learning_sessions","learning_reviews","health_goals","health_records","health_training_sets","health_food_proposals","recipe_search","recipe_meals","recipe_groceries","recipe_nutrition","task_review","media_requests": return true
    }
    return false
}
func validCapabilityStage(stage CapabilityStage, allowPersonal bool) error {
    if !workflowStepID.MatchString(stage.ID)||len(stage.DependsOn)>12||
       len(stage.Query)>120||strings.ContainsAny(stage.Query,"\x00\r\n")||
       stage.Limit<0||stage.Limit>25||stage.ExpectedRevision!=0 {
        return errors.New("invalid workflow stage")
    }
    if requiresPersonalRead(stage.Action)&&!allowPersonal {
        return errors.New("personal capability access requires explicit authorization")
    }
    switch stage.Action {
    case "project_context","project_tasks","engineering_runs","research_claims","learning_summary","learning_sessions","learning_reviews","health_goals","health_records","health_training_sets","health_food_proposals","recipe_meals","media_requests":
        if stage.Query!=""||stage.Limit!=0{return errors.New("unexpected capability parameters")}
    case "study_pack_preview":
        if stage.Query!=""||stage.Limit>10{return errors.New("invalid study pack stage")}
    case "task_review":
        if stage.Query!=""{return errors.New("unexpected task review query")}
    case "recipe_groceries":
        if stage.Limit>14||stage.Query!=""&&!validCapabilityDay(stage.Query){return errors.New("invalid grocery interval")}
    case "recipe_nutrition":
        if strings.TrimSpace(stage.Query)==""||strings.Contains(stage.Query,"/"){return errors.New("invalid recipe reference")}
    case "research_claim_detail":
        if strings.TrimSpace(stage.Query)==""||stage.Limit!=0{return errors.New("claim details require an exact claim ID")}
    case "research_search","research_semantic_query":
        if strings.TrimSpace(stage.Query)=="" {return errors.New("research search requires a query")}
    case "recipe_search":
        if strings.TrimSpace(stage.Query)=="" {return errors.New("recipe search requires a query")}
    default:
        return errors.New("capability operation is not approved")
    }
    return nil
}
func validCapabilityDay(day string)bool{
 if len(day)!=10{return false}
 _,err:=time.Parse("2006-01-02",day)
 return err==nil
}
func summarizeCapabilityWorkflow(w agentflow.Workflow) CapabilityWorkflow {
    summary:=CapabilityWorkflow{
        ID:w.ID,Project:w.Project,Objective:w.Objective,Revision:w.Revision,
        CreatedAt:w.CreatedAt,Deadline:w.Deadline,Replans:w.Replans,PlanError:w.PlanError,PersonalWrite:w.PersonalWrite,
        Steps:make([]CapabilityStepResult,0,len(w.Steps)),
    }
    success:=true
    failed:=false
    running:=false
    for _,step:=range w.Steps {
        summary.Steps=append(summary.Steps,CapabilityStepResult{
            ID:step.ID,Action:step.Kind,DependsOn:step.DependsOn,
            Status:step.Status,Attempts:step.Attempts,Error:step.LastError,
            Result:step.Result,
        })
        if step.Status!=agentflow.Succeeded {success=false}
        if step.Status==agentflow.Failed {failed=true}
        if step.Status==agentflow.Running {running=true}
    }
    switch {
    case w.Cancelled: summary.Status="cancelled"
    case failed: summary.Status="failed"
    case w.PlanError!="": summary.Status="planning_failed"
    case success&&w.AutoReplan&&!w.PlanComplete&&w.Replans<2&&time.Now().Before(w.Deadline):summary.Status="planning"
    case success: summary.Status="succeeded"
    case !w.Deadline.IsZero() && !time.Now().Before(w.Deadline):summary.Status="expired"
    case running:summary.Status="running"
    default:summary.Status="pending"
    }
    return summary
}

func (s *Server) SetWorkflows(store *agentflow.Store) {s.workflows=store}
func (s *Server) SetCapabilityPlanner(planner agentflow.CapabilityPlanner){s.capabilityPlanner=planner}

func (s *Server) submitCapabilityWorkflow(ctx context.Context,program CapabilityProgram)(CapabilityWorkflow,error) {
    if s.workflows==nil{return CapabilityWorkflow{},errors.New("workflow store unavailable")}
    if program.AutoReplan&&s.capabilityPlanner==nil{return CapabilityWorkflow{},errors.New("automatic planning requires an approved model")}
    if program.ShareProjectEvidence&&!program.AutoReplan{return CapabilityWorkflow{},errors.New("model evidence sharing requires automatic planning")}
    if !consoleProjectID.MatchString(program.Project)||strings.TrimSpace(program.Objective)==""||
        len(program.Objective)>2000||len(program.Steps)==0||len(program.Steps)>12 {
        return CapabilityWorkflow{},errors.New("invalid workflow objective or scope")
    }
    // The controller is authoritative for the project, not the user-supplied ID.
    if _,err:=s.projectContext(ctx,program.Project);err!=nil{return CapabilityWorkflow{},err}
    var token [16]byte
    if _,err:=rand.Read(token[:]);err!=nil{return CapabilityWorkflow{},err}
    w:=agentflow.Workflow{
        ID:hex.EncodeToString(token[:]),Project:program.Project,Objective:strings.TrimSpace(program.Objective),
        PersonalRead:program.PersonalRead,PersonalWrite:program.PersonalWrite,CapabilityPlan:true,AutoReplan:program.AutoReplan,
        ShareProjectEvidence:program.ShareProjectEvidence,Deadline:time.Now().Add(30*time.Minute),
        Steps:make([]agentflow.Step,0,len(program.Steps)),
    }
    for _,stage:=range program.Steps {
        if err:=validSubmittedCapabilityStage(stage,program.PersonalRead,program.PersonalWrite);err!=nil{return CapabilityWorkflow{},err}
        input,_:=json.Marshal(struct{
            Query string `json:"query,omitempty"`
            Limit int `json:"limit,omitempty"`
            ExpectedRevision int `json:"expected_revision,omitempty"`
        }{Query:stage.Query,Limit:stage.Limit,ExpectedRevision:stage.ExpectedRevision})
        w.Steps=append(w.Steps,agentflow.Step{
            ID:stage.ID,DependsOn:append([]string(nil),stage.DependsOn...),
            Kind:stage.Action,Input:input,MaxAttempts:2,
        })
    }
    if err:=s.workflows.Create(w);err!=nil{return CapabilityWorkflow{},err}
    current,err:=s.workflows.Get(w.ID)
    if err!=nil{return CapabilityWorkflow{},err}
    return summarizeCapabilityWorkflow(current),nil
}

func (s *Server) capabilityWorkflow(ctx context.Context,id string)(CapabilityWorkflow,error) {
    if s.workflows==nil{return CapabilityWorkflow{},errors.New("workflow store unavailable")}
    w,err:=s.workflows.Get(id)
    if err!=nil{return CapabilityWorkflow{},err}
    return summarizeCapabilityWorkflow(w),nil
}
func (s *Server) capabilityWorkflows()([]CapabilityWorkflow,error) {
    if s.workflows==nil{return nil,errors.New("workflow store unavailable")}
    ids,err:=s.workflows.List()
    if err!=nil{return nil,err}
    result:=make([]CapabilityWorkflow,0,len(ids))
    for _,id:=range ids {
        w,err:=s.workflows.Get(id)
        if err!=nil{return nil,err}
        summary:=summarizeCapabilityWorkflow(w)
        // Listings never expose full personal observations or evidence bodies.
        // Authenticated detail reads still return explicitly approved results.
        for i:=range summary.Steps{summary.Steps[i].Result=nil}
        result=append(result,summary)
    }
    return result,nil
}

// A capability workflow only calls an enumerated read operation. The owner
// cannot smuggle arbitrary MCP names, mutation arguments, or privileged grants.
func (s *Server) executeCapabilityStep(ctx context.Context,w agentflow.Workflow,step agentflow.Claim)(any,error) {
    var input struct{Query string `json:"query"`;Limit int `json:"limit"`;ExpectedRevision int `json:"expected_revision"`}
    if err:=json.Unmarshal(step.Input,&input);err!=nil{return nil,err}
    if step.Kind=="task_create"||step.Kind=="goal_create"||step.Kind=="task_complete"{
        return s.executeAuthorizedPersonalAction(ctx,w,step,input.Query,input.Limit,input.ExpectedRevision)
    }
    if err:=validCapabilityStage(CapabilityStage{ID:step.StepID,Action:step.Kind,Query:input.Query,Limit:input.Limit,ExpectedRevision:input.ExpectedRevision},w.PersonalRead);err!=nil{return nil,err}
    switch step.Kind {
    case "project_context":
        return s.projectContext(ctx,w.Project)
    case "project_tasks":
        overview,err:=s.projectOverview(ctx,w.Project)
        if err!=nil{return nil,err}
        return map[string]any{"tasks":overview.Tasks,"goals":overview.Goals,"links":overview.Links},nil
    case "task_review":
        overview,err:=s.projectOverview(ctx,w.Project)
        if err!=nil{return nil,err}
        days:=input.Limit
        if days==0{days=7}
        review,err:=s.db.ReviewTasks(time.Now(),days)
        if err!=nil{return nil,err}
        allowed:=map[string]bool{}
        for _,task:=range overview.Tasks{allowed[task.ID]=true}
        filtered:=make([]store.TaskReviewEntry,0,len(review.Entries))
        for _,entry:=range review.Entries{if allowed[entry.TaskID]{filtered=append(filtered,entry)}}
        review.Entries=filtered
        return review,nil
    case "study_pack_preview":
        prerequisites:=map[string]bool{}
        for _,candidate:=range w.Steps{
            if candidate.ID==step.StepID{for _,id:=range candidate.DependsOn{prerequisites[id]=true}}
        }
        passageIDs:=[]string{}
        for _,candidate:=range w.Steps{
            if !prerequisites[candidate.ID]||candidate.Kind!="research_search"||candidate.Status!=agentflow.Succeeded{continue}
            var matches []map[string]any
            if err:=json.Unmarshal(candidate.Result,&matches);err!=nil{return nil,errors.New("research evidence is invalid")}
            for _,match:=range matches{
                if len(passageIDs)>=10{break}
                id,_:=match["id"].(string)
                if id!=""{passageIDs=append(passageIDs,id)}
            }
        }
        if len(passageIDs)==0{return nil,errors.New("study pack requires confirmed research results")}
        if input.Limit>0&&len(passageIDs)>input.Limit{passageIDs=passageIDs[:input.Limit]}
        return s.capabilityBackendRead(ctx,"workspace","study_pack_prepare",map[string]any{
            "project_id":w.Project,"title":"Research evidence","objective":w.Objective,"passage_ids":passageIDs,
        })
    case "research_semantic_query":
        limit:=input.Limit
        if limit==0{limit=8}
        return s.capabilityBackendRead(ctx,"workspace","research_semantic_query",map[string]any{
            "project_id":w.Project,"query":input.Query,"limit":limit,
        })
    case "research_search":
        limit:=input.Limit
        if limit==0{limit=8}
        return s.capabilityBackendRead(ctx,"workspace","research_search",map[string]any{
            "project_id":w.Project,"query":input.Query,"limit":limit,
        })
    case "research_claims":
        return s.capabilityBackendRead(ctx,"workspace","research_claims",map[string]any{"project_id":w.Project})
    case "research_claim_detail":
        return s.capabilityBackendRead(ctx,"workspace","research_claim_detail",map[string]any{"project_id":w.Project,"claim_id":input.Query})
    case "engineering_runs":
        if s.engineering==nil{return nil,errResourceUnavailable}
        runs,err:=s.engineering.List()
        if err!=nil{return nil,err}
        filtered:=make([]agentflow.RunSummary,0)
        for _,run:=range runs {if run.Project==w.Project {filtered=append(filtered,run)}}
        return filtered,nil
    case "learning_summary":
        return s.capabilityBackendRead(ctx,"learning","get_learning_summary",map[string]any{})
    case "learning_sessions":
        return s.capabilityBackendRead(ctx,"learning","list_learning_sessions",map[string]any{"limit":20})
    case "learning_reviews":
        return s.capabilityBackendRead(ctx,"learning","get_review_queue",map[string]any{})
    case "health_goals":
        return s.capabilityBackendRead(ctx,"health","health_goal_list",map[string]any{})
    case "health_records":
        return s.capabilityBackendRead(ctx,"health","health_record_list",map[string]any{})
    case "health_training_sets":
        return s.capabilityBackendRead(ctx,"health","health_training_set_list",map[string]any{})
    case "health_food_proposals":
        return s.capabilityBackendRead(ctx,"health","health_food_proposal_list",map[string]any{})
    case "recipe_search":
        if s.recipes==nil{return nil,errResourceUnavailable}
        return s.recipes.List(input.Query),nil
    case "recipe_meals":
        if s.recipes==nil{return nil,errResourceUnavailable}
        return s.recipes.Meals(),nil
    case "recipe_groceries":
        if s.recipes==nil{return nil,errResourceUnavailable}
        day:=input.Query
        if day==""{day=time.Now().UTC().Format("2006-01-02")}
        start,err:=time.Parse("2006-01-02",day)
        if err!=nil{return nil,err}
        days:=input.Limit
        if days==0{days=7}
        return s.recipes.Board(day,start.AddDate(0,0,days).Format("2006-01-02"))
    case "recipe_nutrition":
        if s.recipes==nil{return nil,errResourceUnavailable}
        portions:=input.Limit
        if portions==0{portions=1}
        return s.recipes.Nutrition(input.Query,float64(portions))
    case "media_requests":
        if s.media==nil{return nil,errResourceUnavailable}
        return s.media.Requests(ctx)
    default:
        return nil,errors.New("unsupported capability operation")
    }
}
func (s *Server) capabilityBackendRead(ctx context.Context,backend,tool string,args map[string]any)(any,error) {
    raw,err:=json.Marshal(args)
    if err!=nil{return nil,err}
    reply:=s.invoke(ctx,backend+"__"+tool,raw,json.RawMessage("1"))
    if failed,_:=reply["isError"].(bool);failed{return nil,errResourceUnavailable}
    parts,ok:=reply["content"].([]any)
    if !ok||len(parts)!=1{return nil,errResourceUnavailable}
    part,ok:=parts[0].(map[string]any)
    if !ok{return nil,errResourceUnavailable}
    content,ok:=part["text"].(string)
    if !ok||len(content)>32<<10{return nil,errors.New("capability response exceeds limit")}
    var data any
    if err:=json.Unmarshal([]byte(content),&data);err!=nil{return nil,err}
    return data,nil
}

type capabilityDispatcher struct{server *Server}
func (d capabilityDispatcher) Dispatch(ctx context.Context,claim agentflow.Claim)(json.RawMessage,error) {
    w,err:=d.server.workflows.Get(claim.WorkflowID)
    if err!=nil{return nil,err}
    if w.Cancelled{return nil,agentflow.ErrAgentRejected}
    result,err:=d.server.executeCapabilityStep(ctx,w,claim)
    if err!=nil{return nil,err}
    encoded,err:=json.Marshal(result)
    if err!=nil{return nil,err}
    if len(encoded)>48<<10 {return nil,errors.New("capability result exceeds persistence limit")}
    return encoded,nil
}

func (s *Server) advanceCapabilityWorkflow(ctx context.Context,id string,maxSteps int)error {
    if s.workflows==nil{return errors.New("workflow store unavailable")}
    runner:=agentflow.Runner{Store:s.workflows,Worker:capabilityDispatcher{s},Lease:time.Minute}
    for n:=0;n<maxSteps;n++ {
        advanced,err:=runner.RunOne(ctx,id)
        if err!=nil{return err}
        if !advanced{return s.advanceCapabilityReplan(ctx,id)}
    }
    return nil
}

// Model-authored stages remain read-only. Exact owner-submitted personal task
// creation requires separately recorded write consent at submission.
func (s *Server) StartWorkflowScheduler(ctx context.Context) {
    if s.workflows==nil{return}
    go func() {
        ticker:=time.NewTicker(10*time.Second)
        defer ticker.Stop()
        for {
            select {
            case <-ctx.Done():return
            case <-ticker.C:
                period,cancel:=context.WithTimeout(ctx,8*time.Second)
                _=s.fireDueAutomations(period)
                cancel()
                ids,err:=s.workflows.List()
                if err!=nil{continue}
                for _,id:=range ids {
                    if ctx.Err()!=nil{return}
                    current,err:=s.capabilityWorkflow(ctx,id)
                    if err!=nil||current.Status=="succeeded"||current.Status=="failed"||
                        current.Status=="cancelled"||current.Status=="expired"||current.Status=="planning_failed"{continue}
                    budget,cancel:=context.WithTimeout(ctx,8*time.Second)
                    _=s.advanceCapabilityWorkflow(budget,id,1)
                    cancel()
                }
            }
        }
    }()
}

func (s *Server) capabilityWorkflowCollection(w http.ResponseWriter,r *http.Request) {
    if s.workflows==nil{writeError(w,503,"workflows unavailable");return}
    if r.Method==http.MethodGet {
        flows,err:=s.capabilityWorkflows()
        if err!=nil{writeError(w,500,"workflow listing unavailable");return}
        writeJSON(w,200,map[string]any{"workflows":flows});return
    }
    var program CapabilityProgram
    if err:=decode(r,&program);err!=nil{writeError(w,400,err.Error());return}
    flow,err:=s.submitCapabilityWorkflow(r.Context(),program)
    if err!=nil{writeError(w,400,err.Error());return}
    writeJSON(w,202,flow)
}
func (s *Server) capabilityWorkflowItem(w http.ResponseWriter,r *http.Request) {
    if s.workflows==nil{writeError(w,503,"workflows unavailable");return}
    id:=r.PathValue("id")
    if r.Method==http.MethodPost {
        var input struct{Revision uint64 `json:"revision"`}
        if err:=decode(r,&input);err!=nil{writeError(w,400,err.Error());return}
        if err:=s.workflows.Cancel(id,input.Revision);err!=nil{writeError(w,409,err.Error());return}
    }
    flow,err:=s.capabilityWorkflow(r.Context(),id)
    if err!=nil{
        if errors.Is(err,os.ErrNotExist){writeError(w,404,"workflow not found");return}
        writeError(w,400,"workflow unavailable");return
    }
    writeJSON(w,200,flow)
}
func (s *Server) capabilityWorkflowTool(ctx context.Context,name string,raw json.RawMessage)(any,error) {
    if s.workflows==nil{return nil,errors.New("workflows unavailable")}
    switch name {
    case "workflow_plan":
        var input CapabilityPlanInput
        if err:=json.Unmarshal(raw,&input);err!=nil{return nil,err}
        return s.proposeCapabilityWorkflow(ctx,input)
    case "workflow_submit":
        var program CapabilityProgram
        if err:=json.Unmarshal(raw,&program);err!=nil{return nil,err}
        return s.submitCapabilityWorkflow(ctx,program)
    case "workflow_list":
        return s.capabilityWorkflows()
    case "workflow_get":
        var input struct{ID string `json:"id"`}
        if err:=json.Unmarshal(raw,&input);err!=nil{return nil,err}
        return s.capabilityWorkflow(ctx,input.ID)
    case "workflow_cancel":
        var input struct{ID string `json:"id"`;Revision uint64 `json:"revision"`}
        if err:=json.Unmarshal(raw,&input);err!=nil{return nil,err}
        if err:=s.workflows.Cancel(input.ID,input.Revision);err!=nil{return nil,err}
        return s.capabilityWorkflow(ctx,input.ID)
    default:return nil,fmt.Errorf("unknown workflow operation: %s",name)
    }
}
