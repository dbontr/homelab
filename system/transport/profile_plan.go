package transport

import (
 "context"
 "encoding/json"
 "errors"
 "net/http"
)

type profilePlanInput struct {
 ID string `json:"id"`
 Version int `json:"version"`
 ProjectID string `json:"project_id"`
 NodeID string `json:"node_id,omitempty"`
}

func(s *Server) planProfile(ctx context.Context,raw json.RawMessage)(any,error){
 if s.profiles==nil{return nil,errors.New("compute profiles are not configured")}
 if !s.hasBackend("workspace"){return nil,errors.New("workspace planner is not configured")}
 var input profilePlanInput
 if err:=json.Unmarshal(raw,&input);err!=nil{return nil,err}
 if input.ProjectID==""||input.ID==""{return nil,errors.New("profile and project are required")}
 profile,err:=s.profiles.Get(input.ID,input.Version);if err!=nil{return nil,err}
 req:=map[string]any{
   "project_id":input.ProjectID,
   "node_id":input.NodeID,
   "architecture":profile.Requires.Architecture,
   "cpu":profile.Requires.CPU,
   "memory_mb":profile.Requires.MemoryMB,
   "hardware":profile.Requires.Hardware,
 }
 if profile.Requires.CPU==0 {req["cpu"]=1}
 if profile.Requires.MemoryMB==0 {req["memory_mb"]=512}
 if profile.Environment!=""{req["environment"]=profile.Environment}
 encoded,err:=json.Marshal(req);if err!=nil{return nil,err}
 outcome:=s.invoke(ctx,"workspace__resource_plan",encoded,json.RawMessage("1"))
 if failed,_:=outcome["isError"].(bool);failed{return nil,errors.New("workspace resource planning was rejected")}
 parts,ok:=outcome["content"].([]any)
 if !ok||len(parts)==0{return nil,errors.New("missing workspace resource plan")}
 item,ok:=parts[0].(map[string]any)
 if !ok{return nil,errors.New("invalid workspace resource plan")}
 text,ok:=item["text"].(string)
 if !ok||len(text)>2<<20{return nil,errors.New("invalid workspace resource plan")}
 var plan any
 if err:=json.Unmarshal([]byte(text),&plan);err!=nil{return nil,errors.New("workspace resource plan is not JSON")}
 return map[string]any{
   "profile_id":profile.ID,
   "version":profile.Version,
   "resource_plan":plan,
   "software_ids":profile.SoftwareIDs,
   "advisory":"Resource planning does not validate software installations or reserve workers.",
 },nil
}

func(s *Server) profilePlanRoute(w http.ResponseWriter,r *http.Request){
 var input profilePlanInput
 if err:=decode(r,&input);err!=nil{writeError(w,400,err.Error());return}
 input.ID=r.PathValue("id")
 raw,_:=json.Marshal(input)
 result,err:=s.planProfile(r.Context(),raw)
 if err!=nil{writeError(w,503,err.Error());return}
 writeJSON(w,200,result)
}

// Profiles remain declarative. The Workspace backend owns actual scheduling,
// image admission, hardware leases and execution authorization.
