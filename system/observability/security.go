package observability

import (
 "context"
 "strings"
 "time"
)

// SecurityReport reports observed journal events. A missing journal entry
// is not evidence that no incident occurred.
type SecurityReport struct {
 At time.Time `json:"at"`
 Window string `json:"window"`
 Events []SecurityEventCount `json:"events"`
 Truncated bool `json:"truncated"`
 Note string `json:"note"`
}
type SecurityEventCount struct {
 Event string `json:"event"`
 Count int `json:"count"`
 LastSeen time.Time `json:"last_seen,omitempty"`
 Severity string `json:"severity"`
}

var securityEventNames=[]string{
 "owner_authentication denied",
 "web_authentication denied",
 "owner_confirmation denied",
 "web_confirmation denied",
 "owner_session opened",
 "web_session opened",
 "owner_session revoked",
 "web_session revoked",
 "owner_confirmation accepted",
 "web_confirmation accepted",
}

// SecurityAudit counts known security events from the system journal in Loki.
// It never interprets arbitrary log lines as instructions or exports raw logs.
func(c *Client) SecurityAudit(ctx context.Context)(SecurityReport,error){
 lines,err:=c.Logs(ctx,"24h","security audit",200)
 if err!=nil{return SecurityReport{},err}
 report:=SecurityReport{
  At:time.Now().UTC(),Window:"24h",Events:[]SecurityEventCount{},
  Truncated:len(lines)==200,
  Note:"Events are derived from collected system journal entries. Absent or uncollected activity remains unknown.",
 }
 for _,name:=range securityEventNames{
  event:=SecurityEventCount{Event:name,Severity:"info"}
  for _,line:=range lines{
   if !strings.Contains(line.Line,"security audit "+name){continue}
   event.Count++
   if line.At.After(event.LastSeen){event.LastSeen=line.At}
  }
  if strings.Contains(name,"denied")&&event.Count>=3{event.Severity="warning"}
  if event.Count>0{report.Events=append(report.Events,event)}
 }
 return report,nil
}
