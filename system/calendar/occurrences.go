package calendar

import (
 "errors"
 "fmt"
 "sort"
 "strconv"
 "strings"
 "time"
)

const maxCalendarOccurrences = 1000

type Occurrence struct {
 UID string `json:"uid"`
 Summary string `json:"summary"`
 Start string `json:"start"`
 End string `json:"end"`
 Location string `json:"location,omitempty"`
 AllDay bool `json:"all_day"`
}

type Expansion struct {
 Start string `json:"start"`
 End string `json:"end"`
 Occurrences []Occurrence `json:"occurrences"`
 Limitations []string `json:"limitations"`
}

func ParseInstant(value, zone string, fallback *time.Location)(time.Time,error){
 if value==""{return time.Time{},errors.New("calendar timestamp is missing")}
 if strings.HasSuffix(value,"Z") {
  if len(value)==16{return time.Parse("20060102T150405Z",value)}
  return time.Time{},errors.New("unsupported UTC timestamp")
 }
 loc:=fallback
 if zone!=""{
  var err error
  loc,err=time.LoadLocation(zone)
  if err!=nil{return time.Time{},errors.New("unknown calendar timezone")}
 }
 if loc==nil{loc=time.UTC}
 if len(value)==8{return time.ParseInLocation("20060102",value,loc)}
 if len(value)==15{return time.ParseInLocation("20060102T150405",value,loc)}
 return time.Time{},errors.New("unsupported calendar timestamp")
}

type recurrenceRule struct {
 freq string
 interval int
 count int
 until time.Time
 untilDefined bool
 weekStart time.Weekday
 weekdays map[time.Weekday]bool
 months map[time.Month]bool
 monthDays map[int]bool
}

var weekdayNames=map[string]time.Weekday{
 "SU":time.Sunday,"MO":time.Monday,"TU":time.Tuesday,
 "WE":time.Wednesday,"TH":time.Thursday,"FR":time.Friday,"SA":time.Saturday,
}

func parseRule(raw,zone string,fallback *time.Location)(recurrenceRule,error){
 r:=recurrenceRule{interval:1,weekStart:time.Monday}
 if raw==""{return r,nil}
 for _,part:=range strings.Split(raw,";"){
  fields:=strings.SplitN(part,"=",2)
  if len(fields)!=2{return r,errors.New("malformed calendar recurrence")}
  switch strings.ToUpper(fields[0]){
  case "FREQ":
   r.freq=fields[1]
   if r.freq!="DAILY"&&r.freq!="WEEKLY"&&r.freq!="MONTHLY"&&r.freq!="YEARLY"{
    return r,errors.New("calendar recurrence frequency is unsupported")
   }
  case "INTERVAL":
   n,err:=strconv.Atoi(fields[1])
   if err!=nil||n<1||n>366{return r,errors.New("invalid recurrence interval")}
   r.interval=n
  case "COUNT":
   n,err:=strconv.Atoi(fields[1])
   if err!=nil||n<1||n>100000{return r,errors.New("invalid recurrence count")}
   r.count=n
  case "UNTIL":
   until,err:=ParseInstant(fields[1],zone,fallback)
   if err!=nil{return r,err}
   if len(fields[1])==8{until=until.AddDate(0,0,1).Add(-time.Nanosecond)}
   r.until=until
   r.untilDefined=true
  case "WKST":
   day,ok:=weekdayNames[fields[1]]
   if !ok{return r,errors.New("unsupported recurrence week start")}
   r.weekStart=day
  case "BYDAY":
   r.weekdays=map[time.Weekday]bool{}
   for _,name:=range strings.Split(fields[1],","){
    day,ok:=weekdayNames[name]
    if !ok{return r,errors.New("ordinal recurrence weekdays are unsupported")}
    r.weekdays[day]=true
   }
  case "BYMONTH":
   r.months=map[time.Month]bool{}
   for _,value:=range strings.Split(fields[1],","){
    n,err:=strconv.Atoi(value)
    if err!=nil||n<1||n>12{return r,errors.New("invalid recurrence month")}
    r.months[time.Month(n)]=true
   }
  case "BYMONTHDAY":
   r.monthDays=map[int]bool{}
   for _,value:=range strings.Split(fields[1],","){
    n,err:=strconv.Atoi(value)
    if err!=nil||n==0||n < -31||n>31{return r,errors.New("invalid recurrence month day")}
    r.monthDays[n]=true
   }
  default:
   return r,fmt.Errorf("unsupported recurrence field %s",fields[0])
  }
 }
 if r.freq==""{return r,errors.New("recurrence frequency required")}
 if r.count>0&&r.untilDefined{return r,errors.New("COUNT and UNTIL cannot appear together")}
 return r,nil
}

func weekdayOffset(day,weekStart time.Weekday)int{
 return (int(day)-int(weekStart)+7)%7
}

func matchesRule(rule recurrenceRule,origin,day time.Time,dayIndex int)bool{
 if len(rule.months)>0&&!rule.months[day.Month()]{return false}
 if len(rule.weekdays)>0&&!rule.weekdays[day.Weekday()]{return false}
 monthDays:=rule.monthDays
 if len(monthDays)>0{
  last:=time.Date(day.Year(),day.Month()+1,0,0,0,0,0,day.Location()).Day()
  if !monthDays[day.Day()]&&!monthDays[day.Day()-last-1]{return false}
 }
 switch rule.freq{
 case "DAILY":
  return dayIndex%rule.interval==0
 case "WEEKLY":
  week:=(dayIndex+weekdayOffset(origin.Weekday(),rule.weekStart))/7
  if week%rule.interval!=0{return false}
  if len(rule.weekdays)==0{return day.Weekday()==origin.Weekday()}
  return true
 case "MONTHLY":
  months:=(day.Year()-origin.Year())*12+int(day.Month()-origin.Month())
  if months%rule.interval!=0{return false}
  if len(monthDays)==0&&len(rule.weekdays)==0{return day.Day()==origin.Day()}
  return true
 case "YEARLY":
  if (day.Year()-origin.Year())%rule.interval!=0{return false}
  if len(rule.months)==0&&day.Month()!=origin.Month(){return false}
  if len(monthDays)==0&&len(rule.weekdays)==0{return day.Day()==origin.Day()}
  return true
 }
 return false
}

func exclusionSet(event Event, loc *time.Location)(map[string]bool,error){
 excludes:=map[string]bool{}
 for _,value:=range event.Excluded{
  instant,err:=ParseInstant(value,event.TimeZone,loc)
  if err!=nil{return nil,err}
  excludes[instant.UTC().Format(time.RFC3339Nano)]=true
 }
 return excludes,nil
}

func asOccurrence(event Event,instant time.Time,length time.Duration)Occurrence{
 end:=instant.Add(length)
 return Occurrence{
  UID:event.UID,Summary:event.Summary,Location:event.Location,
  Start:instant.Format(time.RFC3339),End:end.Format(time.RFC3339),
  AllDay:len(event.Start)==8,
 }
}

func Expand(feed Feed,start,end time.Time)(Expansion,error){
 if !end.After(start)||end.Sub(start)>366*24*time.Hour{
  return Expansion{},errors.New("calendar range must be positive and no more than 366 days")
 }
 out:=Expansion{
  Start:start.Format(time.RFC3339),End:end.Format(time.RFC3339),
  Occurrences:[]Occurrence{},Limitations:[]string{},
 }
 seen:=map[string]bool{}
 overrides:=map[string]map[string]bool{}
 for _,event:=range feed.Events{
  if event.RecurrenceID==""||event.UID==""{continue}
  original,err:=ParseInstant(event.RecurrenceID,event.TimeZone,start.Location())
  if err!=nil{out.Limitations=append(out.Limitations,"Calendar override has an unsupported date");continue}
  if overrides[event.UID]==nil{overrides[event.UID]=map[string]bool{}}
  overrides[event.UID][original.UTC().Format(time.RFC3339Nano)]=true
 }
 appendOccurrence:=func(event Event,at time.Time,length time.Duration){
  if event.Status=="CANCELLED"||!at.Before(end)||!at.Add(length).After(start){return}
  key:=event.UID+"|"+at.UTC().Format(time.RFC3339Nano)
  if seen[key]{return}
  seen[key]=true
  if len(out.Occurrences)<maxCalendarOccurrences{
   out.Occurrences=append(out.Occurrences,asOccurrence(event,at,length))
  }
 }
 for _,event:=range feed.Events{
  if event.Status=="CANCELLED"&&event.RecurrenceID==""{continue}
  first,err:=ParseInstant(event.Start,event.TimeZone,start.Location())
  if err!=nil{out.Limitations=append(out.Limitations,"Calendar event has an unsupported start time");continue}
  last,err:=ParseInstant(event.End,event.TimeZone,start.Location())
  if err!=nil||!last.After(first){
   out.Limitations=append(out.Limitations,"Calendar event has an unsupported duration");continue
  }
  duration:=last.Sub(first)
  if event.RecurrenceID!=""{
   appendOccurrence(event,first,duration)
   continue
  }
  excluded,err:=exclusionSet(event,start.Location())
  if err!=nil{out.Limitations=append(out.Limitations,"Calendar recurrence exclusion has an unsupported date");continue}
  emit:=func(at time.Time){
   key:=at.UTC().Format(time.RFC3339Nano)
   if excluded[key]||overrides[event.UID][key]{return}
   appendOccurrence(event,at,duration)
  }
  if event.Recurrence==""{
   emit(first)
  }else{
   rule,err:=parseRule(event.Recurrence,event.TimeZone,start.Location())
   if err!=nil{out.Limitations=append(out.Limitations,err.Error());continue}
   originDay:=time.Date(first.Year(),first.Month(),first.Day(),0,0,0,0,first.Location())
   day:=originDay
   generated:=0
   complete:=false
   for i:=0;i<50000;i++{
    candidate:=time.Date(day.Year(),day.Month(),day.Day(),first.Hour(),first.Minute(),first.Second(),first.Nanosecond(),first.Location())
    if candidate.After(end){complete=true;break}
    if rule.untilDefined&&candidate.After(rule.until){complete=true;break}
    if matchesRule(rule,first,candidate,i){
     generated++
     emit(candidate)
     if rule.count>0&&generated>=rule.count{complete=true;break}
    }
    day=day.AddDate(0,0,1)
   }
   if !complete{out.Limitations=append(out.Limitations,"Calendar recurrence exceeded the expansion horizon")}
  }
  for _,date:=range event.Additional{
   at,err:=ParseInstant(date,event.TimeZone,start.Location())
   if err!=nil{out.Limitations=append(out.Limitations,"Calendar additional date is unsupported");continue}
   emit(at)
  }
 }
 sort.Slice(out.Occurrences,func(i,j int)bool{
  if out.Occurrences[i].Start==out.Occurrences[j].Start{return out.Occurrences[i].UID<out.Occurrences[j].UID}
  return out.Occurrences[i].Start<out.Occurrences[j].Start
 })
 if len(seen)>maxCalendarOccurrences{
  out.Limitations=append(out.Limitations,"Calendar occurrences were limited to 1000 results")
 }
 return out,nil
}
