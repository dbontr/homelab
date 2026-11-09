package store

import (
 "errors"
 "sort"
 "time"
)

type TaskReviewEntry struct {
 TaskID string `json:"task_id"`
 Title string `json:"title"`
 Category string `json:"category"`
 Due string `json:"due,omitempty"`
 Minutes int `json:"minutes,omitempty"`
 Priority int `json:"priority"`
 DependencyCount int `json:"dependency_count"`
 CreatedAt time.Time `json:"created_at"`
 OpenSince string `json:"open_since,omitempty"`
}
type TaskReview struct {
 At time.Time `json:"at"`
 HorizonDays int `json:"horizon_days"`
 Entries []TaskReviewEntry `json:"entries"`
 Note string `json:"note"`
}

const taskReviewAgeDays = 14

func (s *Store) ReviewTasks(now time.Time,horizonDays int)(TaskReview,error){
 if horizonDays<1||horizonDays>30{
  return TaskReview{},errors.New("deadline review window must be 1 to 30 days")
 }
 if now.IsZero(){now=time.Now()}
 now=now.In(s.zone)
 result:=TaskReview{
  At:now, HorizonDays:horizonDays,Entries:[]TaskReviewEntry{},
  Note:"Deadline, dependency and age signals use recorded task fields. Age since creation does not establish inactivity or neglect.",
 }
 tasks:=s.Tasks("")
 completed:=map[string]bool{}
 for _,task:=range tasks{
  if task.Status=="completed"{completed[task.ID]=true}
 }
 for _,task:=range tasks{
  if task.Status!="open"||task.Decomposed{continue}
  entry:=TaskReviewEntry{
   TaskID:task.ID,Title:task.Title,Due:task.Due,Minutes:task.Minutes,
   Priority:task.Priority,CreatedAt:task.CreatedAt,
  }
  for _,dep:=range task.DependsOn{
   if !completed[dep]{entry.DependencyCount++}
  }
  if task.Due!=""{
   var deadline time.Time
   var err error
   if len(task.Due)==10{
    deadline,err=time.ParseInLocation("2006-01-02",task.Due,s.zone)
    deadline=deadline.AddDate(0,0,1)
   }else{
    deadline,err=time.Parse(time.RFC3339,task.Due)
   }
   if err==nil{
    switch{
    case deadline.Before(now):
     entry.Category="overdue"
    case deadline.Before(now.AddDate(0,0,horizonDays)):
     entry.Category="upcoming"
    }
   }
  }
  if entry.Category==""&&entry.DependencyCount>0{
   entry.Category="blocked"
  }
  if entry.Category==""&&task.Minutes==0{
   entry.Category="needs_estimate"
  }
  if entry.Category==""&&task.Due==""&&task.Scheduled==""&&
   !task.CreatedAt.IsZero()&&
   !task.CreatedAt.In(s.zone).After(now.AddDate(0,0,-taskReviewAgeDays)){
   entry.Category="review"
   entry.OpenSince=task.CreatedAt.In(s.zone).Format("2006-01-02")
  }
  if entry.Category!="" {result.Entries=append(result.Entries,entry)}
 }
 order:=map[string]int{"overdue":0,"upcoming":1,"blocked":2,"needs_estimate":3,"review":4}
 sort.SliceStable(result.Entries,func(i,j int)bool{
  a,b:=result.Entries[i],result.Entries[j]
  if order[a.Category]!=order[b.Category]{return order[a.Category]<order[b.Category]}
  if a.Due!=b.Due{if a.Due==""{return false};if b.Due==""{return true};return a.Due<b.Due}
  if a.Priority!=b.Priority{return a.Priority>b.Priority}
  return a.CreatedAt.Before(b.CreatedAt)
 })
 if len(result.Entries)>500{result.Entries=result.Entries[:500]}
 return result,nil
}
