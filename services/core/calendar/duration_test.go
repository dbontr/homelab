package calendar

import (
 "strings"
 "testing"
 "time"
)

func TestEventDurationPreservesLocalTimeAcrossDST(t *testing.T){
 raw:=strings.Join([]string{
  "BEGIN:VCALENDAR",
  "BEGIN:VEVENT",
  "UID:overnight",
  "DTSTART;TZID=America/Chicago:20270313T090000",
  "DURATION:P1D",
  "RRULE:FREQ=DAILY;COUNT=2",
  "SUMMARY:Research retreat",
  "END:VEVENT",
  "END:VCALENDAR",
 },"\r\n")
 out:=calendarRange(t,raw,"2027-03-13T00:00:00Z","2027-03-17T00:00:00Z")
 if len(out.Limitations)!=0||len(out.Occurrences)!=2{
  t.Fatalf("duration-based recurrence failed: %+v",out)
 }
 for i,instance:=range out.Occurrences{
  start,err:=time.Parse(time.RFC3339,instance.Start);if err!=nil{t.Fatal(err)}
  end,err:=time.Parse(time.RFC3339,instance.End);if err!=nil{t.Fatal(err)}
  loc,err:=time.LoadLocation("America/Chicago");if err!=nil{t.Fatal(err)}
  end=end.In(loc)
  if end.Hour()!=9||end.Minute()!=0{
   t.Fatalf("duration changed local wall-clock end: %+v",instance)
  }
  expected:=24*time.Hour
  if i==0{expected=23*time.Hour}
  if end.Sub(start)!=expected{
   t.Fatalf("duration across DST = %v, want %v",end.Sub(start),expected)
  }
 }
}

func TestAllDayExplicitDurationUsesLocalCalendarDays(t *testing.T){
 raw:=strings.Join([]string{
  "BEGIN:VCALENDAR",
  "BEGIN:VEVENT",
  "UID:all-day",
  "DTSTART;TZID=America/Chicago;VALUE=DATE:20270313",
  "DURATION:P2D",
  "SUMMARY:Conference",
  "END:VEVENT",
  "END:VCALENDAR",
 },"\r\n")
 out:=calendarRange(t,raw,"2027-03-12T00:00:00Z","2027-03-18T00:00:00Z")
 if len(out.Occurrences)!=1||len(out.Limitations)!=0{
  t.Fatalf("all-day duration not expanded: %+v",out)
 }
 event:=out.Occurrences[0]
 if !event.AllDay||event.Start!="2027-03-13T00:00:00-06:00"||
  event.End!="2027-03-15T00:00:00-05:00"{
  t.Fatalf("all-day duration crossed DST incorrectly: %+v",event)
 }
}

func TestCalendarDurationRejectsConflictingOrInvalidForms(t *testing.T){
 cases:=[]struct{
  name string
  duration string
  end string
  start string
 }{
  {"negative","P-1D","","20270313T090000"},
  {"empty","P","","20270313T090000"},
  {"missingTime","P1DT","","20270313T090000"},
  {"zero","PT0H","","20270313T090000"},
  {"oversized","P367D","","20270313T090000"},
  {"mixedWeek","P2W1D","","20270313T090000"},
  {"doubleEnd","PT1H","20270313T110000","20270313T090000"},
  {"allDayClock","PT1H","","20270313"},
 }
 for _,tc:=range cases{
  t.Run(tc.name,func(t *testing.T){
   lines:=[]string{"BEGIN:VCALENDAR","BEGIN:VEVENT","UID:bad","DTSTART;TZID=America/Chicago:"+tc.start,"DURATION:"+tc.duration}
   if tc.end!=""{lines=append(lines,"DTEND;TZID=America/Chicago:"+tc.end)}
   lines=append(lines,"END:VEVENT","END:VCALENDAR")
   out:=calendarRange(t,strings.Join(lines,"\r\n"),"2027-03-01T00:00:00Z","2027-04-01T00:00:00Z")
   if len(out.Limitations)==0||len(out.Occurrences)!=0{
    t.Fatalf("invalid event was accepted: %+v",out)
   }
  })
 }
}

func TestCalendarDurationWithDaysAndHours(t *testing.T){
 raw:=strings.Join([]string{
  "BEGIN:VCALENDAR",
  "BEGIN:VEVENT",
  "UID:long",
  "DTSTART;TZID=America/Chicago:20270313T090000",
  "DURATION:P1DT2H",
  "END:VEVENT",
  "END:VCALENDAR",
 },"\r\n")
 out:=calendarRange(t,raw,"2027-03-01T00:00:00Z","2027-03-18T00:00:00Z")
 if len(out.Occurrences)!=1||len(out.Limitations)!=0{
  t.Fatalf("day and time duration rejected: %+v",out)
 }
 if got:=out.Occurrences[0].End;got!="2027-03-14T11:00:00-05:00"{
  t.Fatalf("duration end %s, want March 14 at 11:00 CDT",got)
 }
}
