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
 ordinalDays map[time.Weekday]map[int]bool
 bySetPos []int
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
   r.ordinalDays=map[time.Weekday]map[int]bool{}
   for _,part:=range strings.Split(fields[1],","){
    if len(part)<2{return r,errors.New("invalid recurrence weekday")}
    name:=part[len(part)-2:]
    day,ok:=weekdayNames[name]
    if !ok{return r,errors.New("invalid recurrence weekday")}
    if len(part)==2 {
     r.weekdays[day]=true
     continue
    }
    ordinal,err:=strconv.Atoi(part[:len(part)-2])
    if err!=nil||ordinal==0||ordinal < -53||ordinal>53{
     return r,errors.New("invalid ordinal recurrence weekday")
    }
    if r.ordinalDays[day]==nil{r.ordinalDays[day]=map[int]bool{}}
    r.ordinalDays[day][ordinal]=true
   }
  case "BYSETPOS":
   if len(r.bySetPos)!=0{return r,errors.New("repeated recurrence position filter")}
   for _,value:=range strings.Split(fields[1],","){
    position,err:=strconv.Atoi(value)
    if err!=nil||position==0||position < -366||position>366{
     return r,errors.New("invalid recurrence position")
    }
    r.bySetPos=append(r.bySetPos,position)
    if len(r.bySetPos)>32{return r,errors.New("too many recurrence positions")}
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
 if len(r.ordinalDays)>0 {
  if r.freq!="MONTHLY"&&!(r.freq=="YEARLY"&&len(r.months)>0){
   return r,errors.New("ordinal weekdays require a monthly rule or yearly rule with BYMONTH")
  }
  for _,positions:=range r.ordinalDays {
   for ordinal:=range positions { if ordinal < -5||ordinal>5{
    return r,errors.New("ordinal weekday exceeds one calendar month")
   }}
  }
 }
 if len(r.bySetPos)>0 && len(r.weekdays)==0&&len(r.ordinalDays)==0&&len(r.monthDays)==0{
  return r,errors.New("positional recurrence requires a day filter")
 }
 return r,nil
}

func weekdayOffset(day,weekStart time.Weekday)int{
 return (int(day)-int(weekStart)+7)%7
}

func matchesRule(rule recurrenceRule,origin,day time.Time,dayIndex int)bool{
 if len(rule.months)>0&&!rule.months[day.Month()]{return false}
 if len(rule.weekdays)>0||len(rule.ordinalDays)>0 {
  if !rule.weekdays[day.Weekday()] {
   ordinalMatches:=false
   if positions:=rule.ordinalDays[day.Weekday()];len(positions)>0{
    monthLast:=time.Date(day.Year(),day.Month()+1,0,0,0,0,0,day.Location()).Day()
    positive:=(day.Day()-1)/7+1
    negative:=-((monthLast-day.Day())/7+1)
    ordinalMatches=positions[positive]||positions[negative]
   }
   if !ordinalMatches{return false}
  }
 }
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
  if len(monthDays)==0&&len(rule.weekdays)==0&&len(rule.ordinalDays)==0{return day.Day()==origin.Day()}
  return true
 case "YEARLY":
  if (day.Year()-origin.Year())%rule.interval!=0{return false}
  if len(rule.months)==0&&day.Month()!=origin.Month(){return false}
  if len(monthDays)==0&&len(rule.weekdays)==0&&len(rule.ordinalDays)==0{return day.Day()==origin.Day()}
  return true
 }
 return false
}

// A position filter applies to the complete recurrence period, not the
// caller's requested window. This is essential for negative BYSETPOS values.
func positionPeriod(rule recurrenceRule, day time.Time)(time.Time,time.Time){
 loc:=day.Location()
 midnight:=func(y int,m time.Month,d int)time.Time{
  return time.Date(y,m,d,0,0,0,0,loc)
 }
 switch rule.freq{
 case "DAILY":
  start:=midnight(day.Year(),day.Month(),day.Day())
  return start,start.AddDate(0,0,1)
 case "WEEKLY":
  start:=midnight(day.Year(),day.Month(),day.Day()).
   AddDate(0,0,-weekdayOffset(day.Weekday(),rule.weekStart))
  return start,start.AddDate(0,0,7)
 case "MONTHLY":
  start:=midnight(day.Year(),day.Month(),1)
  return start,start.AddDate(0,1,0)
 default:
  start:=midnight(day.Year(),time.January,1)
  return start,start.AddDate(1,0,0)
 }
}

func recurrenceDayIndex(origin,day time.Time)int{
 utcDate:=func(t time.Time)time.Time{
  return time.Date(t.Year(),t.Month(),t.Day(),12,0,0,0,time.UTC)
 }
 return int(utcDate(day).Sub(utcDate(origin))/(24*time.Hour))
}

func selectedPeriodDates(rule recurrenceRule, origin, periodStart, periodEnd time.Time)map[string]bool{
 originDay:=time.Date(origin.Year(),origin.Month(),origin.Day(),0,0,0,0,origin.Location())
 candidates:=[]string{}
 for day:=periodStart;day.Before(periodEnd);day=day.AddDate(0,0,1){
  if day.Before(originDay){continue}
  at:=time.Date(day.Year(),day.Month(),day.Day(),origin.Hour(),origin.Minute(),origin.Second(),origin.Nanosecond(),origin.Location())
  if matchesRule(rule,origin,at,recurrenceDayIndex(origin,at)){
   candidates=append(candidates,day.Format("2006-01-02"))
  }
 }
 selected:=map[string]bool{}
 for _,position:=range rule.bySetPos{
  index:=position-1
  if position<0{index=len(candidates)+position}
  if index>=0&&index<len(candidates){selected[candidates[index]]=true}
 }
 return selected
}

func exceptionTimeZone(zones []string,index int,fallback string)string{
 if index<len(zones)&&zones[index]!=""{return zones[index]}
 return fallback
}

func exclusionSet(event Event, loc *time.Location)(map[string]bool,error){
 excludes:=map[string]bool{}
 for index,value:=range event.Excluded{
  zone:=exceptionTimeZone(event.ExcludedTimeZones,index,event.TimeZone)
  instant,err:=ParseInstant(value,zone,loc)
  if err!=nil{return nil,err}
  excludes[instant.UTC().Format(time.RFC3339Nano)]=true
 }
 return excludes,nil
}

func occurrenceEnd(event Event,at time.Time,length time.Duration,calendarDays int)time.Time{
 if len(event.Start)==8&&calendarDays>0{return at.AddDate(0,0,calendarDays)}
 return at.Add(length)
}
func asOccurrence(event Event,instant time.Time,length time.Duration,calendarDays int)Occurrence{
 end:=occurrenceEnd(event,instant,length,calendarDays)
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
 masterZones:=map[string]string{}
 for _,event:=range feed.Events{
  if event.RecurrenceID==""&&event.UID!=""{
   masterZones[event.UID]=event.TimeZone
  }
 }
 for _,event:=range feed.Events{
  if event.RecurrenceID==""||event.UID==""{continue}
  zone:=event.RecurrenceIDTimeZone
  if zone==""{zone=masterZones[event.UID]}
  if zone==""{zone=event.TimeZone}
  original,err:=ParseInstant(event.RecurrenceID,zone,start.Location())
  if err!=nil{out.Limitations=append(out.Limitations,"Calendar override has an unsupported date");continue}
  if overrides[event.UID]==nil{overrides[event.UID]=map[string]bool{}}
  overrides[event.UID][original.UTC().Format(time.RFC3339Nano)]=true
 }
 appendOccurrence:=func(event Event,at time.Time,length time.Duration,calendarDays int){
  if event.Status=="CANCELLED"||!at.Before(end)||!occurrenceEnd(event,at,length,calendarDays).After(start){return}
  key:=event.UID+"|"+at.UTC().Format(time.RFC3339Nano)
  if seen[key]{return}
  seen[key]=true
  if len(out.Occurrences)<maxCalendarOccurrences{
   out.Occurrences=append(out.Occurrences,asOccurrence(event,at,length,calendarDays))
  }
 }
 for _,event:=range feed.Events{
  if event.Status=="CANCELLED"{continue}
  first,err:=ParseInstant(event.Start,event.TimeZone,start.Location())
  if err!=nil{out.Limitations=append(out.Limitations,"Calendar event has an unsupported start time");continue}
  endZone:=event.EndTimeZone
  if endZone==""{endZone=event.TimeZone}
  last,err:=ParseInstant(event.End,endZone,start.Location())
  if event.End==""&&len(event.Start)==8 {
   // RFC 5545 date-valued VEVENT without DTEND occupies one local day.
   last=first.AddDate(0,0,1)
   err=nil
  }
  if err!=nil||!last.After(first){
   out.Limitations=append(out.Limitations,"Calendar event has an unsupported duration");continue
  }
  duration:=last.Sub(first)
  calendarDays:=0
  if len(event.Start)==8 {
   if event.End!=""&&len(event.End)!=8{
    out.Limitations=append(out.Limitations,"All-day event uses an unsupported end date");continue
   }
   startDate:=time.Date(first.Year(),first.Month(),first.Day(),12,0,0,0,time.UTC)
   endDate:=time.Date(last.Year(),last.Month(),last.Day(),12,0,0,0,time.UTC)
   calendarDays=int(endDate.Sub(startDate)/(24*time.Hour))
   if calendarDays<1{
    out.Limitations=append(out.Limitations,"All-day event duration is invalid");continue
   }
  }
  if event.RecurrenceID!=""{
   appendOccurrence(event,first,duration,calendarDays)
   continue
  }
  excluded,err:=exclusionSet(event,start.Location())
  if err!=nil{out.Limitations=append(out.Limitations,"Calendar recurrence exclusion has an unsupported date");continue}
  emit:=func(at time.Time){
   key:=at.UTC().Format(time.RFC3339Nano)
   if excluded[key]||overrides[event.UID][key]{return}
   appendOccurrence(event,at,duration,calendarDays)
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
   var selectedDates map[string]bool
   var selectedStart time.Time
   for i:=0;i<50000;i++{
    candidate:=time.Date(day.Year(),day.Month(),day.Day(),first.Hour(),first.Minute(),first.Second(),first.Nanosecond(),first.Location())
    if candidate.After(end){complete=true;break}
    if rule.untilDefined&&candidate.After(rule.until){complete=true;break}
    selected:=true
    if len(rule.bySetPos)>0{
     periodStart,periodEnd:=positionPeriod(rule,candidate)
     if selectedDates==nil||!periodStart.Equal(selectedStart){
      selectedStart=periodStart
      selectedDates=selectedPeriodDates(rule,first,periodStart,periodEnd)
     }
     selected=selectedDates[candidate.Format("2006-01-02")]
    }
    if selected && matchesRule(rule,first,candidate,i){
     generated++
     emit(candidate)
     if rule.count>0&&generated>=rule.count{complete=true;break}
    }
    day=day.AddDate(0,0,1)
   }
   if !complete{out.Limitations=append(out.Limitations,"Calendar recurrence exceeded the expansion horizon")}
  }
  for index,date:=range event.Additional{
   zone:=exceptionTimeZone(event.AdditionalTimeZones,index,event.TimeZone)
   at,err:=ParseInstant(date,zone,start.Location())
   if err!=nil{out.Limitations=append(out.Limitations,"Calendar additional date is unsupported");continue}
   emit(at)
  }
 }
 sort.Slice(out.Occurrences,func(i,j int)bool{
  // RFC3339 strings with different offsets do not sort by UTC instant.
  left,_:=time.Parse(time.RFC3339,out.Occurrences[i].Start)
  right,_:=time.Parse(time.RFC3339,out.Occurrences[j].Start)
  if left.Equal(right){return out.Occurrences[i].UID<out.Occurrences[j].UID}
  return left.Before(right)
 })
 if len(seen)>maxCalendarOccurrences{
  out.Limitations=append(out.Limitations,"Calendar occurrences were limited to 1000 results")
 }
 return out,nil
}
