package motivation

import (
	"bytes"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
)

func NewHandler(engine *Engine,token string)(http.Handler,error){
	if engine==nil||len(token)<32{return nil,InvalidError("motivation requires a private service token")}
	mux:=http.NewServeMux()
	mux.HandleFunc("GET /healthz",func(w http.ResponseWriter,_ *http.Request){
		writeJSON(w,http.StatusOK,map[string]string{"status":"ok"})
	})
	protected:=http.NewServeMux()
	protected.HandleFunc("GET /v1/motivation/state",func(w http.ResponseWriter,_ *http.Request){
		writeJSON(w,http.StatusOK,engine.Snapshot())
	})
	protected.HandleFunc("PUT /v1/motivation/profile",func(w http.ResponseWriter,r *http.Request){
		input:=ProfileChange{CadenceMinutes:240}
		if !readRequest(w,r,&input,"expected_revision","enabled","identity","outcome",
			"next_step","intensity","daily_limit","quiet_start_utc_hour","quiet_end_utc_hour"){return}
		next,err:=engine.UpdateProfile(input)
		writeResult(w,next,err)
	})
	protected.HandleFunc("POST /v1/motivation/pause",func(w http.ResponseWriter,_ *http.Request){
		next,err:=engine.Pause()
		writeResult(w,next,err)
	})
	protected.HandleFunc("POST /v1/motivation/interventions",func(w http.ResponseWriter,r *http.Request){
		var input SuggestRequest
		if !readRequest(w,r,&input){return}
		next,err:=engine.Suggest(input,Now())
		writeResult(w,next,err)
	})
	protected.HandleFunc("POST /v1/motivation/feedback",func(w http.ResponseWriter,r *http.Request){
		var input FeedbackInput
		if !readRequest(w,r,&input,"intervention_id","engagement","progress","wellbeing"){return}
		next,err:=engine.Feedback(input,Now())
		writeResult(w,next,err)
	})
	protected.HandleFunc("POST /v1/motivation/dismiss",func(w http.ResponseWriter,r *http.Request){
		var input struct{ID uint64 `json:"id"`}
		if !readRequest(w,r,&input,"id"){return}
		next,err:=engine.Dismiss(input.ID)
		writeResult(w,next,err)
	})
	protected.HandleFunc("POST /v1/motivation/checkins",func(w http.ResponseWriter,r *http.Request){
		var input CheckinInput
		if !readRequest(w,r,&input,"minutes","self_initiated","fascination","wellbeing"){return}
		next,err:=engine.Checkin(input,Now())
		writeResult(w,next,err)
	})
	protected.HandleFunc("PUT /v1/motivation/attention-plans/{id}",func(w http.ResponseWriter,r *http.Request){
  var input AttentionPlanChange
  if !readRequest(w,r,&input,"expected_revision","enabled","paused","trigger","context","response","cooldown_minutes"){return}
  next,err:=engine.PutAttentionPlan(r.PathValue("id"),input)
  writeResult(w,next,err)
 })
 protected.HandleFunc("POST /v1/motivation/attention-response",func(w http.ResponseWriter,r *http.Request){
  var input struct{Trigger string `json:"trigger"`; Context string `json:"context"`}
  if !readRequest(w,r,&input,"trigger","context"){return}
  plan,err:=engine.AttentionResponse(input.Trigger,input.Context)
  writeResult(w,plan,err)
 })
 protected.HandleFunc("PUT /v1/motivation/curiosity/{id}",func(w http.ResponseWriter,r *http.Request){
  var input CuriosityChange
  if !readRequest(w,r,&input,"expected_revision","enabled","paused","source_kind","source_id","question","worthwhile_outcome","first_action","expected_evidence"){return}
  next,err:=engine.SaveCuriosity(r.PathValue("id"),input)
  writeResult(w,next,err)
 })
 protected.HandleFunc("GET /v1/motivation/curiosity/{id}",func(w http.ResponseWriter,r *http.Request){
  next,err:=engine.Curiosity(r.PathValue("id"))
  writeResult(w,next,err)
 })
 protected.HandleFunc("GET /v1/motivation/recovery",func(w http.ResponseWriter,_ *http.Request){
  writeJSON(w,http.StatusOK,engine.Recovery())
 })
 protected.HandleFunc("PUT /v1/motivation/recovery",func(w http.ResponseWriter,r *http.Request){
  var input RecoveryChange
  if !readRequest(w,r,&input,"expected_revision","next_action"){return}
  next,err:=engine.SaveRecovery(input)
  writeResult(w,next,err)
 })
 protected.HandleFunc("POST /v1/motivation/recovery/clear",func(w http.ResponseWriter,r *http.Request){
  var input struct{ExpectedRevision uint64 `json:"expected_revision"`}
  if !readRequest(w,r,&input,"expected_revision"){return}
  next,err:=engine.ClearRecovery(input.ExpectedRevision)
  writeResult(w,next,err)
 })
 protected.HandleFunc("GET /v1/motivation/metrics",func(w http.ResponseWriter,_ *http.Request){
		writeJSON(w,http.StatusOK,engine.Metrics(Now()))
	})
	protected.HandleFunc("POST /v1/motivation/focus",func(w http.ResponseWriter,r *http.Request){
		var input FocusStart
		if !readRequest(w,r,&input,"target_minutes"){return}
		next,err:=engine.StartFocus(input,Now())
		writeResult(w,next,err)
	})
	protected.HandleFunc("POST /v1/motivation/focus/end",func(w http.ResponseWriter,r *http.Request){
		var input FocusFinish
		if !readRequest(w,r,&input,"id","completed"){return}
		next,err:=engine.FinishFocus(input,Now())
		writeResult(w,next,err)
	})
	mux.Handle("/",http.HandlerFunc(func(w http.ResponseWriter,r *http.Request){
		expected:="Bearer "+token
		presented:=r.Header.Get("Authorization")
		if len(presented)!=len(expected)||
			subtle.ConstantTimeCompare([]byte(presented),[]byte(expected))!=1{
			writeJSON(w,http.StatusUnauthorized,map[string]string{"error":"authorization required"})
			return
		}
		protected.ServeHTTP(w,r)
	}))
	return mux,nil
}

func readRequest(w http.ResponseWriter,r *http.Request,target any,required ...string)bool{
	if ct:=r.Header.Get("Content-Type");ct!="application/json"&&
		!strings.HasPrefix(ct,"application/json;") {
		writeJSON(w,http.StatusBadRequest,map[string]string{"error":"invalid motivation request"})
		return false
	}
	raw,err:=io.ReadAll(io.LimitReader(r.Body,32<<10+1))
	if err!=nil||len(raw)>32<<10{
		writeJSON(w,http.StatusBadRequest,map[string]string{"error":"invalid motivation request"})
		return false
	}
	var keys map[string]json.RawMessage
	if err:=json.Unmarshal(raw,&keys);err!=nil||keys==nil {
		writeJSON(w,http.StatusBadRequest,map[string]string{"error":"invalid motivation request"})
		return false
	}
	for _,field:=range required{
		value,ok:=keys[field]
		if !ok||string(value)=="null"{
			writeJSON(w,http.StatusBadRequest,map[string]string{"error":"invalid motivation request"})
			return false
		}
	}
	reader:=json.NewDecoder(bytes.NewReader(raw))
	reader.DisallowUnknownFields()
	if err:=reader.Decode(target);err!=nil{
		writeJSON(w,http.StatusBadRequest,map[string]string{"error":"invalid motivation request"})
		return false
	}
	var extra any
	if err:=reader.Decode(&extra);err!=io.EOF{
		writeJSON(w,http.StatusBadRequest,map[string]string{"error":"invalid motivation request"})
		return false
	}
	return true
}

func writeResult(w http.ResponseWriter,value any,err error){
	if err==nil{writeJSON(w,http.StatusOK,value);return}
	var invalid InvalidError
	var conflict ConflictError
	var limited LimitedError
	switch{
	case errors.As(err,&invalid):
		writeJSON(w,http.StatusBadRequest,map[string]string{"error":"invalid motivation request"})
	case errors.As(err,&conflict):
		writeJSON(w,http.StatusConflict,map[string]string{"error":"motivation state conflict"})
	case errors.As(err,&limited):
		writeJSON(w,http.StatusTooManyRequests,map[string]string{"error":"motivation intervention limited"})
	default:
		writeJSON(w,http.StatusServiceUnavailable,map[string]string{"error":"motivation state unavailable"})
	}
}

func writeJSON(w http.ResponseWriter,status int,value any){
	w.Header().Set("Content-Type","application/json")
	w.Header().Set("Cache-Control","no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}
