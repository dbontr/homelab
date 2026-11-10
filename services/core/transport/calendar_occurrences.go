package transport

import (
 "net/http"
 "time"

 "github.com/dbontr/homelab-new/system/calendar"
)

// Occurrences present bounded calendar availability without modifying the
// external source. Unsupported rules appear as explicit limitations.
func(s *Server) calendarOccurrences(w http.ResponseWriter,r *http.Request){
 if s.calendarSource==nil{writeError(w,503,"calendar is not configured");return}
 fromRaw,untilRaw:=r.URL.Query().Get("start"),r.URL.Query().Get("end")
 if fromRaw==""&&untilRaw==""{
  now:=time.Now().UTC()
  fromRaw=now.Format(time.RFC3339)
  untilRaw=now.AddDate(0,0,30).Format(time.RFC3339)
 }
 from,err1:=time.Parse(time.RFC3339,fromRaw)
 until,err2:=time.Parse(time.RFC3339,untilRaw)
 if err1!=nil||err2!=nil||!until.After(from)||until.Sub(from)>366*24*time.Hour{
  writeError(w,400,"calendar range must be RFC3339 and within 366 days");return
 }
 feed,err:=s.fetchCalendar(r.Context())
 if err!=nil{writeError(w,502,"calendar feed unavailable");return}
 expanded,err:=calendar.Expand(feed,from,until)
 if err!=nil{writeError(w,422,"calendar range cannot be expanded");return}
 writeJSON(w,200,expanded)
}
