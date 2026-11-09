package transport

import (
    "bytes"
    "context"
    "encoding/json"
    "errors"
    "io"
    "time"

    "github.com/dbontr/homelab-new/system/agentflow"
)

// Continuation observations are bounded and never include personal record
// bodies. Source excerpts are included only after explicit owner consent.
func capabilityObservations(w agentflow.Workflow) []map[string]any {
    observations:=make([]map[string]any,0,len(w.Steps))
    start:=0
    if len(w.Steps)>10{start=len(w.Steps)-10}
    for _,step:=range w.Steps[start:]{
        item:=map[string]any{"id":step.ID,"action":step.Kind,"status":step.Status}
        if w.ShareProjectEvidence && step.Kind=="research_search" && step.Status==agentflow.Succeeded {
            var passages []map[string]any
            if err:=json.Unmarshal(step.Result,&passages);err==nil{
                evidence:=make([]map[string]string,0,3)
                for _,passage:=range passages {
                    if len(evidence)>=3{break}
                    citation,_:=passage["citation_ref"].(string)
                    excerpt,_:=passage["excerpt"].(string)
                    if citation==""||len(citation)>160{continue}
                    if len(excerpt)>260{excerpt=excerpt[:260]}
                    evidence=append(evidence,map[string]string{"citation_ref":citation,"excerpt":excerpt})
                }
                if len(evidence)>0{item["evidence"]=evidence}
            }
        }
        observations=append(observations,item)
    }
    return observations
}

func (s *Server) advanceCapabilityReplan(ctx context.Context,id string) error {
    if s.workflows==nil{return errors.New("capability workflow store unavailable")}
    workflow,err:=s.workflows.Get(id)
    if err!=nil{return err}
    if !workflow.CapabilityPlan||!workflow.AutoReplan||workflow.PlanComplete||workflow.PlanError!=""||
        workflow.Replans>=agentflow.MaxCapabilityReplans||workflow.Cancelled||
        (!workflow.Deadline.IsZero()&&!time.Now().Before(workflow.Deadline)){
        return nil
    }
    for _,step:=range workflow.Steps {
        if step.Status!=agentflow.Succeeded{return nil}
    }
    fail:=func()error{
        // The workflow must not be displayed as successfully completed when
        // the required model continuation could not be safely determined.
        return s.workflows.ExtendCapabilityPlan(workflow.ID,workflow.Revision,nil,true,"model continuation unavailable")
    }
    if s.capabilityPlanner==nil{return fail()}
    if _,err:=s.projectContext(ctx,workflow.Project);err!=nil{return fail()}
    raw,err:=s.capabilityPlanner.ContinueWorkflowStages(ctx,workflow.Project,workflow.Objective,
        capabilityObservations(workflow),agentflow.MaxCapabilityReplans-workflow.Replans)
    if err!=nil{return fail()}
    var decision struct{
        Finish bool `json:"finish"`
        Steps []CapabilityStage `json:"steps"`
    }
    decoder:=json.NewDecoder(bytes.NewReader(raw))
    decoder.DisallowUnknownFields()
    if err:=decoder.Decode(&decision);err!=nil{return fail()}
    if err:=decoder.Decode(new(any));err!=io.EOF{return fail()}
    if decision.Finish {
        if len(decision.Steps)!=0{return fail()}
        return s.workflows.ExtendCapabilityPlan(workflow.ID,workflow.Revision,nil,true,"")
    }
    if len(decision.Steps)==0||len(decision.Steps)>3||
        len(workflow.Steps)+len(decision.Steps)>agentflow.MaxCapabilityStages{return fail()}
    additions:=make([]agentflow.Step,0,len(decision.Steps))
    // At least one prerequisite for each new stage must be a confirmed stage
    // or a stage proposed earlier in the same continuation.
    reachable:=make(map[string]bool,len(workflow.Steps)+len(decision.Steps))
    for _,step:=range workflow.Steps{reachable[step.ID]=true}
    for _,stage:=range decision.Steps{
        if err:=validCapabilityStage(stage,workflow.PersonalRead);err!=nil{return fail()}
        if reachable[stage.ID]||len(stage.DependsOn)==0{return fail()}
        depends:=false
        for _,prerequisite:=range stage.DependsOn{if reachable[prerequisite]{depends=true}}
        if !depends{return fail()}
        input,_:=json.Marshal(map[string]any{"query":stage.Query,"limit":stage.Limit})
        additions=append(additions,agentflow.Step{
            ID:stage.ID,DependsOn:append([]string(nil),stage.DependsOn...),Kind:stage.Action,Input:input,MaxAttempts:2,
        })
        reachable[stage.ID]=true
    }
    return s.workflows.ExtendCapabilityPlan(workflow.ID,workflow.Revision,additions,false,"")
}

