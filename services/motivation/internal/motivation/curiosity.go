package motivation

import (
 "errors"
 "strings"
)

func validCuriosity(c CuriosityPrompt)bool{
 if c.ID==""||len(c.ID)>80||!validReference(c.ID)||
  !validReference(c.SourceID)||c.SourceID==""||
  len([]rune(c.SourceLabel))>320||
  len([]rune(c.Question))>420||len([]rune(c.WorthwhileOutcome))>420||
  len([]rune(c.FirstAction))>320||len([]rune(c.ExpectedEvidence))>420||
  c.Provenance!="owner_declared"&&c.Provenance!="source_verified"{
  return false
 }
 if c.SourceKind!="task"&&c.SourceKind!="learning"&&c.SourceKind!="research"{
  return false
 }
 if c.Question==""||c.WorthwhileOutcome==""||
  c.FirstAction==""||c.ExpectedEvidence==""{
  return false
 }
 for _,field:=range []string{
  c.SourceLabel,c.Question,c.WorthwhileOutcome,c.FirstAction,c.ExpectedEvidence,
 }{if hasControls(field){return false}}
 return true
}

// SaveCuriosity makes the origin and proposed evidence explicit. The
// Motivation writer never asserts task completion or academic mastery.
func (e *Engine)SaveCuriosity(id string,input CuriosityChange)(Record,error){
 id=strings.TrimSpace(id)
 prompt:=CuriosityPrompt{
  ID:id,Enabled:input.Enabled,Paused:input.Paused,
  SourceKind:input.SourceKind,SourceID:input.SourceID,
  SourceLabel:strings.TrimSpace(input.SourceLabel),
  Provenance:"owner_declared",
  Question:strings.TrimSpace(input.Question),
  WorthwhileOutcome:strings.TrimSpace(input.WorthwhileOutcome),
  FirstAction:strings.TrimSpace(input.FirstAction),
  ExpectedEvidence:strings.TrimSpace(input.ExpectedEvidence),
  UpdatedAt:Now(),
 }
 if !validCuriosity(prompt){return Record{},InvalidError("invalid curiosity prompt")}
 e.mu.Lock();defer e.mu.Unlock()
 next:=cloneState(e.state)
 for i:=range next.CuriosityPrompts{
  current:=next.CuriosityPrompts[i]
  if current.ID!=id{continue}
  if current.Revision!=input.ExpectedRevision{
   return Record{},ConflictError("curiosity revision conflict")
  }
  revision,err:=nextRevision(current.Revision)
  if err!=nil{return Record{},err}
  prompt.Revision=revision
  prompt.CreatedAt=current.CreatedAt
  next.CuriosityPrompts[i]=prompt
  return e.commit(next)
 }
 if input.ExpectedRevision!=0{return Record{},ConflictError("curiosity revision conflict")}
 if len(next.CuriosityPrompts)>=64{return Record{},InvalidError("curiosity prompt capacity reached")}
 prompt.Revision=1;prompt.CreatedAt=prompt.UpdatedAt
 next.CuriosityPrompts=append(next.CuriosityPrompts,prompt)
 return e.commit(next)
}

// Curiosity is a deliberately requested question, not a proactive notification.
func(e *Engine)Curiosity(id string)(CuriosityPrompt,error){
 e.mu.Lock();defer e.mu.Unlock()
 if !e.state.Profile.Enabled{return CuriosityPrompt{},ConflictError("motivation interventions are paused")}
 for _,prompt:=range e.state.CuriosityPrompts{
  if prompt.ID==id&&prompt.Enabled&&!prompt.Paused{return prompt,nil}
 }
 return CuriosityPrompt{},errors.New("curiosity prompt is not enabled")
}
