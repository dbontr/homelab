package store

import (
 "errors"
 "fmt"
 "strings"
 "time"
 "unicode"
)

// ContinuityCheckpoint stores only a deliberately selected working summary.
// The host's complete model context is never available to an ordinary MCP
// service; recorded usage is explicitly reported, not independently metered.
type ContinuityCheckpoint struct {
 Key string `json:"key"`
 ProjectID string `json:"project_id,omitempty"`
 Objective string `json:"objective"`
 Summary string `json:"summary"`
 NextSteps []string `json:"next_steps,omitempty"`
 Evidence []string `json:"evidence,omitempty"`
 ReportedInputTokens int `json:"reported_input_tokens,omitempty"`
 ContextWindowTokens int `json:"context_window_tokens,omitempty"`
 ReservedOutputTokens int `json:"reserved_output_tokens,omitempty"`
 Revision int `json:"revision"`
 UpdatedAt time.Time `json:"updated_at"`
}

type ContinuityInput struct {
 Key string `json:"key"`
 ProjectID string `json:"project_id,omitempty"`
 Objective string `json:"objective"`
 Summary string `json:"summary"`
 NextSteps []string `json:"next_steps,omitempty"`
 Evidence []string `json:"evidence,omitempty"`
 ExpectedRevision int `json:"expected_revision"`
 ReportedInputTokens int `json:"reported_input_tokens,omitempty"`
 ContextWindowTokens int `json:"context_window_tokens,omitempty"`
 ReservedOutputTokens int `json:"reserved_output_tokens,omitempty"`
}

type ContextPressure struct {
 Source string `json:"source"`
 Status string `json:"status"`
 ReportedInputTokens int `json:"reported_input_tokens,omitempty"`
 ContextWindowTokens int `json:"context_window_tokens,omitempty"`
 ReservedOutputTokens int `json:"reserved_output_tokens,omitempty"`
 RemainingTokens int `json:"remaining_tokens,omitempty"`
 UtilizationPercent int `json:"utilization_percent,omitempty"`
 Advisory string `json:"advisory"`
}

func validContinuityKey(key string)bool{
 if len(key)<1||len(key)>96{return false}
 for _,c:=range key{
  if !(c>='a'&&c<='z')&&!(c>='A'&&c<='Z')&&
   !(c>='0'&&c<='9')&&c!='_'&&c!='-'{return false}
 }
 return true
}

func validContinuityText(value string,limit int)bool{
 if len(strings.TrimSpace(value))==0||len(value)>limit{return false}
 for _,c:=range value{
  if c==0||unicode.IsControl(c)&&c!='\n'&&c!='\t'{return false}
 }
 return true
}

func contextPressure(c ContinuityCheckpoint)ContextPressure{
 if c.ContextWindowTokens<=0{
  return ContextPressure{
   Source:"unavailable",Status:"unknown",
   Advisory:"The MCP cannot inspect ChatGPT's full context or exact remaining tokens. Record a checkpoint before a long operation.",
  }
 }
 used:=c.ReportedInputTokens+c.ReservedOutputTokens
 remaining:=c.ContextWindowTokens-used
 if remaining<0{remaining=0}
 percent:=used*100/c.ContextWindowTokens
 if percent>100{percent=100}
 status:="normal"
 advisory:="The context figures are caller-reported and may exclude hidden instructions, reasoning and other host overhead."
 if percent>=85{
  status="handoff_recommended"
  advisory="Prepare a portable checkpoint and continue in a fresh conversation before attempting another large operation. Reported usage is not independently verified."
 }else if percent>=70{
  status="checkpoint_recommended"
  advisory="Record a durable checkpoint while the conversation still has room for reconciliation. Reported usage is not independently verified."
 }
 return ContextPressure{
  Source:"caller_reported_unverified",Status:status,
  ReportedInputTokens:c.ReportedInputTokens,
  ContextWindowTokens:c.ContextWindowTokens,
  ReservedOutputTokens:c.ReservedOutputTokens,
  RemainingTokens:remaining,
  UtilizationPercent:percent,Advisory:advisory,
 }
}

func validateContinuity(in ContinuityInput)error{
 if !validContinuityKey(in.Key)||in.ProjectID!=""&&!validContinuityKey(in.ProjectID)||
  !validContinuityText(in.Objective,1500)||
  !validContinuityText(in.Summary,16000)||
  in.ExpectedRevision<0||len(in.NextSteps)>24||len(in.Evidence)>32{
  return errors.New("invalid continuity checkpoint")
 }
 for _,step:=range in.NextSteps{
  if !validContinuityText(step,800){return errors.New("invalid continuity step")}
 }
 for _,ref:=range in.Evidence{
  if !validContinuityText(ref,500){return errors.New("invalid continuity evidence reference")}
 }
 if in.ReportedInputTokens<0||in.ContextWindowTokens<0||in.ReservedOutputTokens<0{
  return errors.New("negative token usage is invalid")
 }
 if in.ContextWindowTokens==0{
  if in.ReportedInputTokens!=0||in.ReservedOutputTokens!=0{
   return errors.New("token usage requires a reported context window")
  }
 }else if in.ContextWindowTokens<1024||in.ContextWindowTokens>4_000_000||
  in.ReportedInputTokens>in.ContextWindowTokens||
  in.ReservedOutputTokens>in.ContextWindowTokens{
  return errors.New("invalid reported context budget")
 }
 return nil
}

// SaveContinuity provides revision-fenced state continuity without saving
// raw conversation transcripts or making unsupported host-usage claims.
func(s *Store)SaveContinuity(in ContinuityInput)(ContinuityCheckpoint,ContextPressure,error){
 if s==nil{return ContinuityCheckpoint{},ContextPressure{},errors.New("state store unavailable")}
 if err:=validateContinuity(in);err!=nil{return ContinuityCheckpoint{},ContextPressure{},err}
 s.mu.Lock()
 defer s.mu.Unlock()
 previous,exists:=s.data.Continuity[in.Key]
 if !exists&&in.ExpectedRevision!=0||exists&&previous.Revision!=in.ExpectedRevision{
  return ContinuityCheckpoint{},ContextPressure{},errors.New("checkpoint revision conflict")
 }
 if !exists&&len(s.data.Continuity)>=128{
  return ContinuityCheckpoint{},ContextPressure{},errors.New("continuity checkpoint limit reached")
 }
 checkpoint:=ContinuityCheckpoint{
  Key:in.Key,ProjectID:in.ProjectID,Objective:in.Objective,Summary:in.Summary,
  NextSteps:append([]string(nil),in.NextSteps...),
  Evidence:append([]string(nil),in.Evidence...),
  ReportedInputTokens:in.ReportedInputTokens,
  ContextWindowTokens:in.ContextWindowTokens,
  ReservedOutputTokens:in.ReservedOutputTokens,
  Revision:in.ExpectedRevision+1,UpdatedAt:time.Now().UTC(),
 }
 next:=clone(s.data)
 next.Continuity[in.Key]=checkpoint
 if err:=s.save(next);err!=nil{return ContinuityCheckpoint{},ContextPressure{},err}
 return checkpoint,contextPressure(checkpoint),nil
}

func(s *Store)Continuity(key string)(ContinuityCheckpoint,ContextPressure,error){
 if s==nil||!validContinuityKey(key){
  return ContinuityCheckpoint{},ContextPressure{},errors.New("invalid conversation key")
 }
 s.mu.Lock()
 defer s.mu.Unlock()
 checkpoint,exists:=s.data.Continuity[key]
 if !exists{return ContinuityCheckpoint{},ContextPressure{},errors.New("conversation checkpoint not found")}
 checkpoint.NextSteps=append([]string(nil),checkpoint.NextSteps...)
 checkpoint.Evidence=append([]string(nil),checkpoint.Evidence...)
 return checkpoint,contextPressure(checkpoint),nil
}

// ContinuityHandoff is an explicit, portable prompt for a fresh model
// context. It does not create a chat, infer hidden context or claim that
// the checkpoint captures information the caller did not provide.
func(s *Store)ContinuityHandoff(key string)(map[string]any,error){
 checkpoint,pressure,err:=s.Continuity(key)
 if err!=nil{return nil,err}
 var b strings.Builder
 fmt.Fprintf(&b,"Continue this work from the recorded checkpoint. Verify current repository state before modifying files.\n\n")
 fmt.Fprintf(&b,"Conversation key: %s\n",checkpoint.Key)
 if checkpoint.ProjectID!=""{fmt.Fprintf(&b,"Project: %s\n",checkpoint.ProjectID)}
 fmt.Fprintf(&b,"Objective: %s\n\nRecorded working summary (verify against evidence):\n%s\n",
  checkpoint.Objective,checkpoint.Summary)
 if len(checkpoint.NextSteps)>0{
  b.WriteString("\nNext steps:\n")
  for _,step:=range checkpoint.NextSteps{fmt.Fprintf(&b,"- %s\n",step)}
 }
 if len(checkpoint.Evidence)>0{
  b.WriteString("\nReferences to inspect:\n")
  for _,evidence:=range checkpoint.Evidence{fmt.Fprintf(&b,"- %s\n",evidence)}
 }
 fmt.Fprintf(&b,"\nCheckpoint revision: %d\nSaved: %s\n",
  checkpoint.Revision,checkpoint.UpdatedAt.Format(time.RFC3339))
 b.WriteString("Do not assume tests, deployment or approvals occurred unless confirmed by repository evidence.\n")
 return map[string]any{
  "checkpoint":checkpoint,
  "context_pressure":pressure,
  "handoff":b.String(),
  "automatic_chat_transfer":false,
 },nil
}
