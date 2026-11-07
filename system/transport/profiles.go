package transport

import (
 "encoding/json"
 "errors"
 "net/http"
 "os"
 "strconv"

 "github.com/dbontr/homelab-new/system/profiles"
)

type profileInput struct {
 Spec profiles.Spec `json:"spec"`
 ExpectedVersion int `json:"expected_version"`
}

func (s *Server) profileRoutes(w http.ResponseWriter, r *http.Request) {
 if s.profiles==nil {writeError(w,http.StatusServiceUnavailable,"compute profiles are not configured");return}
 if r.Method=="GET" && r.PathValue("id")=="" {
   writeJSON(w,http.StatusOK,s.profiles.List());return
 }
 if r.Method=="GET" {
   version:=0
   if value:=r.URL.Query().Get("version");value!="" {
     var err error
     version,err=strconv.Atoi(value)
     if err!=nil || version<0 {writeError(w,400,"invalid profile version");return}
   }
   p,err:=s.profiles.Get(r.PathValue("id"),version)
   if errors.Is(err,os.ErrNotExist){writeError(w,404,"profile not found");return}
   if err!=nil{writeError(w,500,"profile retrieval failed");return}
   writeJSON(w,200,p);return
 }
 var input profileInput
 if err:=decode(r,&input);err!=nil {writeError(w,400,err.Error());return}
 if input.Spec.ID!=r.PathValue("id"){writeError(w,400,"profile identifier does not match URL");return}
 p,err:=s.profiles.Save(input.Spec,input.ExpectedVersion)
 if err!=nil{writeError(w,400,err.Error());return}
 writeJSON(w,http.StatusOK,p)
}

func (s *Server) profileTool(name string,raw json.RawMessage)(any,error) {
 if s.profiles==nil{return nil,errors.New("compute profiles are not configured")}
 switch name {
 case "compute_profile_list": return s.profiles.List(),nil
 case "compute_profile_get":
   var input struct {ID string `json:"id"`; Version int `json:"version"`}
   if err:=json.Unmarshal(raw,&input);err!=nil{return nil,err}
   return s.profiles.Get(input.ID,input.Version)
 case "compute_profile_save":
   var input profileInput
   if err:=json.Unmarshal(raw,&input);err!=nil{return nil,err}
   return s.profiles.Save(input.Spec,input.ExpectedVersion)
 default: return nil,errors.New("unknown profile operation")
 }
}
