package transport

import (
 "bytes"
 "context"
 "encoding/json"
 "errors"
 "io"
 "net/http"
 "strings"

 "github.com/dbontr/homelab-new/system/agentflow"
)

type CapabilityPlanInput struct {
 Project string `json:"project"`
 Objective string `json:"objective"`
 PersonalRead bool `json:"personal_read,omitempty"`
}
type CapabilityPlanProposal struct{
 Program CapabilityProgram `json:"program"`
 RequiresOwnerSubmission bool `json:"requires_owner_submission"`
}

func (s *Server) proposeCapabilityWorkflow(ctx context.Context,input CapabilityPlanInput)(CapabilityPlanProposal,error){
 if s.capabilityPlanner==nil{return CapabilityPlanProposal{},errors.New("capability model is not configured")}
 if !consoleProjectID.MatchString(input.Project)||len(input.Objective)>2000||strings.TrimSpace(input.Objective)==""{
  return CapabilityPlanProposal{},errors.New("invalid planning scope")
 }
 // Check the owning controller before asking a model to propose stages.
 if _,err:=s.projectContext(ctx,input.Project);err!=nil{return CapabilityPlanProposal{},err}
 raw,err:=s.capabilityPlanner.ProposeWorkflowStages(ctx,input.Project,input.Objective,input.PersonalRead)
 if err!=nil{return CapabilityPlanProposal{},err}
 var proposed struct{Steps []CapabilityStage `json:"steps"`}
 decoder:=json.NewDecoder(bytes.NewReader(raw))
 decoder.DisallowUnknownFields()
 if err:=decoder.Decode(&proposed);err!=nil{return CapabilityPlanProposal{},errors.New("model returned an invalid plan")}
 if err:=decoder.Decode(new(any));err!=io.EOF{return CapabilityPlanProposal{},errors.New("model returned extra planning data")}
 if len(proposed.Steps)<1||len(proposed.Steps)>12{
  return CapabilityPlanProposal{},errors.New("model plan exceeds stage limit")
 }
 program:=CapabilityProgram{Project:input.Project,Objective:strings.TrimSpace(input.Objective),PersonalRead:input.PersonalRead,Steps:proposed.Steps}
 // The same step and DAG validators that gate persisted workflows gate proposals.
 steps:=make([]agentflow.Step,0,len(program.Steps))
 for _,stage:=range program.Steps {
  if err:=validCapabilityStage(stage,program.PersonalRead);err!=nil{return CapabilityPlanProposal{},err}
  encoded,_:=json.Marshal(map[string]any{"query":stage.Query,"limit":stage.Limit})
  steps=append(steps,agentflow.Step{ID:stage.ID,Kind:stage.Action,DependsOn:stage.DependsOn,Input:encoded,MaxAttempts:2})
 }
 if err:=agentflow.Validate(agentflow.Workflow{ID:"proposed",Project:program.Project,Steps:steps});err!=nil{return CapabilityPlanProposal{},err}
 return CapabilityPlanProposal{Program:program,RequiresOwnerSubmission:true},nil
}

func (s *Server) capabilityWorkflowPlan(w http.ResponseWriter,r *http.Request) {
 var input CapabilityPlanInput
 if err:=decode(r,&input);err!=nil{writeError(w,400,err.Error());return}
 proposal,err:=s.proposeCapabilityWorkflow(r.Context(),input)
 if err!=nil{
  if s.capabilityPlanner==nil{writeError(w,503,"capability model unavailable");return}
  writeError(w,400,err.Error());return
 }
 writeJSON(w,200,proposal)
}
