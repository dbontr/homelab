package motivation

import (
	"errors"
	"strings"
	"unicode"
)

const (
	maxHistory = 256
	maxCheckins = 512
	maxStateBytes = 2 << 20
	cooldownSeconds uint64 = 25 * 60
)

type Profile struct {
	Revision uint64 `json:"revision"`
	Enabled bool `json:"enabled"`
	ProactiveEnabled bool `json:"proactive_enabled"`
	CadenceMinutes uint16 `json:"cadence_minutes"`
	HoldoutWeekdayUTC *uint8 `json:"holdout_weekday_utc"`
	GoalID string `json:"goal_id"`
	Identity string `json:"identity"`
	Outcome string `json:"outcome"`
	NextStep string `json:"next_step"`
	Intensity string `json:"intensity"`
	DailyLimit uint8 `json:"daily_limit"`
	QuietStartUTCHour uint8 `json:"quiet_start_utc_hour"`
	QuietEndUTCHour uint8 `json:"quiet_end_utc_hour"`
}

type ProfileChange struct {
	ExpectedRevision uint64 `json:"expected_revision"`
	Enabled bool `json:"enabled"`
	ProactiveEnabled bool `json:"proactive_enabled"`
	CadenceMinutes uint16 `json:"cadence_minutes"`
	HoldoutWeekdayUTC *uint8 `json:"holdout_weekday_utc"`
	GoalID string `json:"goal_id"`
	Identity string `json:"identity"`
	Outcome string `json:"outcome"`
	NextStep string `json:"next_step"`
	Intensity string `json:"intensity"`
	DailyLimit uint8 `json:"daily_limit"`
	QuietStartUTCHour uint8 `json:"quiet_start_utc_hour"`
	QuietEndUTCHour uint8 `json:"quiet_end_utc_hour"`
}

type SuggestRequest struct {
	Trigger string `json:"trigger"`
	Question string `json:"question"`
}

type Feedback struct {
	Engagement uint8 `json:"engagement"`
	Progress uint8 `json:"progress"`
	Wellbeing uint8 `json:"wellbeing"`
	Notes string `json:"notes"`
	RecordedAt uint64 `json:"recorded_at"`
}

type FeedbackInput struct {
	InterventionID uint64 `json:"intervention_id"`
	Engagement uint8 `json:"engagement"`
	Progress uint8 `json:"progress"`
	Wellbeing uint8 `json:"wellbeing"`
	Notes string `json:"notes"`
}

type Intervention struct {
	ID uint64 `json:"id"`
	Strategy string `json:"strategy"`
	Trigger string `json:"trigger"`
	Message string `json:"message"`
	CreatedAt uint64 `json:"created_at"`
	Feedback *Feedback `json:"feedback"`
	Cancelled bool `json:"cancelled"`
}

type Checkin struct {
	RecordedAt uint64 `json:"recorded_at"`
	Minutes uint16 `json:"minutes"`
	SelfInitiated bool `json:"self_initiated"`
	Fascination uint8 `json:"fascination"`
	Wellbeing uint8 `json:"wellbeing"`
	InterventionID *uint64 `json:"intervention_id"`
	EvidenceRef string `json:"evidence_ref"`
	Holdout bool `json:"holdout"`
}

type CheckinInput struct {
	Minutes uint16 `json:"minutes"`
	SelfInitiated bool `json:"self_initiated"`
	Fascination uint8 `json:"fascination"`
	Wellbeing uint8 `json:"wellbeing"`
	InterventionID *uint64 `json:"intervention_id"`
	EvidenceRef string `json:"evidence_ref"`
}

type FocusSession struct {
	ID uint64 `json:"id"`
	GoalID string `json:"goal_id"`
	Outcome string `json:"outcome"`
	Step string `json:"step"`
	TargetMinutes uint16 `json:"target_minutes"`
	StartedAt uint64 `json:"started_at"`
	EndedAt *uint64 `json:"ended_at"`
	Status string `json:"status"`
	Reflection string `json:"reflection"`
}

type FocusStart struct { TargetMinutes uint16 `json:"target_minutes"` }
type FocusFinish struct {
	ID uint64 `json:"id"`
	Completed bool `json:"completed"`
	Emergency bool `json:"emergency"`
	Reflection string `json:"reflection"`
}

type Record struct {
	Profile Profile `json:"profile"`
	Interventions []Intervention `json:"interventions"`
	Checkins []Checkin `json:"checkins"`
	FocusSessions []FocusSession `json:"focus_sessions"`
	NextFocusID uint64 `json:"next_focus_id"`
	NextID uint64 `json:"next_id"`
}

type EngagementSummary struct {
	Days uint64 `json:"days"`
	Sessions int `json:"sessions"`
	Minutes uint64 `json:"minutes"`
	SelfInitiatedSessions int `json:"self_initiated_sessions"`
	SelfInitiatedMinutes uint64 `json:"self_initiated_minutes"`
	HoldoutMinutes uint64 `json:"holdout_minutes"`
	HoldoutSelfInitiatedMinutes uint64 `json:"holdout_self_initiated_minutes"`
	MeanFascination float64 `json:"mean_fascination"`
	MeanWellbeing float64 `json:"mean_wellbeing"`
	InterventionsWithFeedback int `json:"interventions_with_feedback"`
	FocusCompleted int `json:"focus_completed"`
	FocusAbandoned int `json:"focus_abandoned"`
	FocusElapsedMinutes uint64 `json:"focus_elapsed_minutes"`
}

type InvalidError string
func (e InvalidError) Error() string { return string(e) }
type ConflictError string
func (e ConflictError) Error() string { return string(e) }
type LimitedError string
func (e LimitedError) Error() string { return string(e) }

func defaultRecord() Record {
	return Record{
		Profile:Profile{CadenceMinutes:240,Intensity:"guided",DailyLimit:4},
		Interventions:[]Intervention{},
		Checkins:[]Checkin{},
		FocusSessions:[]FocusSession{},
		NextFocusID:1,NextID:1,
	}
}

func validProfile(p Profile) error {
	if len([]rune(p.Identity))>240||len([]rune(p.Outcome))>400||
		len([]rune(p.NextStep))>320||len(p.GoalID)>128 {
		return InvalidError("profile text exceeds accepted limits")
	}
	for _,c:=range p.GoalID {
		if !(c>='a'&&c<='z'||c>='A'&&c<='Z'||c>='0'&&c<='9'||c=='-'||c=='_') {
			return InvalidError("invalid goal reference")
		}
	}
	if p.Intensity!="guided"&&p.Intensity!="direct"{
		return InvalidError("invalid intervention intensity")
	}
	if p.DailyLimit==0||p.DailyLimit>8||p.CadenceMinutes<60||p.CadenceMinutes>1440||
		p.HoldoutWeekdayUTC!=nil&&*p.HoldoutWeekdayUTC>6||
		p.QuietStartUTCHour>23||p.QuietEndUTCHour>23 {
		return InvalidError("invalid intervention limits")
	}
	if p.Enabled&&(p.Identity==""||p.Outcome==""||p.NextStep=="") {
		return InvalidError("identity, outcome and next step are required before enabling")
	}
	return nil
}

func validState(s Record) error {
	if err:=validProfile(s.Profile);err!=nil{return err}
	if s.NextID==0||s.NextFocusID==0||len(s.Interventions)>maxHistory||
		len(s.FocusSessions)>maxHistory||len(s.Checkins)>maxCheckins {
		return InvalidError("stored intervention history is invalid")
	}
	var previous uint64
	pending:=0
	for _,event:=range s.Interventions {
		if event.ID<=previous||event.ID>=s.NextID||len([]rune(event.Message))>1600||
			!validStrategy(event.Strategy)||!validTrigger(event.Trigger) {
			return InvalidError("stored intervention sequence is invalid")
		}
		previous=event.ID
		if event.Cancelled&&event.Feedback!=nil{return InvalidError("stored intervention sequence is invalid")}
		if event.Feedback==nil&&!event.Cancelled{pending++}
		if event.Feedback!=nil&&(
			event.Feedback.Engagement>5||event.Feedback.Progress>5||
			event.Feedback.Wellbeing>5||len([]rune(event.Feedback.Notes))>400) {
			return InvalidError("stored intervention history is invalid")
		}
	}
	if pending>1{return InvalidError("stored intervention sequence is invalid")}
	previous=0
	active:=0
	for _,session:=range s.FocusSessions {
		if session.ID<=previous||session.ID>=s.NextFocusID||
			session.TargetMinutes<10||session.TargetMinutes>240||
			len(session.GoalID)>128||len([]rune(session.Outcome))>400||
			len([]rune(session.Step))>320||session.Step==""||
			len([]rune(session.Reflection))>400 {
			return InvalidError("stored focus history is invalid")
		}
		previous=session.ID
		switch session.Status {
		case "active":
			active++
			if session.EndedAt!=nil{return InvalidError("active focus has end timestamp")}
		case "completed","abandoned","superseded":
			if session.EndedAt==nil||*session.EndedAt<session.StartedAt {
				return InvalidError("closed focus has invalid end timestamp")
			}
		default:return InvalidError("invalid focus status")
		}
	}
	if active>1{return InvalidError("concurrent focus sessions are not supported")}
	var lastTime uint64
	for i,c:=range s.Checkins {
		if c.Minutes==0||c.Minutes>720||c.Fascination>5||c.Wellbeing>5||
			len(c.EvidenceRef)>128||(i>0&&c.RecordedAt<lastTime){
			return InvalidError("stored check-ins are invalid")
		}
		for _,char:=range c.EvidenceRef {
			if !(char>='a'&&char<='z'||char>='A'&&char<='Z'||char>='0'&&char<='9'||
				strings.ContainsRune(":/_-",char)) {return InvalidError("stored check-in reference is invalid")}
		}
		if c.InterventionID!=nil&&(*c.InterventionID==0||*c.InterventionID>=s.NextID) {
			return InvalidError("stored check-in intervention reference is invalid")
		}
		lastTime=c.RecordedAt
	}
	return nil
}

func validTrigger(trigger string)bool {
	switch trigger{
	case "session_start","resistance","distraction","uncertainty","milestone":return true
	}
	return false
}
func validStrategy(strategy string)bool {
	switch strategy{
	case "outcome","inquiry","challenge","accountability","reflection","opportunity_cost","commitment":return true
	}
	return false
}
func hasControls(s string)bool {
	for _,c:=range s{if unicode.IsControl(c){return true}}
	return false
}
func nextRevision(n uint64)(uint64,error){
	if n==^uint64(0){return 0,InvalidError("profile revision exhausted")}
	return n+1,nil
}
func nextIdentity(n uint64)(uint64,error){
	if n==^uint64(0){return 0,InvalidError("identity exhausted")}
	return n+1,nil
}
var errNoState=errors.New("motivation state not found")
