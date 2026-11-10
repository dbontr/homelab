package transport

import (
 "context"
 "errors"
 "net/http"
 "strconv"
 "strings"
 "time"

 "github.com/dbontr/homelab-new/system/calendar"
 "github.com/dbontr/homelab-new/system/store"
)

// The planner is advisory. External calendar reads do not modify tasks,
// calendar entries, Workspace scheduling, or persistent state.
func (s *Server) planWeek(ctx context.Context,day string,capacity,hour int)(store.WeeklyPlan,error){
 var capacities [7]int
 for i:=range capacities{capacities[i]=capacity}
 return s.planWeekCapacities(ctx,day,capacities,hour)
}

func (s *Server) planWeekCapacities(ctx context.Context,day string,capacities [7]int,hour int)(store.WeeklyPlan,error){
 first,err:=s.db.PlanningStart(day)
 if err!=nil{return store.WeeklyPlan{},err}
 if hour<0||hour>16{
  return store.WeeklyPlan{},errors.New("invalid capacity or starting hour")
 }
 reserved:=[]store.BusyWindow{}
 limitations:=[]string{}
 if s.calendarSource!=nil{
  feed,err:=s.fetchCalendar(ctx)
  if err!=nil{
   limitations=append(limitations,"Calendar unavailable: only recorded task reservations are considered")
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
 plan,err:=s.db.PlanWeekWithCapacities(day,capacities,hour,reserved)
 if err!=nil{return store.WeeklyPlan{},err}
 plan.Limitations=append(plan.Limitations,limitations...)
 return plan,nil
}

func(s *Server) uiWeekPlan(w http.ResponseWriter,r *http.Request){
 query:=r.URL.Query()
 capacity,hour:=180,9
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
 var plan store.WeeklyPlan
 var err error
 if raw:=query.Get("day_minutes");raw!=""{
  parts:=strings.Split(raw,",")
  if len(parts)!=7{writeError(w,400,"day_minutes must contain exactly seven comma-separated values");return}
  var capacities [7]int
  for i,part:=range parts{
   value,parseErr:=strconv.Atoi(strings.TrimSpace(part))
   if parseErr!=nil||value<0||value>480||value>0&&value<30{
    writeError(w,400,"day_minutes values must be zero or 30..480");return
   }
   capacities[i]=value
  }
  plan,err=s.planWeekCapacities(r.Context(),query.Get("from"),capacities,hour)
 }else{
  plan,err=s.planWeek(r.Context(),query.Get("from"),capacity,hour)
 }
 if err!=nil{writeError(w,400,err.Error());return}
 writeJSON(w,200,plan)
}
