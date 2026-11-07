package store

import (
 "crypto/rand"
 "encoding/hex"
 "encoding/json"
 "errors"
 "fmt"
 "os"
 "path/filepath"
 "sort"
 "strconv"
 "strings"
 "sync"
 "time"
)

type Task struct {
 ID string `json:"id"`
 Title string `json:"title"`
 Notes string `json:"notes,omitempty"`
 GoalID string `json:"goal_id,omitempty"`
 Links []string `json:"links,omitempty"`
 ParentID string `json:"parent_id,omitempty"`
 DependsOn []string `json:"depends_on,omitempty"`
 Due string `json:"due,omitempty"`
 Minutes int `json:"minutes,omitempty"`
 Energy string `json:"energy,omitempty"`
 Location string `json:"location,omitempty"`
 Priority int `json:"priority,omitempty"`
 Status string `json:"status"`
 CreatedAt time.Time `json:"created_at"`
 CompletedAt *time.Time `json:"completed_at,omitempty"`
}
type TaskInput struct {
 Title string `json:"title"`
 Notes string `json:"notes,omitempty"`
 GoalID string `json:"goal_id,omitempty"`
 Links []string `json:"links,omitempty"`
 ParentID string `json:"parent_id,omitempty"`
 DependsOn []string `json:"depends_on,omitempty"`
 Due string `json:"due,omitempty"`
 Minutes int `json:"minutes,omitempty"`
 Energy string `json:"energy,omitempty"`
 Location string `json:"location,omitempty"`
 Priority int `json:"priority,omitempty"`
}
type Goal struct { ID string `json:"id"`; Title string `json:"title"`; Notes string `json:"notes,omitempty"`; CreatedAt time.Time `json:"created_at"` }
type Rule struct { Key string `json:"key"`; Text string `json:"text"`; Kind string `json:"kind"` }
type Daily struct { Date string `json:"date"`; Completed int `json:"completed"`; MinimumMet bool `json:"minimum_met"` }
type GoalProgress struct { GoalID string `json:"goal_id"`; Title string `json:"title"`; Tasks int `json:"tasks"`; Completed int `json:"completed"` }
type Report struct {
 WeekOf string `json:"week_of"`
 MinimumDailyTasks int `json:"minimum_daily_tasks"`
 TotalCompleted int `json:"total_completed"`
 Days []Daily `json:"days"`
 Goals []GoalProgress `json:"goals"`
 Note string `json:"note"`
}
type snapshot struct { Tasks map[string]Task `json:"tasks"`; Goals map[string]Goal `json:"goals"`; Rules map[string]Rule `json:"rules"` }
type Store struct { mu sync.Mutex; path string; zone *time.Location; data snapshot }

func Open(path, timeZone string) (*Store,error) {
 if path=="" { return nil,errors.New("database path is required") }
 if timeZone=="" {timeZone="America/Chicago"}
 zone,err:=time.LoadLocation(timeZone);if err!=nil{return nil,err}
 if err=os.MkdirAll(filepath.Dir(path),0700);err!=nil{return nil,err}
 s:=&Store{path:path,zone:zone,data:snapshot{Tasks:map[string]Task{},Goals:map[string]Goal{},Rules:map[string]Rule{}}}
 raw,err:=os.ReadFile(path)
 if err!=nil&&!errors.Is(err,os.ErrNotExist){return nil,err}
 if err==nil {
  if e:=json.Unmarshal(raw,&s.data);e!=nil{return nil,fmt.Errorf("decode state: %w",e)}
  if s.data.Tasks==nil{s.data.Tasks=map[string]Task{}}
  if s.data.Goals==nil{s.data.Goals=map[string]Goal{}}
  if s.data.Rules==nil{s.data.Rules=map[string]Rule{}}
 }
 return s,nil
}
func clone(in snapshot) snapshot {
 out:=snapshot{Tasks:make(map[string]Task,len(in.Tasks)),Goals:make(map[string]Goal,len(in.Goals)),Rules:make(map[string]Rule,len(in.Rules))}
 for k,v:=range in.Tasks{out.Tasks[k]=v}
 for k,v:=range in.Goals{out.Goals[k]=v}
 for k,v:=range in.Rules{out.Rules[k]=v}
 return out
}
func (s *Store) save(next snapshot) error {
 raw,e:=json.MarshalIndent(next,"","  ");if e!=nil{return e}
 f,e:=os.CreateTemp(filepath.Dir(s.path),".state-*");if e!=nil{return e}
 defer os.Remove(f.Name())
 if e=f.Chmod(0600);e!=nil{f.Close();return e}
 if _,e=f.Write(raw);e!=nil{f.Close();return e}
 if e=f.Sync();e!=nil{f.Close();return e}
 if e=f.Close();e!=nil{return e}
 if e=os.Rename(f.Name(),s.path);e!=nil{return e}
 if dir,e:=os.Open(filepath.Dir(s.path));e==nil{_ = dir.Sync();_ = dir.Close()}
 s.data=next
 return nil
}
func id()(string,error){var b [12]byte;if _,e:=rand.Read(b[:]);e!=nil{return "",e};return hex.EncodeToString(b[:]),nil}
func validDue(d string)bool{
 if d==""{return true}
 if _,e:=time.Parse(time.RFC3339,d);e==nil{return true}
 _,e:=time.Parse("2006-01-02",d);return e==nil
}
func (s *Store) AddTask(in TaskInput)(Task,error){
 in.Title=strings.TrimSpace(in.Title)
 if in.Title==""||len(in.Title)>500{return Task{},errors.New("title must be 1-500 characters")}
 if in.Minutes<0||in.Minutes>1440{return Task{},errors.New("minutes must be 0-1440")}
 if in.Priority<0||in.Priority>5{return Task{},errors.New("priority must be 0-5")}
 if !validDue(in.Due){return Task{},errors.New("due must be YYYY-MM-DD or RFC3339")}
 if in.Energy!=""&&in.Energy!="low"&&in.Energy!="medium"&&in.Energy!="high"{return Task{},errors.New("invalid energy")}
 s.mu.Lock();defer s.mu.Unlock()
 if in.GoalID!=""{if _,ok:=s.data.Goals[in.GoalID];!ok{return Task{},errors.New("goal does not exist")}}
 if in.ParentID!=""{if _,ok:=s.data.Tasks[in.ParentID];!ok{return Task{},errors.New("parent task does not exist")}}
 if len(in.DependsOn)>32{return Task{},errors.New("too many prerequisites")}
 seen:=map[string]bool{}
 for _,dep:=range in.DependsOn{
  if _,ok:=s.data.Tasks[dep];!ok{return Task{},errors.New("prerequisite task does not exist")}
  if seen[dep]{return Task{},errors.New("duplicate prerequisite task")}
  seen[dep]=true
 }
 key,e:=id();if e!=nil{return Task{},e}
 t:=Task{ID:key,Title:in.Title,Notes:in.Notes,GoalID:in.GoalID,Links:append([]string(nil),in.Links...),ParentID:in.ParentID,DependsOn:append([]string(nil),in.DependsOn...),Due:in.Due,Minutes:in.Minutes,Energy:in.Energy,Location:in.Location,Priority:in.Priority,Status:"open",CreatedAt:time.Now().UTC()}
 next:=clone(s.data);next.Tasks[key]=t
 return t,s.save(next)
}
func (s *Store) CompleteTask(key string)(Task,error){
 s.mu.Lock();defer s.mu.Unlock()
 t,ok:=s.data.Tasks[key];if !ok{return Task{},os.ErrNotExist}
 if t.Status=="completed"{return t,nil}
 now:=time.Now().UTC();t.Status="completed";t.CompletedAt=&now
 next:=clone(s.data);next.Tasks[key]=t
 return t,s.save(next)
}
func (s *Store) Tasks(status string)[]Task{
 s.mu.Lock();defer s.mu.Unlock()
 out:=make([]Task,0,len(s.data.Tasks))
 for _,t:=range s.data.Tasks{if status==""||t.Status==status{t.Links=append([]string(nil),t.Links...);t.DependsOn=append([]string(nil),t.DependsOn...);out=append(out,t)}}
 sort.Slice(out,func(i,j int)bool{return out[i].CreatedAt.After(out[j].CreatedAt)})
 return out
}
func (s *Store) Suggest(minutes int,energy,location,mode string,limit int)[]Task{
 tasks:=s.Tasks("");out:=make([]Task,0,len(tasks))
 completed:=map[string]bool{}
 for _,task:=range tasks{if task.Status=="completed"{completed[task.ID]=true}}
 for _,t:=range tasks{
  if t.Status!="open"{continue}
  blocked:=false
  for _,dependency:=range t.DependsOn{if !completed[dependency]{blocked=true;break}}
  if blocked{continue}
  if minutes>0&&t.Minutes>minutes{continue}
  if energy!=""&&t.Energy!=""&&t.Energy!=energy{continue}
  if location!=""&&t.Location!=""&&t.Location!=location{continue}
  out=append(out,t)
 }
 now:=time.Now().In(s.zone)
 score:=func(t Task)int{
  p:=t.Priority*10
  if t.Due!=""{
   d,e:=time.Parse(time.RFC3339,t.Due)
   if e!=nil{d,_=time.ParseInLocation("2006-01-02",t.Due,s.zone);d=d.Add(24*time.Hour-time.Nanosecond)}
   days:=int(d.Sub(now).Hours()/24)
   if days<0{p+=100}else if days<2{p+=70}else if days<7{p+=30}
  }
  if t.GoalID!=""{p+=8}
  return p
 }
 sort.SliceStable(out,func(i,j int)bool{
  a,b:=out[i],out[j]
  if mode=="quick"{
   if a.Minutes>0&&b.Minutes>0&&a.Minutes!=b.Minutes{return a.Minutes<b.Minutes}
   if a.Minutes>0&&b.Minutes==0{return true}
   if a.Minutes==0&&b.Minutes>0{return false}
  }
  if score(a)!=score(b){return score(a)>score(b)}
  return a.CreatedAt.Before(b.CreatedAt)
 })
 if limit<=0{limit=3};if limit>50{limit=50};if len(out)>limit{out=out[:limit]}
 return out
}
func (s *Store) AddGoal(title,notes string)(Goal,error){
 title=strings.TrimSpace(title);if title==""||len(title)>500{return Goal{},errors.New("goal title must be 1-500 characters")}
 key,e:=id();if e!=nil{return Goal{},e}
 g:=Goal{ID:key,Title:title,Notes:notes,CreatedAt:time.Now().UTC()}
 s.mu.Lock();defer s.mu.Unlock();next:=clone(s.data);next.Goals[key]=g
 return g,s.save(next)
}
func (s *Store) Goals()[]Goal{
 s.mu.Lock();defer s.mu.Unlock();out:=make([]Goal,0,len(s.data.Goals))
 for _,g:=range s.data.Goals{out=append(out,g)}
 sort.Slice(out,func(i,j int)bool{return out[i].CreatedAt.Before(out[j].CreatedAt)})
 return out
}
func (s *Store) SetRule(key,text,kind string)(Rule,error){
 key=strings.TrimSpace(key);text=strings.TrimSpace(text)
 if key==""||len(key)>80||text==""||len(text)>2000{return Rule{},errors.New("invalid rule key or text")}
 if kind!="constraint"&&kind!="preference"{return Rule{},errors.New("rule kind must be constraint or preference")}
 if key=="minimum_daily_tasks"{n,e:=strconv.Atoi(text);if e!=nil||n<0||n>20{return Rule{},errors.New("minimum_daily_tasks must be from 0 to 20")}}
 r:=Rule{Key:key,Text:text,Kind:kind}
 s.mu.Lock();defer s.mu.Unlock();next:=clone(s.data);next.Rules[key]=r
 return r,s.save(next)
}
func (s *Store) Rules()[]Rule{
 s.mu.Lock();defer s.mu.Unlock();out:=make([]Rule,0,len(s.data.Rules))
 for _,r:=range s.data.Rules{out=append(out,r)}
 sort.Slice(out,func(i,j int)bool{return out[i].Key<out[j].Key})
 return out
}
func (s *Store) Report(weekOf string)(Report,error){
 var monday time.Time
 if weekOf==""{monday=time.Now().In(s.zone)}else{
  parsed,e:=time.ParseInLocation("2006-01-02",weekOf,s.zone);if e!=nil{return Report{},errors.New("week_of must be YYYY-MM-DD")};monday=parsed
 }
 weekday:=(int(monday.Weekday())+6)%7
 monday=time.Date(monday.Year(),monday.Month(),monday.Day(),0,0,0,0,s.zone).AddDate(0,0,-weekday)
 s.mu.Lock();defer s.mu.Unlock()
 min:=3
 if r,ok:=s.data.Rules["minimum_daily_tasks"];ok{if n,e:=strconv.Atoi(r.Text);e==nil{min=n}}
 report:=Report{WeekOf:monday.Format("2006-01-02"),MinimumDailyTasks:min,Days:make([]Daily,7),Note:"Includes only recorded task completions; missing records do not establish inactivity."}
 for i:=range report.Days{report.Days[i]=Daily{Date:monday.AddDate(0,0,i).Format("2006-01-02")}}
 for _,t:=range s.data.Tasks{
  if t.CompletedAt==nil{continue}
  date:=t.CompletedAt.In(s.zone).Format("2006-01-02")
  for i:=range report.Days{if date==report.Days[i].Date{report.Days[i].Completed++;report.TotalCompleted++;break}}
 }
 for i:=range report.Days{report.Days[i].MinimumMet=report.Days[i].Completed>=min}
 for _,g:=range s.data.Goals{
  p:=GoalProgress{GoalID:g.ID,Title:g.Title}
  for _,t:=range s.data.Tasks{if t.GoalID==g.ID{p.Tasks++;if t.Status=="completed"{p.Completed++}}}
  report.Goals=append(report.Goals,p)
 }
 sort.Slice(report.Goals,func(i,j int)bool{return report.Goals[i].Title<report.Goals[j].Title})
 return report,nil
}
