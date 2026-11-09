package transport

import (
 "context"
 "encoding/json"
 "errors"
 "net/http"

 "github.com/dbontr/homelab-new/system/agentflow"
)

func (s *Server) SetEngineering(engine agentflow.Manager){s.engineering=engine}

func (s *Server) engineeringCollection(w http.ResponseWriter,r *http.Request){
 if s.engineering==nil{writeError(w,503,"engineering runs unavailable");return}
 switch r.Method{
 case http.MethodGet:
  runs,err:=s.engineering.List();if err!=nil{writeError(w,500,"engineering runs unavailable");return}
  writeJSON(w,200,map[string]any{"runs":runs})
 case http.MethodPost:
  var p agentflow.Program
  if err:=decode(r,&p);err!=nil{writeError(w,400,err.Error());return}
  created,err:=s.engineering.Submit(p)
  if err!=nil{writeError(w,400,err.Error());return}
  writeJSON(w,201,created)
 }
}
func(s *Server)engineeringRun(w http.ResponseWriter,r *http.Request){
 if s.engineering==nil{writeError(w,503,"engineering runs unavailable");return}
 id:=r.PathValue("id")
 if r.Method==http.MethodGet{
  run,err:=s.engineering.Get(id);if err!=nil{writeError(w,404,"engineering run unavailable");return}
  writeJSON(w,200,run);return
 }
 if r.Method==http.MethodPost {
  var in struct{Revision uint64 `json:"revision"`}
  if err:=decode(r,&in);err!=nil{writeError(w,400,err.Error());return}
  if err:=s.engineering.Cancel(id,in.Revision);err!=nil{writeError(w,409,err.Error());return}
  run,err:=s.engineering.Get(id);if err!=nil{writeError(w,500,"engineering run unavailable");return}
  writeJSON(w,200,run)
 }
}
func (s *Server) engineeringTool(_ context.Context,name string,raw json.RawMessage)(any,error){
 if s.engineering==nil{return nil,errors.New("engineering runs unavailable")}
 switch name{
 case "engineering_run_submit":
  var p agentflow.Program
  if err:=json.Unmarshal(raw,&p);err!=nil{return nil,err}
  return s.engineering.Submit(p)
 case "engineering_run_list":
  return s.engineering.List()
 case "engineering_run_get":
  var request struct{ID string `json:"id"`}
  if err:=json.Unmarshal(raw,&request);err!=nil{return nil,err}
  return s.engineering.Get(request.ID)
 case "engineering_run_cancel":
  var request struct{ID string `json:"id"`;Revision uint64 `json:"revision"`}
  if err:=json.Unmarshal(raw,&request);err!=nil{return nil,err}
  if err:=s.engineering.Cancel(request.ID,request.Revision);err!=nil{return nil,err}
  return s.engineering.Get(request.ID)
 default:return nil,errors.New("unsupported engineering tool")
 }
}
