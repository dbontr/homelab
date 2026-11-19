package motivation

import (
	"fmt"
	"math"
	"strings"
	"time"
)

var strategyOrder=[]string{
	"outcome","inquiry","challenge","accountability","reflection",
	"opportunity_cost","commitment",
}

func Now()uint64{
	now:=time.Now().Unix()
	if now<0{return 0}
	return uint64(now)
}

func(e *Engine)UpdateProfile(change ProfileChange)(Record,error){
	e.mu.Lock()
	defer e.mu.Unlock()
	current:=e.state.Profile
	if change.ExpectedRevision!=current.Revision{
		return Record{},ConflictError("profile revision conflict")
	}
	revision,err:=nextRevision(current.Revision)
	if err!=nil{return Record{},err}
	profile:=Profile{
		Revision:revision,
		Enabled:change.Enabled,
		ProactiveEnabled:change.ProactiveEnabled,
		CadenceMinutes:change.CadenceMinutes,
		HoldoutWeekdayUTC:change.HoldoutWeekdayUTC,
		GoalID:strings.TrimSpace(change.GoalID),
		Identity:strings.TrimSpace(change.Identity),
		Outcome:strings.TrimSpace(change.Outcome),
		NextStep:strings.TrimSpace(change.NextStep),
		Intensity:change.Intensity,
		DailyLimit:change.DailyLimit,
		QuietStartUTCHour:change.QuietStartUTCHour,
		QuietEndUTCHour:change.QuietEndUTCHour,
	}
	if err:=validProfile(profile);err!=nil{return Record{},err}
	next:=cloneState(e.state)
	if current.Identity!=profile.Identity||current.Outcome!=profile.Outcome||
		current.NextStep!=profile.NextStep {
		supersedeFocus(next.FocusSessions,Now())
	}
	next.Profile=profile
	for i:=range next.Interventions{
		if next.Interventions[i].Feedback==nil{next.Interventions[i].Cancelled=true}
	}
	return e.commit(next)
}

func(e *Engine)Pause()(Record,error){
	e.mu.Lock()
	defer e.mu.Unlock()
	next:=cloneState(e.state)
	revision,err:=nextRevision(next.Profile.Revision)
	if err!=nil{return Record{},err}
	next.Profile.Revision=revision
	next.Profile.Enabled=false
	next.Profile.ProactiveEnabled=false
	for i:=range next.Interventions{
		if next.Interventions[i].Feedback==nil{next.Interventions[i].Cancelled=true}
	}
	return e.commit(next)
}

func(e *Engine)Suggest(request SuggestRequest,now uint64)(Intervention,error){
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.suggestLocked(request,now)
}

func(e *Engine)suggestLocked(request SuggestRequest,now uint64)(Intervention,error){
	if request.Trigger==""{request.Trigger="session_start"}
	if !validTrigger(request.Trigger)||len([]rune(request.Question))>320||hasControls(request.Question){
		return Intervention{},InvalidError("invalid research question")
	}
	profile:=e.state.Profile
	if !profile.Enabled{
		return Intervention{},ConflictError("motivation interventions are paused")
	}
	if profile.HoldoutWeekdayUTC!=nil&&*profile.HoldoutWeekdayUTC==weekday(now){
		return Intervention{},LimitedError("intervention-free day is active")
	}
	for i:=len(e.state.Interventions)-1;i>=0;i--{
		event:=e.state.Interventions[i]
		if event.Feedback==nil&&!event.Cancelled{return event,nil}
	}
	hour:=uint8(now/3600%24)
	start,end:=profile.QuietStartUTCHour,profile.QuietEndUTCHour
	quiet:=start<end&&(hour>=start&&hour<end)||
		start>end&&(hour>=start||hour<end)
	if quiet{return Intervention{},LimitedError("interventions are disabled during quiet hours")}
	count:=0
	for _,event:=range e.state.Interventions{
		if event.CreatedAt/86400==now/86400{count++}
	}
	if count>=int(profile.DailyLimit){
		return Intervention{},LimitedError("daily intervention limit reached")
	}
	if len(e.state.Interventions)>0{
		last:=e.state.Interventions[len(e.state.Interventions)-1]
		if since(now,last.CreatedAt)<cooldownSeconds{
			return Intervention{},LimitedError("intervention cooldown is active")
		}
	}
	nextID,err:=nextIdentity(e.state.NextID)
	if err!=nil{return Intervention{},err}
	strategy:=selectStrategy(e.state.Interventions,request.Trigger)
	event:=Intervention{
		ID:e.state.NextID,Strategy:strategy,Trigger:request.Trigger,
		Message:formatMessage(profile,strategy,request.Question),
		CreatedAt:now,Feedback:nil,Cancelled:false,
	}
	next:=cloneState(e.state)
	next.Interventions=append(next.Interventions,event)
	if len(next.Interventions)>maxHistory{next.Interventions=next.Interventions[1:]}
	next.NextID=nextID
	if _,err:=e.commit(next);err!=nil{return Intervention{},err}
	return event,nil
}

func(e *Engine)Feedback(input FeedbackInput,now uint64)(Record,error){
	e.mu.Lock()
	defer e.mu.Unlock()
	if input.Engagement>5||input.Progress>5||input.Wellbeing>5||
		len([]rune(input.Notes))>400 {
		return Record{},InvalidError("feedback values are outside accepted bounds")
	}
	next:=cloneState(e.state)
	for i:=range next.Interventions{
		event:=&next.Interventions[i]
		if event.ID!=input.InterventionID{continue}
		if event.Cancelled||event.Feedback!=nil{
			return Record{},ConflictError("intervention already resolved")
		}
		event.Feedback=&Feedback{
			Engagement:input.Engagement,Progress:input.Progress,
			Wellbeing:input.Wellbeing,Notes:strings.TrimSpace(input.Notes),
			RecordedAt:now,
		}
		if input.Wellbeing<=1 {
			revision,err:=nextRevision(next.Profile.Revision)
			if err!=nil{return Record{},err}
			next.Profile.Revision=revision
			next.Profile.Enabled=false
			next.Profile.ProactiveEnabled=false
		}
		return e.commit(next)
	}
	return Record{},InvalidError("intervention not found")
}

func(e *Engine)Dismiss(id uint64)(Record,error){
	e.mu.Lock()
	defer e.mu.Unlock()
	next:=cloneState(e.state)
	for i:=range next.Interventions{
		event:=&next.Interventions[i]
		if event.ID!=id{continue}
		if event.Cancelled||event.Feedback!=nil{
			return Record{},ConflictError("intervention already resolved")
		}
		event.Cancelled=true
		return e.commit(next)
	}
	return Record{},InvalidError("intervention not found")
}

func(e *Engine)Tick(now uint64)(*Intervention,error){
	e.mu.Lock()
	defer e.mu.Unlock()
	p:=e.state.Profile
	if !p.Enabled||!p.ProactiveEnabled{return nil,nil}
	if p.HoldoutWeekdayUTC!=nil&&*p.HoldoutWeekdayUTC==weekday(now){return nil,nil}
	for _,event:=range e.state.Interventions{
		if event.Feedback==nil&&!event.Cancelled{return nil,nil}
	}
	if len(e.state.Interventions)>0{
		last:=e.state.Interventions[len(e.state.Interventions)-1]
		if since(now,last.CreatedAt)<uint64(p.CadenceMinutes)*60{
			return nil,nil
		}
	}
	event,err:=e.suggestLocked(SuggestRequest{},now)
	if err!=nil {
		if _,limited:=err.(LimitedError);limited{return nil,nil}
		return nil,err
	}
	return &event,nil
}

func weekday(now uint64)uint8{return uint8((now/86400+4)%7)}
func since(now,earlier uint64)uint64{
	if now<earlier{return 0}
	return now-earlier
}

func selectStrategy(events []Intervention,trigger string)string{
	preferred:=map[string]string{
		"session_start":"outcome","resistance":"accountability",
		"distraction":"challenge","uncertainty":"inquiry","milestone":"reflection",
	}[trigger]
	order:=[]string{preferred}
	for _,strategy:=range strategyOrder{
		if strategy!=preferred{order=append(order,strategy)}
	}
	total:=0
	for _,event:=range events{
		if event.Feedback!=nil&&event.Trigger==trigger{total++}
	}
	best:=preferred
	bestValue:=math.Inf(-1)
	for _,strategy:=range order{
		count:=0
		sum:=0.0
		for _,event:=range events{
			if event.Trigger!=trigger||event.Strategy!=strategy||event.Feedback==nil{continue}
			count++
			f:=event.Feedback
			sum+=(float64(f.Progress)*0.50+float64(f.Engagement)*0.25+
				float64(f.Wellbeing)*0.25)/5.0
		}
		if count==0{return strategy}
		score:=sum/float64(count)+math.Sqrt(2*math.Log(float64(total))/float64(count))
		if score>bestValue{bestValue=score;best=strategy}
	}
	return best
}

func formatMessage(p Profile,strategy,question string)string{
	var opening string
	switch strategy{
	case "outcome":
		opening=fmt.Sprintf("Your chosen objective is: %s. What tangible evidence would move you toward that result today?",p.Outcome)
	case "inquiry":
		opening=fmt.Sprintf("What is the most interesting unanswered question behind this outcome: %s? Begin by identifying what you do not yet know.",p.Outcome)
	case "challenge":
		opening=fmt.Sprintf("Give this a serious, uninterrupted 25-minute attempt: %s. Record your reasoning, not merely the answer.",p.NextStep)
	case "accountability":
		opening=fmt.Sprintf("You defined your desired identity as: %s. Compare your present intention with that commitment. What action follows?",p.Identity)
	case "reflection":
		opening=fmt.Sprintf("Your aim is: %s. Choose one concrete result you can establish today, then decide what evidence will count.",p.Outcome)
	case "opportunity_cost":
		opening=fmt.Sprintf("Your chosen outcome is: %s. If you postpone today's next step, which opportunity would you forgo? Name the tradeoff without exaggerating it.",p.Outcome)
	case "commitment":
		opening=fmt.Sprintf("You chose the identity: %s. Is that commitment still yours? If so, act on this next step: %s. Otherwise, deliberately revise the commitment.",p.Identity,p.NextStep)
	}
	focus:=""
	if q:=strings.TrimSpace(question);q!=""{focus=" Your research question is: "+q+"."}
	if p.Intensity=="direct" {
		return fmt.Sprintf("%s%s Your next committed step is: %s. Choose deliberately: begin it, or revise the commitment.",opening,focus,p.NextStep)
	}
	return fmt.Sprintf("%s%s A useful next step is: %s.",opening,focus,p.NextStep)
}
