package motivation

import (
	"strings"
)

func(e *Engine)Checkin(input CheckinInput,now uint64)(Record,error){
	e.mu.Lock()
	defer e.mu.Unlock()
	holdout:=false
	if day:=e.state.Profile.HoldoutWeekdayUTC;day!=nil&&*day==weekday(now){
		holdout=true
		for _,event:=range e.state.Interventions{
			if event.CreatedAt/86400==now/86400||
				(!event.Cancelled&&event.Feedback==nil&&event.CreatedAt<now){
				holdout=false
				break
			}
		}
	}
	checkin:=Checkin{
		RecordedAt:now,Minutes:input.Minutes,SelfInitiated:input.SelfInitiated,
		Fascination:input.Fascination,Wellbeing:input.Wellbeing,
		InterventionID:input.InterventionID,EvidenceRef:strings.TrimSpace(input.EvidenceRef),
		Holdout:holdout,
	}
	if checkin.Minutes==0||checkin.Minutes>720||
		checkin.Fascination>5||checkin.Wellbeing>5||
		len(checkin.EvidenceRef)>128 {
		return Record{},InvalidError("invalid engagement check-in")
	}
	for _,char:=range checkin.EvidenceRef{
		if !(char>='a'&&char<='z'||char>='A'&&char<='Z'||char>='0'&&char<='9'||
			strings.ContainsRune(":/_-",char)){
			return Record{},InvalidError("invalid engagement check-in")
		}
	}
	if checkin.InterventionID!=nil {
		found:=false
		for _,event:=range e.state.Interventions{
			if event.ID==*checkin.InterventionID&&!event.Cancelled {found=true;break}
		}
		if !found{return Record{},InvalidError("invalid intervention reference")}
	}
	if len(e.state.Checkins)>0&&
		now<e.state.Checkins[len(e.state.Checkins)-1].RecordedAt {
		return Record{},ConflictError("check-in timestamp moved backward")
	}
	today:=0
	for _,event:=range e.state.Checkins{
		if event.RecordedAt/86400==now/86400{today++}
	}
	if today>=8{return Record{},LimitedError("daily check-in limit reached")}
	next:=cloneState(e.state)
	next.Checkins=append(next.Checkins,checkin)
	if len(next.Checkins)>maxCheckins{next.Checkins=next.Checkins[1:]}
	if input.Wellbeing<=1&&next.Profile.Enabled {
		revision,err:=nextRevision(next.Profile.Revision)
		if err!=nil{return Record{},err}
		next.Profile.Revision=revision
		next.Profile.Enabled=false
		next.Profile.ProactiveEnabled=false
		for i:=range next.Interventions{
			if next.Interventions[i].Feedback==nil{next.Interventions[i].Cancelled=true}
		}
	}
	return e.commit(next)
}

func(e *Engine)Metrics(now uint64)EngagementSummary{
	e.mu.Lock()
	defer e.mu.Unlock()
	start:=uint64(0)
	if now>28*86400{start=now-28*86400}
	summary:=EngagementSummary{Days:28}
	for _,c:=range e.state.Checkins{
		if c.RecordedAt<start||c.RecordedAt>now{continue}
		summary.Sessions++
		summary.Minutes+=uint64(c.Minutes)
		summary.MeanFascination+=float64(c.Fascination)
		summary.MeanWellbeing+=float64(c.Wellbeing)
		if c.SelfInitiated{
			summary.SelfInitiatedSessions++
			summary.SelfInitiatedMinutes+=uint64(c.Minutes)
		}
		if c.Holdout{
			summary.HoldoutMinutes+=uint64(c.Minutes)
			if c.SelfInitiated{
				summary.HoldoutSelfInitiatedMinutes+=uint64(c.Minutes)
			}
		}
	}
	if summary.Sessions>0{
		summary.MeanFascination/=float64(summary.Sessions)
		summary.MeanWellbeing/=float64(summary.Sessions)
	}
	for _,event:=range e.state.Interventions{
		f:=event.Feedback
		if f!=nil&&f.RecordedAt>=start&&f.RecordedAt<=now{
			summary.InterventionsWithFeedback++
		}
	}
	for _,session:=range e.state.FocusSessions{
		if session.StartedAt<start||session.StartedAt>now{continue}
		switch session.Status {
		case "completed":summary.FocusCompleted++
		case "abandoned":summary.FocusAbandoned++
		}
		if session.EndedAt!=nil{
			elapsed:=since(*session.EndedAt,session.StartedAt)/60
			if elapsed>uint64(session.TargetMinutes){elapsed=uint64(session.TargetMinutes)}
			summary.FocusElapsedMinutes+=elapsed
		}
	}
	return summary
}
