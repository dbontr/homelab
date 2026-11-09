package store

import (
 "errors"
 "os"
 "sort"
 "strings"
 "time"
)

// A date-only due value remains actionable through the end of the owner's
// local calendar day, including days with daylight-saving transitions.
func taskDeadline(due string, zone *time.Location) (time.Time, error) {
 if len(due) == len("2006-01-02") {
  day, err := time.ParseInLocation("2006-01-02", due, zone)
  if err != nil { return time.Time{}, err }
  return day.AddDate(0, 0, 1), nil
 }
 return time.Parse(time.RFC3339, due)
}

// Recurrence is deliberately finite and calendar-based. The weekday form
// accepts "weekly:mon" through "weekly:sun".
func validRecurrence(rule string) bool {
 switch rule {case "","daily","weekdays","weekly","monthly":return true}
 if !strings.HasPrefix(rule,"weekly:"){return false}
 switch strings.TrimPrefix(rule,"weekly:") {
 case "mon","tue","wed","thu","fri","sat","sun":return true
 }
 return false
}

func nextRecurringTime(value,rule string,after time.Time,zone *time.Location)(string,error){
 if !validRecurrence(rule)||rule==""{return "",errors.New("invalid recurrence")}
 dateOnly:=len(value)==10
 var current time.Time
 var err error
 if dateOnly {current,err=time.ParseInLocation("2006-01-02",value,zone)}else{current,err=time.Parse(time.RFC3339,value)}
 if err!=nil{return "",err}
 current=current.In(zone)
 // An overdue recurrence advances to the first future occurrence; it does
 // not manufacture a backlog of missed tasks.
 for step:=0;step<50000;step++ {
  switch {
  case rule=="monthly":
   year,month,day:=current.Date()
   first:=time.Date(year,month+1,1,current.Hour(),current.Minute(),current.Second(),current.Nanosecond(),zone)
   end:=first.AddDate(0,1,-1).Day()
   if day>end{day=end}
   current=time.Date(first.Year(),first.Month(),day,current.Hour(),current.Minute(),current.Second(),current.Nanosecond(),zone)
  case rule=="weekly":current=current.AddDate(0,0,7)
  default:current=current.AddDate(0,0,1)
  }
  if rule=="weekdays"&&(current.Weekday()==time.Saturday||current.Weekday()==time.Sunday){continue}
  if strings.HasPrefix(rule,"weekly:"){
   name:=strings.ToLower(current.Weekday().String()[:3])
   if name!=strings.TrimPrefix(rule,"weekly:"){continue}
  }
  if !current.After(after.In(zone)){continue}
  if dateOnly{return current.Format("2006-01-02"),nil}
  return current.Format(time.RFC3339),nil
 }
 return "",errors.New("recurrence has no reachable future occurrence")
}

// Agenda includes unfinished tasks due by the requested day or explicitly
// scheduled on it. Tasks without dates remain visible in the ordinary inbox.
func (s *Store) Agenda(day string)([]Task,error){
 if !validDue(day)||len(day)!=10{return nil,errors.New("agenda day must use YYYY-MM-DD")}
 rows:=s.Tasks("open")
 selected:=make([]Task,0,len(rows))
 for _,task:=range rows {
  if task.Decomposed{continue}
  due:=task.Due
  if len(due)>10 {
   if at,err:=time.Parse(time.RFC3339,due);err==nil {
    due=at.In(s.zone).Format("2006-01-02")
   }else {
    due=""
   }
  }
  scheduled:=task.Scheduled
  if len(scheduled)>=10{if at,err:=time.Parse(time.RFC3339,scheduled);err==nil{scheduled=at.In(s.zone).Format("2006-01-02")}}
  if (due!=""&&due<=day)||scheduled==day{selected=append(selected,task)}
 }
 sort.SliceStable(selected,func(i,j int)bool{
  if selected[i].Scheduled!=selected[j].Scheduled{
   if selected[i].Scheduled==""{return false}
   if selected[j].Scheduled==""{return true}
   return selected[i].Scheduled<selected[j].Scheduled
  }
  return selected[i].Priority>selected[j].Priority
 })
 return selected,nil
}

type TaskChanges struct {
 Title string `json:"title"`
 Notes string `json:"notes"`
 Due string `json:"due"`
 Scheduled string `json:"scheduled"`
 Recurrence string `json:"recurrence"`
 Minutes int `json:"minutes"`
 Energy string `json:"energy"`
 Location string `json:"location"`
 Priority int `json:"priority"`
 ExpectedRevision int `json:"expected_revision"`
}

func (s *Store) UpdateTask(key string,change TaskChanges)(Task,error){
 if strings.TrimSpace(change.Title)==""||len(change.Title)>500||
  !validDue(change.Due)||!validRecurrence(change.Recurrence)||
  change.Recurrence!=""&&change.Due==""||change.Minutes<0||change.Minutes>1440||
  change.Priority<0||change.Priority>5||
  change.Energy!=""&&change.Energy!="low"&&change.Energy!="medium"&&change.Energy!="high"{
  return Task{},errors.New("invalid task update")
 }
 if change.Scheduled!="" {
  if _,err:=time.Parse(time.RFC3339,change.Scheduled);err!=nil{return Task{},errors.New("scheduled must be RFC3339")}
  if change.Minutes<1{return Task{},errors.New("scheduled tasks require a duration")}
 }
 s.mu.Lock();defer s.mu.Unlock()
 old,ok:=s.data.Tasks[key];if !ok{return Task{},os.ErrNotExist}
 if old.Status!="open"||change.ExpectedRevision!=old.Revision{return Task{},ErrTaskRevision}
 old.Title=strings.TrimSpace(change.Title);old.Notes=change.Notes
 old.Due=change.Due;old.Scheduled=change.Scheduled;old.Recurrence=change.Recurrence
 old.Minutes=change.Minutes;old.Energy=change.Energy;old.Location=change.Location;old.Priority=change.Priority
 old.Revision++
 next:=clone(s.data);next.Tasks[key]=old
 if err:=s.save(next);err!=nil{return Task{},err}
 return old,nil
}

var ErrTaskRevision=errors.New("task revision conflict")

// Capture recognizes a small, predictable vocabulary without interpreting
// arbitrary text as a date or transmitting personal tasks to an AI provider.
func (s *Store) Capture(phrase string)(Task,error){
 input,err:=s.ParseCapture(phrase);if err!=nil{return Task{},err}
 return s.AddTask(input)
}

func (s *Store) ParseCapture(phrase string)(TaskInput,error){
 title:=strings.TrimSpace(phrase)
 if title==""||len(title)>500{return TaskInput{},errors.New("task must have a title")}
 input:=TaskInput{}
 lowered:=strings.ToLower(title)
 rules:=[]struct{suffix,rule string}{
  {" every weekday","weekdays"},{" every day","daily"},{" every week","weekly"},{" every month","monthly"},
  {" every monday","weekly:mon"},{" every tuesday","weekly:tue"},{" every wednesday","weekly:wed"},
  {" every thursday","weekly:thu"},{" every friday","weekly:fri"},
  {" every saturday","weekly:sat"},{" every sunday","weekly:sun"},
 }
 for _,candidate:=range rules {
  if strings.HasSuffix(lowered,candidate.suffix){
   title=strings.TrimSpace(title[:len(title)-len(candidate.suffix)])
   input.Recurrence=candidate.rule;break
  }
 }
 lowered=strings.ToLower(title)
 now:=time.Now().In(s.zone)
 switch {
 case strings.HasSuffix(lowered," tomorrow"):
  title=strings.TrimSpace(title[:len(title)-len(" tomorrow")])
  input.Due=now.AddDate(0,0,1).Format("2006-01-02")
 case strings.HasSuffix(lowered," today"):
  title=strings.TrimSpace(title[:len(title)-len(" today")])
  input.Due=now.Format("2006-01-02")
 }
 if input.Recurrence!=""&&input.Due==""{input.Due=now.Format("2006-01-02")}
 input.Title=title
 if title==""{return TaskInput{},errors.New("task must have a title")}
 return input,nil
}
