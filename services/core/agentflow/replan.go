package agentflow

import (
    "errors"
    "time"
)

const (
    MaxCapabilityReplans = 2
    MaxCapabilityStages = 12
)

// ExtendCapabilityPlan adds validated read-only work to a completed capability
// round. An expected revision fences concurrent schedulers and cancellation.
func (s *Store) ExtendCapabilityPlan(id string, revision uint64, addition []Step, finished bool, planningError string) error {
    if (finished && len(addition)!=0) || (!finished && len(addition)==0) ||
        len(addition)>3 || len(planningError)>512 || (planningError!="" && (!finished||len(addition)!=0)) {
        return errors.New("invalid capability continuation")
    }
    unlock,err:=s.lock()
    if err!=nil{return err}
    defer unlock()
    w,err:=s.read(id)
    if err!=nil{return err}
    if !w.CapabilityPlan||!w.AutoReplan||w.TeamID!=""||w.IntegrationTeamID!=""{
        return errors.New("workflow is not eligible for automatic planning")
    }
    if w.Revision!=revision{return errors.New("workflow revision conflict")}
    if w.Cancelled||w.PlanComplete||w.PlanError!=""||w.Replans>=MaxCapabilityReplans||
        (!w.Deadline.IsZero()&&!s.now().Before(w.Deadline)){
        return errors.New("capability planning no longer permitted")
    }
    for _,st:=range w.Steps{
        if st.Status!=Succeeded {return errors.New("previous capability stages are not complete")}
    }
    if len(w.Steps)+len(addition)>MaxCapabilityStages {
        return errors.New("capability plan has reached its stage limit")
    }
    existing:=map[string]bool{}
    for _,st:=range w.Steps{existing[st.ID]=true}
    for i:=range addition {
        stage:=&addition[i]
        if existing[stage.ID] || len(stage.DependsOn)==0 {
            return errors.New("continuation requires unique dependent stages")
        }
        existing[stage.ID]=true
        stage.Status=Pending
        stage.Attempts=0
        stage.Generation=0
        stage.LeaseDigest=""
        stage.LeaseUntil=time.Time{}
        stage.Result=nil
        stage.LastError=""
        stage.ExternalStarted=false
        if stage.MaxAttempts!=2{return errors.New("invalid continuation attempt budget")}
    }
    candidate:=w
    candidate.Steps=append(append([]Step(nil),w.Steps...),addition...)
    if err:=Validate(candidate);err!=nil{return err}
    w.Steps=candidate.Steps
    if finished {
        w.PlanComplete=true
        w.PlanError=planningError
    } else {
        w.Replans++
    }
    w.Revision++
    return s.save(w)
}
