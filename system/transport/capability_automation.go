package transport

import (
 "context"
 "crypto/rand"
 "encoding/hex"
 "encoding/json"
 "errors"
 "net/http"
 "os"
 "time"

 "github.com/dbontr/homelab-new/system/agentflow"
)

type AutomationRequest struct {
 Program CapabilityProgram `json:"program"`
 EveryMinutes int `json:"every_minutes"`
 Enabled bool `json:"enabled"`
}

func (s *Server) validateAutomationProgram(ctx context.Context,p CapabilityProgram)error{
 if !consoleProjectID.MatchString(p.Project)||len(p.Objective)<1||len(p.Objective)>2000||
  len(p.Steps)<1||len(p.Steps)>12||p.PersonalWrite||p.AutoReplan||p.ShareProjectEvidence{
  return errors.New("scheduled programs must be bounded, read-only and not model-replanned")
 }
 if _,err:=s.projectContext(ctx,p.Project);err!=nil{return err}
 stages:=make([]agentflow.Step,0,len(p.Steps))
 for _,stage:=range p.Steps{
  if err:=validCapabilityStage(stage,p.PersonalRead);err!=nil{return err}
  raw,_:=json.Marshal(map[string]any{"query":stage.Query,"limit":stage.Limit})
  stages=append(stages,agentflow.Step{ID:stage.ID,Kind:stage.Action,
   DependsOn:append([]string(nil),stage.DependsOn...),Input:raw,MaxAttempts:2})
 }
 return agentflow.Validate(agentflow.Workflow{ID:"automation-template",Project:p.Project,
  Objective:p.Objective,CapabilityPlan:true,Steps:stages})
}

func (s *Server) createCapabilityAutomation(ctx context.Context,input AutomationRequest)(agentflow.Automation,error){
 if s.workflows==nil{return agentflow.Automation{},errors.New("workflow store unavailable")}
 if input.EveryMinutes<60||input.EveryMinutes>10080{return agentflow.Automation{},errors.New("automation interval must be between 60 and 10080 minutes")}
 if err:=s.validateAutomationProgram(ctx,input.Program);err!=nil{return agentflow.Automation{},err}
 encoded,err:=json.Marshal(input.Program)
 if err!=nil{return agentflow.Automation{},err}
 var nonce [16]byte
 if _,err:=rand.Read(nonce[:]);err!=nil{return agentflow.Automation{},err}
 return s.workflows.CreateAutomation(agentflow.Automation{
  ID:hex.EncodeToString(nonce[:]),Project:input.Program.Project,
  Program:encoded,EveryMinutes:input.EveryMinutes,Enabled:input.Enabled,
 })
}

func (s *Server) scheduledWorkflow(ctx context.Context,a agentflow.Automation)(agentflow.Workflow,error){
 var p CapabilityProgram
 if err:=json.Unmarshal(a.Program,&p);err!=nil{return agentflow.Workflow{},err}
 if p.Project!=a.Project{return agentflow.Workflow{},errors.New("automation project mismatch")}
 if err:=s.validateAutomationProgram(ctx,p);err!=nil{return agentflow.Workflow{},err}
 w:=agentflow.Workflow{
  ID:agentflow.AutomationRunID(a.ID,a.NextAt),Project:p.Project,Objective:p.Objective,
  AutomationID:a.ID,AutomationDue:a.NextAt,CapabilityPlan:true,
  PersonalRead:p.PersonalRead,Deadline:time.Now().UTC().Add(30*time.Minute),
  Steps:make([]agentflow.Step,0,len(p.Steps)),
 }
 for _,stage:=range p.Steps{
  input,_:=json.Marshal(struct{
   Query string `json:"query,omitempty"`
   Limit int `json:"limit,omitempty"`
  }{stage.Query,stage.Limit})
  w.Steps=append(w.Steps,agentflow.Step{
   ID:stage.ID,DependsOn:append([]string(nil),stage.DependsOn...),
   Kind:stage.Action,Input:input,MaxAttempts:2,
  })
 }
 return w,agentflow.Validate(w)
}

func (s *Server) fireDueAutomations(ctx context.Context)error{
 if s.workflows==nil{return nil}
 due,err:=s.workflows.DueAutomations(time.Now().UTC(),4)
 if err!=nil{return err}
 for _,a:=range due{
  if err:=ctx.Err();err!=nil{return err}
  w,err:=s.scheduledWorkflow(ctx,a)
  if err!=nil{continue} // Permission, owner or dependent service failure remains visible as overdue.
  _,_ = s.workflows.FireAutomation(a,w) // Revision conflicts are recovered in the next scan.
 }
 return nil
}

func (s *Server) automationCollection(w http.ResponseWriter,r *http.Request){
 if s.workflows==nil{writeError(w,503,"automations unavailable");return}
 switch r.Method{
 case http.MethodGet:
  rows,err:=s.workflows.ListAutomations()
  if err!=nil{writeError(w,500,"automation listing unavailable");return}
  writeJSON(w,200,map[string]any{"automations":rows})
 case http.MethodPost:
  var input AutomationRequest
  if err:=decode(r,&input);err!=nil{writeError(w,400,err.Error());return}
  result,err:=s.createCapabilityAutomation(r.Context(),input)
  if err!=nil{writeError(w,400,err.Error());return}
  writeJSON(w,201,result)
 default:writeError(w,405,"method not allowed")
 }
}
func (s *Server) automationItem(w http.ResponseWriter,r *http.Request){
 if s.workflows==nil{writeError(w,503,"automations unavailable");return}
 id:=r.PathValue("id")
 if r.Method==http.MethodPost{
  var in struct{
   Revision uint64 `json:"revision"`
   Enabled bool `json:"enabled"`
  }
  if err:=decode(r,&in);err!=nil{writeError(w,400,err.Error());return}
  result,err:=s.workflows.SetAutomationEnabled(id,in.Revision,in.Enabled)
  if err!=nil{writeError(w,409,"automation revision or identity conflict");return}
  writeJSON(w,200,result)
  return
 }
 result,err:=s.workflows.GetAutomation(id)
 if err!=nil{
  if errors.Is(err,os.ErrNotExist){writeError(w,404,"automation not found");return}
  writeError(w,400,"automation unavailable");return
 }
 writeJSON(w,200,result)
}

func (s *Server) capabilityAutomationTool(ctx context.Context,name string,raw json.RawMessage)(any,error){
 if s.workflows==nil{return nil,errors.New("automations unavailable")}
 switch name{
 case "automation_create":
  var in AutomationRequest
  if err:=json.Unmarshal(raw,&in);err!=nil{return nil,err}
  return s.createCapabilityAutomation(ctx,in)
 case "automation_list":
  return s.workflows.ListAutomations()
 case "automation_get":
  var in struct{ID string `json:"id"`}
  if err:=json.Unmarshal(raw,&in);err!=nil{return nil,err}
  return s.workflows.GetAutomation(in.ID)
 case "automation_set_enabled":
  var in struct{ID string `json:"id"`;Revision uint64 `json:"revision"`;Enabled bool `json:"enabled"`}
  if err:=json.Unmarshal(raw,&in);err!=nil{return nil,err}
  return s.workflows.SetAutomationEnabled(in.ID,in.Revision,in.Enabled)
 }
 return nil,errors.New("unrecognized automation tool")
}
