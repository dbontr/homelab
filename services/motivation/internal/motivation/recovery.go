package motivation

import "strings"

func validRecovery(anchor RecoveryAnchor)bool{
 if !validReference(anchor.GoalID)||!validReference(anchor.TaskID)||
  len(anchor.ArtifactRef)>512||len([]rune(anchor.Draft))>4096||
  len([]rune(anchor.Context))>1200||len([]rune(anchor.NextAction))>500{return false}
 for _,s:=range []string{anchor.ArtifactRef,anchor.Draft,anchor.Context,anchor.NextAction}{
  if strings.ContainsRune(s,'\x00'){return false}
 }
 if anchor.Active&&strings.TrimSpace(anchor.NextAction)==""{return false}
 return true
}

// SaveRecovery keeps a resumable research artifact and action in the same
// private revisioned state as Motivation, without editing the artifact itself.
func(e *Engine)SaveRecovery(change RecoveryChange)(Record,error){
 nextAction:=strings.TrimSpace(change.NextAction)
 anchor:=RecoveryAnchor{
  Active:true,GoalID:change.GoalID,TaskID:change.TaskID,
  ArtifactRef:strings.TrimSpace(change.ArtifactRef),
  Draft:change.Draft,Context:change.Context,
  NextAction:nextAction,UpdatedAt:Now(),
 }
 if !validRecovery(anchor){return Record{},InvalidError("invalid recovery anchor")}
 e.mu.Lock();defer e.mu.Unlock()
 if change.ExpectedRevision!=e.state.Recovery.Revision{
  return Record{},ConflictError("recovery revision conflict")
 }
 revision,err:=nextRevision(change.ExpectedRevision)
 if err!=nil{return Record{},err}
 anchor.Revision=revision
 next:=cloneState(e.state);next.Recovery=anchor
 return e.commit(next)
}

// ClearRecovery is explicit, idempotent only at the matching revision.
func(e *Engine)ClearRecovery(expected uint64)(Record,error){
 e.mu.Lock();defer e.mu.Unlock()
 if expected!=e.state.Recovery.Revision{
  return Record{},ConflictError("recovery revision conflict")
 }
 revision,err:=nextRevision(expected)
 if err!=nil{return Record{},err}
 next:=cloneState(e.state)
 next.Recovery=RecoveryAnchor{Revision:revision,UpdatedAt:Now()}
 return e.commit(next)
}

func(e *Engine)Recovery()RecoveryAnchor{
 e.mu.Lock();defer e.mu.Unlock()
 return e.state.Recovery
}
