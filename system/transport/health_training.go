package transport

import (
 "encoding/json"
 "math"
 "net/http"
 "strconv"
 "strings"
 "time"
)

// This owner-scoped training recorder accepts only actual performed values.
// It does not automatically decide that a planned workout was completed.
func (s *Server) uiHealthTrainingLog(w http.ResponseWriter,r *http.Request){
 var in struct{
  PlanID string `json:"plan_id"`
  SessionID string `json:"session_id"`
  Exercise string `json:"exercise"`
  Activity string `json:"activity"`
  SetNumber int `json:"set_number"`
  Repetitions int `json:"repetitions"`
  LoadKG *float64 `json:"load_kg"`
  DurationSeconds *float64 `json:"duration_seconds"`
  DistanceMeters *float64 `json:"distance_meters"`
  RPE *float64 `json:"rpe"`
  Notes string `json:"notes"`
 }
 if err:=decode(r,&in);err!=nil{writeError(w,400,err.Error());return}
 if strings.TrimSpace(in.Exercise)==""||len(in.Exercise)>200||len(in.Notes)>4096||
    len(in.PlanID)>128||len(in.SessionID)>128||
    in.SessionID!=""&&in.PlanID==""||
    in.SetNumber<0||in.SetNumber>1000||
    in.Repetitions<0||in.Repetitions>100000{
  writeError(w,400,"invalid training entry");return
 }
 if in.Activity!="strength"&&in.Activity!="cardio"&&in.Activity!="mobility"{
  writeError(w,400,"unsupported activity");return
 }
 for _,v:=range []*float64{in.LoadKG,in.DurationSeconds,in.DistanceMeters,in.RPE}{
  if v!=nil&&(math.IsNaN(*v)||math.IsInf(*v,0)||*v<0){
   writeError(w,400,"training measurements must be nonnegative and finite");return
  }
 }
 if in.RPE!=nil&&*in.RPE>10{writeError(w,400,"RPE must be between zero and ten");return}
 if in.Activity=="strength"&&in.Repetitions==0&&in.DurationSeconds==nil{
  writeError(w,400,"strength training requires repetitions or duration");return
 }
 if in.Activity=="cardio"&&in.DistanceMeters==nil&&in.DurationSeconds==nil{
  writeError(w,400,"cardio training requires distance or duration");return
 }
 payload:=map[string]any{
  "exercise":strings.TrimSpace(in.Exercise),"activity":in.Activity,
  "set_number":in.SetNumber,"repetitions":in.Repetitions,
  "at":time.Now().UTC().Format(time.RFC3339),"source":"manual",
  "notes":in.Notes,
 }
 if in.PlanID!=""{payload["plan_id"]=in.PlanID}
 if in.SessionID!=""{payload["session_id"]=in.SessionID}
 if in.LoadKG!=nil{payload["load_kg"]=*in.LoadKG}
 if in.DurationSeconds!=nil{payload["duration_seconds"]=*in.DurationSeconds}
 if in.DistanceMeters!=nil{payload["distance_meters"]=*in.DistanceMeters}
 if in.RPE!=nil{payload["rpe"]=*in.RPE}
 raw,err:=json.Marshal(payload)
 if err!=nil{writeError(w,500,"invalid training data");return}
 output:=s.invoke(r.Context(),"health__health_training_set_log",raw,json.RawMessage("1"))
 if failed,_:=output["isError"].(bool);failed{writeError(w,502,"Health rejected the training entry");return}
 writeJSON(w,201,output)
}

func (s *Server) uiHealthTrainingProgress(w http.ResponseWriter,r *http.Request){
 exercise:=strings.TrimSpace(r.URL.Query().Get("exercise"))
 reps,err:=strconv.Atoi(r.URL.Query().Get("repetitions"))
 if exercise==""||len(exercise)>200||err!=nil||reps<1||reps>30{
  writeError(w,400,"exercise and target repetitions are required");return
 }
 raw,err:=json.Marshal(map[string]any{"exercise":exercise,"target_repetitions":reps})
 if err!=nil{writeError(w,500,"invalid training query");return}
 output:=s.invoke(r.Context(),"health__health_progression_preview",raw,json.RawMessage("1"))
 if failed,_:=output["isError"].(bool);failed{writeError(w,502,"training progression query unavailable");return}
 entries,ok:=output["content"].([]any)
 if !ok||len(entries)==0{writeError(w,502,"training service returned no data");return}
 first,ok:=entries[0].(map[string]any);if !ok{writeError(w,502,"invalid training result");return}
 text,ok:=first["text"].(string);if !ok||len(text)>2<<20{writeError(w,502,"invalid training result");return}
 var result any
 if err:=json.Unmarshal([]byte(text),&result);err!=nil{writeError(w,502,"invalid training JSON");return}
 writeJSON(w,200,result)
}
