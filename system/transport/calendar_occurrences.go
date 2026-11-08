package transport

import (
 "net/http"
 "time"

 "github.com/dbontr/homelab-new/system/calendar"
)

// Occurrences present bounded calendar availability without modifying the
// external source. Unsupported rules appear as explicit limitations.
func(s *Server) calendarOccurrences(w http.ResponseWriter,r *http.Request){
 if s.calendarURL==""{writeError(w,503,"calendar is not configured");return}
 from,err1:=time.Parse(time.RFC3339,r.URL.Query().Get("start"))
 until,err2:=time.Parse(time.RFC3339,r.URL.Query().Get("end"))
 if err1!=nil||err2!=nil||!until.After(from)||until.Sub(from)>366*24*time.Hour{
  writeError(w,400,"calendar range must be RFC3339 and within 366 days");return
 }
 feed,err:=calendar.Fetch(r.Context(),s.client,s.calendarURL)
 if err!=nil{writeError(w,502,"calendar feed unavailable");return}
 expanded,err:=calendar.Expand(feed,from,until)
 if err!=nil{writeError(w,422,"calendar range cannot be expanded");return}
 writeJSON(w,200,expanded)
}
