package transport

import (
 "net/http"
 "strconv"
 "time"

 "github.com/dbontr/homelab-new/system/calendar"
 "github.com/dbontr/homelab-new/system/store"
)

// The planner is advisory. Its calendar reads are independent of persistence;
// it does not write tasks, modify calendar entries, or reserve compute.
func(s *Server) uiWeekPlan(w http.ResponseWriter,r *http.Request){
 query:=r.URL.Query()
 day:=query.Get("from")
 capacity:=180
 hour:=9
 if raw:=query.Get("daily_minutes");raw!=""{
  value,err:=strconv.Atoi(raw)
  if err!=nil||value<30||value>480{
   writeError(w,400,"daily_minutes must be 30..480");return
  }
  capacity=value
 }
 if raw:=query.Get("start_hour");raw!=""{
  value,err:=strconv.Atoi(raw)
  if err!=nil||value<0||value>16{
   writeError(w,400,"start_hour must be 0..16");return
  }
  hour=value
 }
 first,err:=s.db.PlanningStart(day)
 if err!=nil{writeError(w,400,err.Error());return}
 reserved:=[]store.BusyWindow{}
 limitations:=[]string{}
 if s.calendarURL!=""{
  feed,err:=calendar.Fetch(r.Context(),s.client,s.calendarURL)
  if err!=nil{
   limitations=append(limitations,"Calendar unavailable: the plan uses only recorded task reservations")
  }else{
   expanded,err:=calendar.Expand(feed,first,first.AddDate(0,0,7))
   if err!=nil{
    limitations=append(limitations,"Calendar recurrence could not be expanded")
   }else{
    limitations=append(limitations,expanded.Limitations...)
    for _,event:=range expanded.Occurrences{
     from,errA:=time.Parse(time.RFC3339,event.Start)
     until,errB:=time.Parse(time.RFC3339,event.End)
     if errA!=nil||errB!=nil||!until.After(from){continue}
     reserved=append(reserved,store.BusyWindow{
      Start:from,End:until,Source:"calendar",Title:event.Summary,
     })
    }
   }
  }
 }else{
  limitations=append(limitations,"No read-only calendar is configured")
 }
 plan,err:=s.db.PlanWeek(day,capacity,hour,reserved)
 if err!=nil{writeError(w,400,err.Error());return}
 plan.Limitations=append(plan.Limitations,limitations...)
 writeJSON(w,200,plan)
}
