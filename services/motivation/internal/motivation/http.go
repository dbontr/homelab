package motivation

import (
	"crypto/subtle"
	"encoding/json"
	"errors"
	"io"
	"net/http"
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
		var input ProfileChange
		if !readRequest(w,r,&input){return}
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
		if !readRequest(w,r,&input){return}
		next,err:=engine.Feedback(input,Now())
		writeResult(w,next,err)
	})
	protected.HandleFunc("POST /v1/motivation/dismiss",func(w http.ResponseWriter,r *http.Request){
		var input struct{ID uint64 `json:"id"`}
		if !readRequest(w,r,&input){return}
		next,err:=engine.Dismiss(input.ID)
		writeResult(w,next,err)
	})
	protected.HandleFunc("POST /v1/motivation/checkins",func(w http.ResponseWriter,r *http.Request){
		var input CheckinInput
		if !readRequest(w,r,&input){return}
		next,err:=engine.Checkin(input,Now())
		writeResult(w,next,err)
	})
	protected.HandleFunc("GET /v1/motivation/metrics",func(w http.ResponseWriter,_ *http.Request){
		writeJSON(w,http.StatusOK,engine.Metrics(Now()))
	})
	protected.HandleFunc("POST /v1/motivation/focus",func(w http.ResponseWriter,r *http.Request){
		var input FocusStart
		if !readRequest(w,r,&input){return}
		next,err:=engine.StartFocus(input,Now())
		writeResult(w,next,err)
	})
	protected.HandleFunc("POST /v1/motivation/focus/end",func(w http.ResponseWriter,r *http.Request){
		var input FocusFinish
		if !readRequest(w,r,&input){return}
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

func readRequest(w http.ResponseWriter,r *http.Request,target any)bool{
	reader:=json.NewDecoder(io.LimitReader(r.Body,32<<10+1))
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
