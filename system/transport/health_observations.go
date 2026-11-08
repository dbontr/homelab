package transport

import (
 "encoding/json"
 "math"
 "net/http"
 "regexp"
 "strings"
 "time"
)

var metricName = regexp.MustCompile("^[a-z][a-z0-9_]{0,63}$")

// Health observations are user statements; the server never upgrades an
// estimated or reported observation to a measured one.
func (s *Server) uiHealthObservation(w http.ResponseWriter,r *http.Request){
 var in struct{
  Domain string `json:"domain"`
  Metric string `json:"metric"`
  Value float64 `json:"value"`
  Unit string `json:"unit"`
  Method string `json:"method"`
  At string `json:"at,omitempty"`
 }
 if err:=decode(r,&in);err!=nil{writeError(w,400,err.Error());return}
 if !metricName.MatchString(in.Metric)||
    !metricName.MatchString(in.Domain)||
    len(in.Unit)==0||len(in.Unit)>64||strings.TrimSpace(in.Unit)!=in.Unit||
    (in.Method!="measured"&&in.Method!="reported"&&in.Method!="estimated")||
    math.IsNaN(in.Value)||math.IsInf(in.Value,0) {
   writeError(w,400,"invalid observation, unit, or measurement method");return
 }
 at:=time.Now().UTC()
 if in.At!=""{
  parsed,err:=time.Parse(time.RFC3339,in.At)
  if err!=nil||parsed.After(time.Now().Add(24*time.Hour)){
   writeError(w,400,"observation timestamp must be RFC3339 and not in the future");return
  }
  at=parsed
 }
 args:=map[string]any{
  "domain":in.Domain,"metric":in.Metric,"value":in.Value,"unit":in.Unit,
  "method":in.Method,"source":"manual","at":at.Format(time.RFC3339),
 }
 raw,err:=json.Marshal(args);if err!=nil{writeError(w,500,"cannot encode observation");return}
 output:=s.invoke(r.Context(),"health__health_record_add",raw,json.RawMessage("1"))
 if failed,_:=output["isError"].(bool);failed{writeError(w,502,"Health rejected the observation");return}
 writeJSON(w,201,output)
}
