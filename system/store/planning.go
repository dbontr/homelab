package store

import (
 "errors"
 "sort"
 "time"
)

type BusyWindow struct {
 Start time.Time
 End time.Time
 Source string
 Title string
}
type PlannedTask struct {
 TaskID string `json:"task_id"`
 Title string `json:"title"`
 Date string `json:"date"`
 Start string `json:"start"`
 End string `json:"end"`
 Minutes int `json:"minutes"`
 Priority int `json:"priority"`
 Due string `json:"due,omitempty"`
}
type UnplannedTask struct {
 TaskID string `json:"task_id"`
 Title string `json:"title"`
 Reason string `json:"reason"`
}
type WeeklyPlan struct {
 From string `json:"from"`
 Until string `json:"until"`
 DailyMinutes int `json:"daily_minutes"`
 StartHour int `json:"start_hour"`
 Slots []PlannedTask `json:"slots"`
 Unplanned []UnplannedTask `json:"unplanned"`
 Reservations int `json:"reservations"`
 Limitations []string `json:"limitations"`
}

func overlap(aStart,aEnd,bStart,bEnd time.Time)bool{
 return aStart.Before(bEnd)&&bStart.Before(aEnd)
}

func (s *Store) PlanningStart(day string)(time.Time,error){
 if day==""{day=time.Now().In(s.zone).Format("2006-01-02")}
 first,err:=time.ParseInLocation("2006-01-02",day,s.zone)
 if err!=nil{return time.Time{},errors.New("planning day must be YYYY-MM-DD")}
 return first,nil
}

func (s *Store) PlanWeek(day string,dailyMinutes,startHour int,external []BusyWindow)(WeeklyPlan,error){
 if dailyMinutes<30||dailyMinutes>480||startHour<0||startHour>16{
  return WeeklyPlan{},errors.New("daily capacity must be 30..480 minutes and start hour 0..16")
 }
 first,err:=s.PlanningStart(day)
 if err!=nil{return WeeklyPlan{},err}
 plan:=WeeklyPlan{
  From:first.Format("2006-01-02"),Until:first.AddDate(0,0,7).Format("2006-01-02"),
  DailyMinutes:dailyMinutes,StartHour:startHour,
  Slots:[]PlannedTask{},Unplanned:[]UnplannedTask{},Limitations:[]string{},
 }
 tasks:=s.Tasks("")
 if len(tasks)>2000{
  plan.Limitations=append(plan.Limitations,"Planning is limited to the first 2000 recorded tasks")
  tasks=tasks[:2000]
 }
 completed:=map[string]bool{}
 busy:=make([]BusyWindow,0,len(external)+len(tasks))
 for _,task:=range tasks{
  if task.Status=="completed"{completed[task.ID]=true}
  if task.Status!="open"||task.Scheduled==""||task.Minutes<1{continue}
  start,err:=time.Parse(time.RFC3339,task.Scheduled)
  if err!=nil{continue}
  busy=append(busy,BusyWindow{Start:start,End:start.Add(time.Duration(task.Minutes)*time.Minute),Source:"task",Title:task.Title})
 }
 for _,slot:=range external{
  if slot.End.After(slot.Start){busy=append(busy,slot)}
 }
 plan.Reservations=len(busy)
 sort.Slice(busy,func(i,j int)bool{return busy[i].Start.Before(busy[j].Start)})
 eligible:=[]Task{}
 for _,task:=range tasks {
  if task.Status!="open"||task.Scheduled!=""{continue}
  reason:=""
  switch {
  case task.Minutes<1:reason="Estimated duration is missing"
  case task.Minutes>dailyMinutes:reason="Duration exceeds the daily capacity"
  default:
   for _,dependency:=range task.DependsOn{
    if !completed[dependency]{reason="Waiting for a prerequisite";break}
   }
  }
  if reason!=""{
   plan.Unplanned=append(plan.Unplanned,UnplannedTask{TaskID:task.ID,Title:task.Title,Reason:reason})
  }else{
   eligible=append(eligible,task)
  }
 }
 sort.SliceStable(eligible,func(i,j int)bool{
  a,b:=eligible[i],eligible[j]
  if a.Due!=b.Due{
   if a.Due==""{return false}
   if b.Due==""{return true}
   return a.Due<b.Due
  }
  if a.Priority!=b.Priority{return a.Priority>b.Priority}
  if a.Minutes!=b.Minutes{return a.Minutes<b.Minutes}
  return a.CreatedAt.Before(b.CreatedAt)
 })
 now:=time.Now().In(s.zone)
 used:=make([]int,7)
 for _,task:=range eligible {
  placed:=false
  duration:=time.Duration(task.Minutes)*time.Minute
  for dayIndex:=0;dayIndex<7&&!placed;dayIndex++ {
   if used[dayIndex]+task.Minutes>dailyMinutes{continue}
   date:=first.AddDate(0,0,dayIndex)
   cursor:=time.Date(date.Year(),date.Month(),date.Day(),startHour,0,0,0,s.zone)
   cutoff:=cursor.Add(8*time.Hour)
   if cursor.Before(now){
    if !now.Before(cutoff){continue}
    cursor=now.Truncate(time.Minute).Add(time.Minute)
    cursor=cursor.Add(time.Duration((15-cursor.Minute()%15)%15)*time.Minute)
   }
   if !cursor.Before(cutoff){continue}
   for move:=0;move<100&&cursor.Add(duration).Before(cutoff.Add(time.Nanosecond));move++{
    next:=cursor.Add(duration)
    blocked:=false
    for _,reserved:=range busy {
     if overlap(cursor,next,reserved.Start,reserved.End){
      if reserved.End.After(cursor){cursor=reserved.End}
      blocked=true
      break
     }
    }
    if blocked{continue}
    plan.Slots=append(plan.Slots,PlannedTask{
     TaskID:task.ID,Title:task.Title,Date:date.Format("2006-01-02"),
     Start:cursor.Format(time.RFC3339),End:next.Format(time.RFC3339),
     Minutes:task.Minutes,Priority:task.Priority,Due:task.Due,
    })
    busy=append(busy,BusyWindow{Start:cursor,End:next,Source:"proposal",Title:task.Title})
    used[dayIndex]+=task.Minutes
    placed=true
    break
   }
  }
  if !placed{
   plan.Unplanned=append(plan.Unplanned,UnplannedTask{TaskID:task.ID,
    Title:task.Title,Reason:"No available slot within the requested capacity"})
  }
 }
 sort.Slice(plan.Slots,func(i,j int)bool{return plan.Slots[i].Start<plan.Slots[j].Start})
 return plan,nil
}
