package motivation

import (
	"strings"
)

func supersedeFocus(sessions []FocusSession,now uint64){
	for i:=range sessions {
		session:=&sessions[i]
		if session.Status!="active"{continue}
		session.Status="superseded"
		at:=now
		if at<session.StartedAt{at=session.StartedAt}
		session.EndedAt=&at
		session.Reflection="Goal revised"
	}
}

func(e *Engine)StartFocus(input FocusStart,now uint64)(FocusSession,error){
	e.mu.Lock()
	defer e.mu.Unlock()
	if input.TargetMinutes<10||input.TargetMinutes>240{
		return FocusSession{},InvalidError("focus duration must be between 10 and 240 minutes")
	}
	p:=e.state.Profile
	if p.Identity==""||p.Outcome==""||p.NextStep==""{
		return FocusSession{},ConflictError("define an identity and next step first")
	}
	today:=0
	for _,s:=range e.state.FocusSessions{
		if s.Status=="active"{
			return FocusSession{},ConflictError("an active focus commitment already exists")
		}
		if s.StartedAt/86400==now/86400{today++}
	}
	if today>=12{return FocusSession{},LimitedError("daily focus commitment limit reached")}
	nextID,err:=nextIdentity(e.state.NextFocusID)
	if err!=nil{return FocusSession{},err}
	session:=FocusSession{
		ID:e.state.NextFocusID,GoalID:p.GoalID,Outcome:p.Outcome,
		Step:p.NextStep,TargetMinutes:input.TargetMinutes,
		StartedAt:now,Status:"active",Reflection:"",
	}
	next:=cloneState(e.state)
	next.FocusSessions=append(next.FocusSessions,session)
	if len(next.FocusSessions)>maxHistory{
		next.FocusSessions=next.FocusSessions[1:]
	}
	next.NextFocusID=nextID
	if _,err:=e.commit(next);err!=nil{return FocusSession{},err}
	return session,nil
}

func(e *Engine)FinishFocus(input FocusFinish,now uint64)(Record,error){
	e.mu.Lock()
	defer e.mu.Unlock()
	if len([]rune(input.Reflection))>400||hasControls(input.Reflection){
		return Record{},InvalidError("invalid focus reflection")
	}
	if !input.Completed&&!input.Emergency&&
		len([]rune(strings.TrimSpace(input.Reflection)))<5 {
		return Record{},InvalidError("a short reflection is required to end early")
	}
	next:=cloneState(e.state)
	for i:=range next.FocusSessions{
		session:=&next.FocusSessions[i]
		if session.ID!=input.ID{continue}
		if session.Status!="active"{
			return Record{},ConflictError("focus commitment already closed")
		}
		if input.Completed{session.Status="completed"}else{session.Status="abandoned"}
		ended:=now
		if ended<session.StartedAt{ended=session.StartedAt}
		session.EndedAt=&ended
		session.Reflection=strings.TrimSpace(input.Reflection)
		return e.commit(next)
	}
	return Record{},InvalidError("focus commitment not found")
}
