package transport

import (
 "net/http"
 "strconv"
 "time"

 "github.com/dbontr/homelab-new/system/calendar"
)

type Conflict struct {
 Source string `json:"source"`
 Title string `json:"title"`
 Start string `json:"start"`
 End string `json:"end"`
}
type ConflictPreview struct {
 Start string `json:"start"`
 End string `json:"end"`
 Conflicts []Conflict `json:"conflicts"`
 Limitations []string `json:"limitations"`
}

// A timebox preview is advisory. It never modifies the personal task ledger,
// an external calendar, or the Workspace scheduler.
func (s *Server) taskConflicts(w http.ResponseWriter,r *http.Request){
 raw:=r.URL.Query().Get("start")
 start,err:=time.Parse(time.RFC3339,raw)
 minutes,minuteErr:=strconv.Atoi(r.URL.Query().Get("minutes"))
 if err!=nil||minuteErr!=nil||minutes<1||minutes>1440{
  writeError(w,400,"start must be RFC3339 and minutes must be 1..1440");return
 }
 end:=start.Add(time.Duration(minutes)*time.Minute)
 out:=ConflictPreview{Start:start.Format(time.RFC3339),End:end.Format(time.RFC3339),Conflicts:[]Conflict{},Limitations:[]string{}}
 for _,task:=range s.db.Tasks("open"){
  if task.ID==r.URL.Query().Get("exclude_id"){continue}
  if task.Scheduled==""||task.Minutes<1{continue}
  other,err:=time.Parse(time.RFC3339,task.Scheduled)
  if err!=nil{continue}
  until:=other.Add(time.Duration(task.Minutes)*time.Minute)
  if start.Before(until)&&other.Before(end){
   out.Conflicts=append(out.Conflicts,Conflict{Source:"task",Title:task.Title,Start:other.Format(time.RFC3339),End:until.Format(time.RFC3339)})
  }
 }
 if s.calendarURL==""{
  out.Limitations=append(out.Limitations,"No external calendar configured")
  writeJSON(w,200,out);return
 }
 feed,err:=calendar.Fetch(r.Context(),s.client,s.calendarURL)
 if err!=nil{
  out.Limitations=append(out.Limitations,"External calendar unavailable; only scheduled tasks were compared")
  writeJSON(w,200,out);return
 }
 if feed.RecurrencesUnexpanded{
  out.Limitations=append(out.Limitations,"Recurring calendar events were not expanded into occurrences")
 }
 skipped:=0
 for _,event:=range feed.Events{
  from,errA:=calendarInstant(event.Start,event.TimeZone)
  to,errB:=calendarInstant(event.End,event.TimeZone)
  if errA!=nil||errB!=nil||!to.After(from){skipped++;continue}
  if start.Before(to)&&from.Before(end){
   title:=event.Summary;if title==""{title="Calendar event"}
   out.Conflicts=append(out.Conflicts,Conflict{Source:"calendar",Title:title,Start:from.Format(time.RFC3339),End:to.Format(time.RFC3339)})
  }
 }
 if skipped>0{out.Limitations=append(out.Limitations,strconv.Itoa(skipped)+" calendar events could not be compared")}
 writeJSON(w,200,out)
}

func calendarInstant(value,zone string)(time.Time,error){
 if value==""{return time.Time{},errors.New("event timestamp is missing")}
 if strings.HasSuffix(value,"Z"){
  return time.Parse("20060102T150405Z",value)
 }
 if zone==""{return time.Time{},errors.New("floating calendar event has no timezone")}
 location,err:=time.LoadLocation(zone);if err!=nil{return time.Time{},err}
 if len(value)==8{return time.ParseInLocation("20060102",value,location)}
 return time.ParseInLocation("20060102T150405",value,location)
}
