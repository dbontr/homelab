package motivation

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func fixture(t *testing.T)*Engine{
	t.Helper()
	e,err:=Open(filepath.Join(t.TempDir(),"state.json"))
	if err!=nil{t.Fatal(err)}
	t.Cleanup(func(){if err:=e.Close();err!=nil{t.Error(err)}})
	return e
}

func enabledProfile(revision uint64)ProfileChange{
	return ProfileChange{
		ExpectedRevision:revision,Enabled:true,ProactiveEnabled:true,
		CadenceMinutes:120,GoalID:"science",
		Identity:"Independent researcher",Outcome:"Prove a useful theorem",
		NextStep:"Establish a lemma",Intensity:"direct",DailyLimit:4,
		QuietStartUTCHour:22,QuietEndUTCHour:6,
	}
}

func TestOptInLimitsAndDurableState(t *testing.T){
	e:=fixture(t)
	now:=uint64(10*86400+12*3600)
	if _,err:=e.Suggest(SuggestRequest{},now);err==nil{
		t.Fatal("disabled profile produced a motivation intervention")
	}
	if _,err:=e.UpdateProfile(enabledProfile(1));err==nil{
		t.Fatal("invalid revision accepted")
	}
	changed,err:=e.UpdateProfile(enabledProfile(0))
	if err!=nil||changed.Profile.Revision!=1{t.Fatalf("opt-in: %v",err)}
	if _,err:=e.Suggest(SuggestRequest{},now-13*3600);err==nil{
		t.Fatal("quiet hours were ignored")
	}
	first,err:=e.Suggest(SuggestRequest{Trigger:"uncertainty",Question:"How does it generalize?"},now)
	if err!=nil||first.Strategy!="inquiry"||first.ID!=1{
		t.Fatalf("incorrect first strategy %+v %v",first,err)
	}
	pending,err:=e.Suggest(SuggestRequest{},now+5)
	if err!=nil||pending.ID!=first.ID{t.Fatal("pending intervention was duplicated")}
	if _,err:=e.Dismiss(first.ID);err!=nil{t.Fatal(err)}
	if _,err:=e.Suggest(SuggestRequest{},now+10);err==nil{
		t.Fatal("cooldown after dismissal was bypassed")
	}
	second,err:=e.Suggest(SuggestRequest{},now+1800)
	if err!=nil||second.ID!=2{t.Fatalf("retry after cooldown: %+v %v",second,err)}
	ended,err:=e.Feedback(FeedbackInput{
		InterventionID:second.ID,Engagement:4,Progress:4,Wellbeing:1,
	},now+1900)
	if err!=nil||ended.Profile.Enabled||ended.Profile.ProactiveEnabled {
		t.Fatal("low wellbeing failed to disable intervention delivery")
	}
	if _,err:=e.Tick(now+7200);err!=nil{t.Fatal(err)}
	reopened,err:=Open(filepath.Join(filepath.Dir(e.path),"state.json"))
	if err==nil{reopened.Close();t.Fatal("the exclusive state lease was not honored")}
	if !strings.Contains(err.Error(),"held")&&!strings.Contains(err.Error(),"lock"){
		t.Fatalf("exclusive lease returned unexpected error: %v",err)
	}
}

func TestFocusCommitmentAndGoalRevision(t *testing.T){
	e:=fixture(t)
	configuration:=enabledProfile(0)
	configuration.Enabled=false
	configuration.ProactiveEnabled=false
	if _,err:=e.UpdateProfile(configuration);err!=nil{t.Fatal(err)}
	focus,err:=e.StartFocus(FocusStart{TargetMinutes:45},100000)
	if err!=nil||focus.ID!=1{t.Fatalf("focus: %+v %v",focus,err)}
	if _,err:=e.StartFocus(FocusStart{TargetMinutes:30},100010);err==nil{
		t.Fatal("parallel focus commitments allowed")
	}
	if _,err:=e.FinishFocus(FocusFinish{ID:focus.ID},100020);err==nil{
		t.Fatal("abandoned focus without reflection was allowed")
	}
	ended,err:=e.FinishFocus(FocusFinish{
		ID:focus.ID,Completed:false,Reflection:"I selected another research priority",
	},100400)
	if err!=nil||ended.FocusSessions[0].Status!="abandoned"{t.Fatal(err)}
	next,err:=e.StartFocus(FocusStart{TargetMinutes:60},100500)
	if err!=nil{t.Fatal(err)}
	configuration.ExpectedRevision=1
	configuration.Outcome="Explore a new conjecture"
	revised,err:=e.UpdateProfile(configuration)
	if err!=nil||revised.FocusSessions[1].Status!="superseded"{t.Fatal("goal revision failed to supersede active focus")}
	if _,err:=e.FinishFocus(FocusFinish{ID:next.ID,Completed:true},100800);err==nil{
		t.Fatal("superseded focus was completed twice")
	}
}

func TestCheckinHoldoutsAndWellbeingPause(t *testing.T){
	e:=fixture(t)
	change:=enabledProfile(0)
	day:=weekday(uint64(12*86400))
	change.HoldoutWeekdayUTC=&day
	if _,err:=e.UpdateProfile(change);err!=nil{t.Fatal(err)}
	now:=uint64(12*86400+12*3600)
	if _,err:=e.Suggest(SuggestRequest{},now);err==nil{
		t.Fatal("control weekday received an intervention")
	}
	if event,err:=e.Tick(now);err!=nil||event!=nil {
		t.Fatal("control weekday scheduler issued intervention")
	}
	record,err:=e.Checkin(CheckinInput{
		Minutes:90,SelfInitiated:true,Fascination:5,Wellbeing:4,EvidenceRef:"paper:001",
	},now)
	if err!=nil||!record.Checkins[0].Holdout{t.Fatal("holdout session not recorded")}
	metrics:=e.Metrics(now)
	if metrics.HoldoutMinutes!=90||metrics.HoldoutSelfInitiatedMinutes!=90||
		metrics.MeanFascination!=5{t.Fatalf("incorrect metrics %+v",metrics)}
	for i:=1;i<8;i++{
		if _,err:=e.Checkin(CheckinInput{
			Minutes:30,Fascination:4,Wellbeing:4,
		},now+uint64(i));err!=nil{t.Fatal(err)}
	}
	if _,err:=e.Checkin(CheckinInput{Minutes:30},now+9);err==nil{
		t.Fatal("daily check-in limit exceeded")
	}
	low,err:=e.Checkin(CheckinInput{Minutes:5,Wellbeing:1},now+86400)
	if err!=nil||low.Profile.Enabled{t.Fatal("low wellbeing did not stop proactive delivery")}
}

func TestRustStateReopensWithoutStateLoss(t *testing.T){
	path:=filepath.Join(t.TempDir(),"state.json")
	rustState:=`{
	  "profile":{"revision":1,"enabled":true,"proactive_enabled":false,
	  "cadence_minutes":240,"holdout_weekday_utc":null,
	  "goal_id":"research","identity":"Researcher","outcome":"A theorem",
	  "next_step":"Prove a lemma","intensity":"direct","daily_limit":4,
	  "quiet_start_utc_hour":0,"quiet_end_utc_hour":0},
	  "interventions":[{"id":1,"strategy":"outcome","trigger":"session_start",
	  "message":"Start","created_at":86400,"feedback":{"engagement":3,
	  "progress":4,"wellbeing":5,"notes":"","recorded_at":86440},"cancelled":false}],
	  "checkins":[{"recorded_at":86500,"minutes":20,"self_initiated":true,
	  "fascination":5,"wellbeing":4,"intervention_id":1,
	  "evidence_ref":"research:lemma","holdout":false}],
	  "focus_sessions":[{"id":1,"goal_id":"research","outcome":"A theorem",
	  "step":"Prove a lemma","target_minutes":25,"started_at":86450,
	  "ended_at":86500,"status":"completed","reflection":"Result obtained"}],
	  "next_focus_id":2,"next_id":2
	}`
	if err:=os.WriteFile(path,[]byte(rustState),0600);err!=nil{t.Fatal(err)}
	e,err:=Open(path)
	if err!=nil{t.Fatalf("Rust JSON rejected: %v",err)}
	if e.Snapshot().Checkins[0].InterventionID==nil||e.Snapshot().FocusSessions[0].Status!="completed"{
		t.Fatal("Rust state lost relationships")
	}
	if _,err:=e.Checkin(CheckinInput{Minutes:10,Fascination:4,Wellbeing:4},86600);err!=nil{t.Fatal(err)}
	if err:=e.Close();err!=nil{t.Fatal(err)}
	reopened,err:=Open(path)
	if err!=nil{t.Fatal(err)}
	defer reopened.Close()
	state:=reopened.Snapshot()
	if state.Profile.Revision!=1||len(state.Checkins)!=2||
		state.Interventions[0].Feedback.Progress!=4||state.FocusSessions[0].Status!="completed"{
		t.Fatal("data changed after migration to Go")
	}
}

func TestStrictPrivateHTTPContract(t *testing.T){
	e:=fixture(t)
	handler,err:=NewHandler(e,strings.Repeat("T",48))
	if err!=nil{t.Fatal(err)}
	send:=func(method,path,payload,auth string)*httptest.ResponseRecorder{
		t.Helper()
		req:=httptest.NewRequest(method,path,bytes.NewBufferString(payload))
		req.Header.Set("Content-Type","application/json")
		if auth!=""{req.Header.Set("Authorization",auth)}
		recorder:=httptest.NewRecorder()
		handler.ServeHTTP(recorder,req)
		return recorder
	}
	if reply:=send("GET","/healthz","","");reply.Code!=http.StatusOK{
		t.Fatal("healthz requires bearer")
	}
	if reply:=send("GET","/v1/motivation/state","","");reply.Code!=http.StatusUnauthorized{
		t.Fatal("protected state disclosed without bearer")
	}
	authorized:="Bearer "+strings.Repeat("T",48)
	if reply:=send("PUT","/v1/motivation/profile",`{"enabled":true}`,authorized);reply.Code!=http.StatusBadRequest{
		t.Fatalf("missing revision and required policy admitted: %d",reply.Code)
	}
	payload:=`{"expected_revision":0,"enabled":true,"identity":"Researcher",
	"outcome":"Prove a theorem","next_step":"Investigate a lemma","intensity":"direct",
	"daily_limit":4,"quiet_start_utc_hour":0,"quiet_end_utc_hour":0}`
	reply:=send("PUT","/v1/motivation/profile",payload,authorized)
	if reply.Code!=http.StatusOK{t.Fatalf("profile was rejected: %d %s",reply.Code,reply.Body)}
	var record Record
	if err:=json.Unmarshal(reply.Body.Bytes(),&record);err!=nil{t.Fatal(err)}
	if record.Profile.CadenceMinutes!=240{t.Fatal("default Rust cadence is not preserved")}
	if reply:=send("POST","/v1/motivation/focus",`{"target_minutes":30,"unexpected":true}`,authorized);reply.Code!=http.StatusBadRequest{
		t.Fatal("unknown JSON fields accepted")
	}
	if reply:=send("POST","/v1/motivation/focus",`{"target_minutes":30}`,authorized);reply.Code!=http.StatusOK{
		t.Fatalf("focus was refused: %d",reply.Code)
	}
	if reply:=send("POST","/v1/motivation/focus/end",`{"id":1}`,authorized);reply.Code!=http.StatusBadRequest{
		t.Fatal("missing completion decision accepted")
	}
}

func TestStateSymlinkAndWorldPermissionsDenied(t *testing.T){
	dir:=t.TempDir()
	path:=filepath.Join(dir,"state.json")
	if err:=os.WriteFile(path,[]byte("{}"),0644);err!=nil{t.Fatal(err)}
	if _,err:=Open(path);err==nil{t.Fatal("world-readable state accepted")}
	if err:=os.Chmod(path,0600);err!=nil{t.Fatal(err)}
	if err:=os.Symlink(path,filepath.Join(dir,"alias.json"));err!=nil{t.Fatal(err)}
	if _,err:=Open(filepath.Join(dir,"alias.json"));err==nil{t.Fatal("state symlink accepted")}
}

func TestStateValidationRejectsUnsafeSequence(t *testing.T){
	r:=defaultRecord()
	r.Interventions=[]Intervention{
		{ID:1,Strategy:"outcome",Trigger:"session_start",CreatedAt:4},
		{ID:2,Strategy:"inquiry",Trigger:"uncertainty",CreatedAt:5},
	}
	r.NextID=3
	if err:=validState(r);err==nil{t.Fatal("multiple pending interventions accepted")}
	var conflict ConflictError
	e:=fixture(t)
	if _,err:=e.UpdateProfile(enabledProfile(99));!errors.As(err,&conflict){
		t.Fatalf("wrong conflict kind: %v",err)
	}
}

func TestLegacyInterventionTriggerDefaultsWithoutForgivingInvalidValues(t *testing.T) {
	record:=defaultRecord()
	record.NextID=2
	raw:=`{"profile":{"revision":0,"enabled":false,"cadence_minutes":240,
	 "holdout_weekday_utc":null,"goal_id":"","identity":"","outcome":"",
	 "next_step":"","intensity":"guided","daily_limit":4,
	 "quiet_start_utc_hour":0,"quiet_end_utc_hour":0},
	 "interventions":[{"id":1,"strategy":"outcome","message":"Observe",
	 "created_at":100,"feedback":null}],"next_id":2}`
	if err:=decodeState([]byte(raw),&record);err!=nil{t.Fatal(err)}
	if err:=validState(record);err!=nil{t.Fatal(err)}
	if record.Interventions[0].Trigger!="session_start"{
		t.Fatal("missing legacy trigger did not receive the Rust serde default")
	}
	record=defaultRecord()
	explicit:=strings.Replace(raw,`"strategy":"outcome"`,`"strategy":"outcome","trigger":""`,1)
	if err:=decodeState([]byte(explicit),&record);err!=nil{t.Fatal(err)}
	if err:=validState(record);err==nil{t.Fatal("invalid explicit trigger accepted")}
}
