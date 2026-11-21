package motivation

import (
 "errors"
 "strings"
 "unicode"
)

func validReference(s string)bool{
 if len(s)>128{return false}
 for _,r:=range s{
  if !unicode.IsLetter(r)&&!unicode.IsDigit(r)&&r!='-'&&r!='_' {return false}
 }
 return true
}

func validAttentionPlan(p AttentionPlan)bool{
 if p.ID==""||len(p.ID)>80||!validReference(p.ID)||
  len([]rune(p.Trigger))>240||len([]rune(p.Alternative))>240||
  len([]rune(p.Context))>128||len([]rune(p.Response))>400||
  len([]rune(p.Boundary))>320||len([]rune(p.Outcome))>320||
  !validReference(p.GoalID)||!validReference(p.TaskID)||!validReference(p.RuleKey)||
  p.CooldownMinutes<25||p.CooldownMinutes>1440{return false}
 if p.Enabled&&(p.Trigger==""||p.Response==""||p.Context==""){return false}
 for _,field:=range []string{p.Trigger,p.Alternative,p.Context,p.Response,p.Boundary,p.Outcome}{
  if hasControls(field){return false}
 }
 return true
}

// PutAttentionPlan is revision-fenced; a plan does not authorize or install
// monitoring, blocking, remote writes or unsolicited notifications.
func(e *Engine)PutAttentionPlan(id string,input AttentionPlanChange)(Record,error){
 id=strings.TrimSpace(id)
 plan:=AttentionPlan{
  ID:id,Enabled:input.Enabled,Paused:input.Paused,
  Trigger:strings.TrimSpace(input.Trigger),
  Alternative:strings.TrimSpace(input.Alternative),
  Context:strings.TrimSpace(input.Context),
  Response:strings.TrimSpace(input.Response),
  Boundary:strings.TrimSpace(input.Boundary),
  Outcome:strings.TrimSpace(input.Outcome),
  GoalID:input.GoalID,TaskID:input.TaskID,RuleKey:input.RuleKey,
  CooldownMinutes:input.CooldownMinutes,UpdatedAt:Now(),
 }
 if !validAttentionPlan(plan){return Record{},InvalidError("invalid attention plan")}
 e.mu.Lock();defer e.mu.Unlock()
 next:=cloneState(e.state)
 for i:=range next.AttentionPlans{
  if next.AttentionPlans[i].ID!=id{continue}
  if input.ExpectedRevision!=next.AttentionPlans[i].Revision{return Record{},ConflictError("attention plan revision conflict")}
  revision,err:=nextRevision(input.ExpectedRevision);if err!=nil{return Record{},err}
  plan.Revision=revision
  next.AttentionPlans[i]=plan
  return e.commit(next)
 }
 if input.ExpectedRevision!=0{return Record{},ConflictError("attention plan revision conflict")}
 if len(next.AttentionPlans)>=64{return Record{},InvalidError("attention plan limit reached")}
 plan.Revision=1
 next.AttentionPlans=append(next.AttentionPlans,plan)
 return e.commit(next)
}

// AttentionResponse returns an owner-selected response only for an explicitly
// declared context. This read does not monitor activity or initiate a prompt.
func(e *Engine)AttentionResponse(trigger,context string)(AttentionPlan,error){
 e.mu.Lock();defer e.mu.Unlock()
 if !e.state.Profile.Enabled{return AttentionPlan{},ConflictError("motivation interventions are paused")}
 if trigger==""||context==""{return AttentionPlan{},InvalidError("attention trigger and context required")}
 for _,plan:=range e.state.AttentionPlans{
  if plan.Enabled&&!plan.Paused&&plan.Trigger==trigger&&plan.Context==context{
   return plan,nil
  }
 }
 return AttentionPlan{},errors.New("no enabled attention plan for this context")
}
