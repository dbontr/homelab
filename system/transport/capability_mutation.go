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

// Model-planned stages use the read-only validator. Mutations are accepted
// only as exact, owner-submitted stages with a separate write grant.
func validSubmittedCapabilityStage(stage CapabilityStage,personalRead,personalWrite bool)error{
 if stage.Action!="task_create"{return validCapabilityStage(stage,personalRead)}
 if !personalWrite{return errors.New("task creation requires explicit personal_write authorization")}
 if !workflowStepID.MatchString(stage.ID)||len(stage.DependsOn)>12||
  len(stage.Query)==0||len(stage.Query)>120||strings.TrimSpace(stage.Query)==""||
  strings.ContainsAny(stage.Query,"\x00\r\n")||stage.Limit!=0 {
  return errors.New("invalid task creation stage")
 }
 return nil
}

func (s *Server) executeAuthorizedTaskCreation(ctx context.Context,w agentflow.Workflow,
 step agentflow.Claim,title string,limit int)(any,error){
 if !w.CapabilityPlan||!w.PersonalWrite{return nil,errors.New("workflow lacks task mutation authority")}
 if err:=validSubmittedCapabilityStage(CapabilityStage{ID:step.StepID,Action:"task_create",Query:title,Limit:limit},
  w.PersonalRead,w.PersonalWrite);err!=nil{return nil,err}
 if _,err:=s.projectContext(ctx,w.Project);err!=nil{return nil,err}
 key:=sha256.Sum256([]byte(w.ID+":"+step.StepID+":"+w.Project))
 task,err:=s.db.AddTaskOnce(hex.EncodeToString(key[:]),store.TaskInput{
  Title:strings.TrimSpace(title),Links:[]string{"homelab:project:"+w.Project},
 })
 if err!=nil{return nil,err}
 return map[string]any{"task_id":task.ID,"project_id":w.Project,"status":"recorded"},nil
}
