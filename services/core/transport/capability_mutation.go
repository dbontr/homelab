package transport

import (
 "context"
 "crypto/sha256"
 "encoding/hex"
 "errors"
 "strings"

 "github.com/dbontr/homelab-new/system/agentflow"
 "github.com/dbontr/homelab-new/system/store"
)

// A model-authored plan never crosses the personal write boundary. Only a
// separately submitted and explicitly authorized stage can mutate records.
func validSubmittedCapabilityStage(stage CapabilityStage, personalRead,personalWrite bool) error {
 switch stage.Action {
 case "task_create","goal_create","task_complete":
  if !personalWrite{return errors.New("personal workflow mutation requires explicit personal_write")}
  if !workflowStepID.MatchString(stage.ID)||len(stage.DependsOn)>12||stage.Limit!=0 {
   return errors.New("invalid personal workflow stage")
  }
  switch stage.Action {
  case "task_create","goal_create":
   if stage.ExpectedRevision!=0||len(stage.Query)<1||len(stage.Query)>120||
    strings.TrimSpace(stage.Query)==""||strings.ContainsAny(stage.Query,"\x00\r\n"){
    return errors.New("invalid personal record title")
   }
  case "task_complete":
   decoded,err:=hex.DecodeString(stage.Query)
   if err!=nil||len(decoded)!=12||stage.ExpectedRevision<1{
    return errors.New("task completion requires exact task ID and revision")
   }
  }
  return nil
 default:
  return validCapabilityStage(stage,personalRead)
 }
}

// The authoritative personal Store atomically records a workflow receipt with
// every write, so a recovered expired lease cannot repeat a side effect.
func (s *Server) executeAuthorizedPersonalAction(ctx context.Context,w agentflow.Workflow,
 step agentflow.Claim,query string,limit,expectedRevision int)(any,error){
 if !w.CapabilityPlan||!w.PersonalWrite{return nil,errors.New("workflow lacks personal write authority")}
 stage:=CapabilityStage{ID:step.StepID,Action:step.Kind,Query:query,Limit:limit,ExpectedRevision:expectedRevision}
 if err:=validSubmittedCapabilityStage(stage,w.PersonalRead,w.PersonalWrite);err!=nil{return nil,err}
 if _,err:=s.projectContext(ctx,w.Project);err!=nil{return nil,err}
 digest:=sha256.Sum256([]byte(w.ID+":"+step.StepID+":"+w.Project))
 key:=hex.EncodeToString(digest[:])
 switch step.Kind{
 case "task_create":
  task,err:=s.db.AddTaskOnce(key,store.TaskInput{Title:strings.TrimSpace(query),
    Links:[]string{"homelab:project:"+w.Project}})
  if err!=nil{return nil,err}
  return map[string]any{"action":"task_create","task_id":task.ID,"revision":task.Revision,
   "project_id":w.Project,"status":"recorded"},nil
 case "goal_create":
  goal,err:=s.db.AddGoalOnce(key,strings.TrimSpace(query),"")
  if err!=nil{return nil,err}
  return map[string]any{"action":"goal_create","goal_id":goal.ID,
   "project_id":w.Project,"status":"recorded"},nil
 case "task_complete":
  authorized:=false
  for _,task:=range s.db.Tasks("") {
   if task.ID!=query{continue}
   for _,link:=range task.Links{
    if link=="homelab:project:"+w.Project{authorized=true;break}
   }
   break
  }
  if !authorized{return nil,errors.New("task is not linked to the authorized project")}
  task,err:=s.db.CompleteTaskOnce(key,query,expectedRevision)
  if err!=nil{return nil,err}
  return map[string]any{"action":"task_complete","task_id":task.ID,"revision":task.Revision,
   "project_id":w.Project,"status":"recorded"},nil
 }
 return nil,errors.New("unregistered personal action")
}
