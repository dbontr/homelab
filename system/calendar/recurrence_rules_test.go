package calendar

import (
 "strings"
 "testing"
 "time"
)

func calendarRange(t *testing.T,raw string,from,until string) Expansion {
 t.Helper()
 feed,err:=Parse(raw)
 if err!=nil{t.Fatal(err)}
 start,err:=time.Parse(time.RFC3339,from)
 if err!=nil{t.Fatal(err)}
 end,err:=time.Parse(time.RFC3339,until)
 if err!=nil{t.Fatal(err)}
 out,err:=Expand(feed,start,end)
 if err!=nil{t.Fatal(err)}
 return out
}

func TestMonthlyOrdinalWeekdaysAndExceptions(t *testing.T){
 raw:="BEGIN:VCALENDAR\r\nVERSION:2.0\r\nBEGIN:VEVENT\r\n"+
  "UID:research-meeting\r\nDTSTART;TZID=America/Chicago:20270112T090000\r\n"+
  "DTEND;TZID=America/Chicago:20270112T100000\r\n"+
  "RRULE:FREQ=MONTHLY;BYDAY=2TU;COUNT=3\r\n"+
  "EXDATE;TZID=America/Chicago:20270209T090000\r\n"+
  "SUMMARY:Research meeting\r\nEND:VEVENT\r\n"+
  "BEGIN:VEVENT\r\nUID:research-meeting\r\n"+
  "RECURRENCE-ID;TZID=America/Chicago:20270309T090000\r\n"+
  "DTSTART;TZID=America/Chicago:20270309T110000\r\n"+
  "DTEND;TZID=America/Chicago:20270309T120000\r\n"+
  "SUMMARY:Rescheduled meeting\r\nEND:VEVENT\r\nEND:VCALENDAR\r\n"
 out:=calendarRange(t,raw,"2027-01-01T00:00:00Z","2027-04-01T00:00:00Z")
 if len(out.Limitations)>0 || len(out.Occurrences)!=2 {
  t.Fatalf("monthly exception expansion failed: %+v",out)
 }
 if !strings.HasPrefix(out.Occurrences[0].Start,"2027-01-12T09:00:00") ||
  out.Occurrences[0].Summary!="Research meeting" ||
  !strings.HasPrefix(out.Occurrences[1].Start,"2027-03-09T11:00:00") ||
  out.Occurrences[1].Summary!="Rescheduled meeting"{
  t.Fatalf("monthly ordinal exception mismatched: %+v",out.Occurrences)
 }
}

func TestMonthlySetPositionLastWeekday(t *testing.T){
 raw:="BEGIN:VCALENDAR\r\nBEGIN:VEVENT\r\nUID:last-workday\r\n"+
  "DTSTART:20270101T130000Z\r\nDTEND:20270101T140000Z\r\n"+
  "RRULE:FREQ=MONTHLY;BYDAY=MO,TU,WE,TH,FR;BYSETPOS=-1;COUNT=3\r\n"+
  "SUMMARY:Close books\r\nEND:VEVENT\r\nEND:VCALENDAR\r\n"
 out:=calendarRange(t,raw,"2027-01-01T00:00:00Z","2027-04-01T00:00:00Z")
 if len(out.Occurrences)!=3 || len(out.Limitations)!=0{
  t.Fatalf("positional expansion failed: %+v",out)
 }
 for i,expect:=range []string{"2027-01-29","2027-02-26","2027-03-31"}{
  if !strings.HasPrefix(out.Occurrences[i].Start,expect){
   t.Fatalf("wrong last workday: %s want %s",out.Occurrences[i].Start,expect)
  }
 }
}

func TestWeeklyRecurrencePreservesLocalTimeAcrossDST(t *testing.T) {
 raw:="BEGIN:VCALENDAR\r\nBEGIN:VEVENT\r\nUID:weekly\r\n"+
  "DTSTART;TZID=America/Chicago:20270308T090000\r\n"+
  "DTEND;TZID=America/Chicago:20270308T100000\r\n"+
  "RRULE:FREQ=WEEKLY;COUNT=3;BYDAY=MO\r\n"+
  "SUMMARY:Seminar\r\nEND:VEVENT\r\nEND:VCALENDAR\r\n"
 out:=calendarRange(t,raw,"2027-03-01T00:00:00Z","2027-04-01T00:00:00Z")
 if len(out.Occurrences)!=3{
  t.Fatalf("missing DST occurrences: %+v",out)
 }
 if out.Occurrences[0].Start!="2027-03-08T09:00:00-06:00" ||
  out.Occurrences[1].Start!="2027-03-15T09:00:00-05:00" ||
  out.Occurrences[2].End!="2027-03-22T10:00:00-05:00" {
  t.Fatalf("local recurrence time shifted across DST: %+v",out.Occurrences)
 }
}

func TestAllDayEventWithoutEndUsesOneLocalDay(t *testing.T) {
 raw:="BEGIN:VCALENDAR\r\nBEGIN:VEVENT\r\nUID:holiday\r\n"+
  "DTSTART;VALUE=DATE:20270104\r\nSUMMARY:Holiday\r\nEND:VEVENT\r\nEND:VCALENDAR\r\n"
 out:=calendarRange(t,raw,"2027-01-01T00:00:00Z","2027-01-08T00:00:00Z")
 if len(out.Occurrences)!=1 || !out.Occurrences[0].AllDay ||
  out.Occurrences[0].Start!="2027-01-04T00:00:00Z" ||
  out.Occurrences[0].End!="2027-01-05T00:00:00Z"{
  t.Fatalf("all-day event was discarded or mis-sized: %+v",out)
 }
}

func TestWeeklySetPositionsUseWeekStartAndCalendarWeeks(t *testing.T){
 raw:="BEGIN:VCALENDAR\r\nBEGIN:VEVENT\r\nUID:weekly-last\r\n"+
  "DTSTART:20270104T090000Z\r\nDTEND:20270104T100000Z\r\n"+
  "RRULE:FREQ=WEEKLY;WKST=MO;BYDAY=MO,WE,FR;BYSETPOS=-1;COUNT=3\r\n"+
  "END:VEVENT\r\nEND:VCALENDAR\r\n"
 out:=calendarRange(t,raw,"2027-01-01T00:00:00Z","2027-02-01T00:00:00Z")
 if len(out.Limitations)!=0 || len(out.Occurrences)!=3{
  t.Fatalf("weekly positional recurrence failed: %+v",out)
 }
 for i,want:=range []string{"2027-01-08","2027-01-15","2027-01-22"}{
  if !strings.HasPrefix(out.Occurrences[i].Start,want){
   t.Fatalf("weekly position %d: got %s, want %s",i,out.Occurrences[i].Start,want)
  }
 }
}

func TestYearlySetPositionsApplyToWholeYearAcrossLeapYear(t *testing.T){
 raw:="BEGIN:VCALENDAR\r\nBEGIN:VEVENT\r\nUID:yearly-last\r\n"+
  "DTSTART:20270101T130000Z\r\nDTEND:20270101T140000Z\r\n"+
  "RRULE:FREQ=YEARLY;BYMONTH=2;BYDAY=MO,TU,WE,TH,FR;BYSETPOS=-1;COUNT=3\r\n"+
  "END:VEVENT\r\nEND:VCALENDAR\r\n"
 out:=calendarRange(t,raw,"2027-01-01T00:00:00Z","2030-01-01T00:00:00Z")
 if len(out.Limitations)!=0 || len(out.Occurrences)!=3{
  t.Fatalf("yearly positional recurrence failed: %+v",out)
 }
 for i,want:=range []string{"2027-02-26","2028-02-29","2029-02-28"}{
  if !strings.HasPrefix(out.Occurrences[i].Start,want){
   t.Fatalf("yearly position %d: got %s, want %s",i,out.Occurrences[i].Start,want)
  }
 }
}

func TestDailySetPositionsRetainWeekdayFiltering(t *testing.T){
 raw:="BEGIN:VCALENDAR\r\nBEGIN:VEVENT\r\nUID:daily\r\n"+
  "DTSTART:20270104T090000Z\r\nDTEND:20270104T100000Z\r\n"+
  "RRULE:FREQ=DAILY;BYDAY=MO,WE,FR;BYSETPOS=1;COUNT=3\r\n"+
  "END:VEVENT\r\nEND:VCALENDAR\r\n"
 out:=calendarRange(t,raw,"2027-01-01T00:00:00Z","2027-02-01T00:00:00Z")
 if len(out.Limitations)!=0 || len(out.Occurrences)!=3{
  t.Fatalf("daily positional recurrence failed: %+v",out)
 }
 for i,want:=range []string{"2027-01-04","2027-01-06","2027-01-08"}{
  if !strings.HasPrefix(out.Occurrences[i].Start,want){
   t.Fatalf("daily position %d: got %s, want %s",i,out.Occurrences[i].Start,want)
  }
 }
}

func TestCalendarEndTimestampUsesItsOwnTimeZone(t *testing.T){
 raw:="BEGIN:VCALENDAR\r\nBEGIN:VEVENT\r\nUID:cross-zone\r\n"+
  "DTSTART;TZID=America/Chicago:20270308T090000\r\n"+
  "DTEND;TZID=America/New_York:20270308T110000\r\n"+
  "RRULE:FREQ=WEEKLY;BYDAY=MO;COUNT=2\r\n"+
  "END:VEVENT\r\nEND:VCALENDAR\r\n"
 out:=calendarRange(t,raw,"2027-03-01T00:00:00Z","2027-04-01T00:00:00Z")
 if len(out.Limitations)!=0 || len(out.Occurrences)!=2{
  t.Fatalf("separate DTEND zone failed: %+v",out)
 }
 for _,instance:=range out.Occurrences{
  first,err:=time.Parse(time.RFC3339,instance.Start);if err!=nil{t.Fatal(err)}
  last,err:=time.Parse(time.RFC3339,instance.End);if err!=nil{t.Fatal(err)}
  if last.Sub(first)!=time.Hour{
   t.Fatalf("cross-zone event duration = %v, expected one hour",last.Sub(first))
  }
 }
}

func TestUnsupportedRecurrenceRemainsExplicit(t *testing.T) {
 for _,rule:=range []string{
  "FREQ=DAILY;BYSETPOS=-1",
  "FREQ=WEEKLY;BYDAY=1MO",
  "FREQ=MONTHLY;BYDAY=6MO",
  "FREQ=MONTHLY;BYDAY=MO;BYSETPOS=0",
  "FREQ=MONTHLY;BYDAY=MO;BYSETPOS=-367",
  "FREQ=MONTHLY;BYDAY=MO;BYSETPOS=1;BYSETPOS=2",
 } {
  raw:="BEGIN:VCALENDAR\r\nBEGIN:VEVENT\r\nUID:bad\r\n"+
   "DTSTART:20270101T130000Z\r\nDTEND:20270101T140000Z\r\n"+
   "RRULE:"+rule+"\r\nEND:VEVENT\r\nEND:VCALENDAR\r\n"
  out:=calendarRange(t,raw,"2027-01-01T00:00:00Z","2027-02-01T00:00:00Z")
  if len(out.Limitations)==0||len(out.Occurrences)!=0{
   t.Fatalf("unsupported recurrence accepted: %s %+v",rule,out)
  }
 }
}

func TestRecurringAllDayEventSpansLocalCalendarDaysAcrossDST(t *testing.T){
 raw:="BEGIN:VCALENDAR\r\nBEGIN:VEVENT\r\nUID:all-day-weekly\r\n"+
 "DTSTART;TZID=America/Chicago;VALUE=DATE:20270314\r\n"+
 "DTEND;TZID=America/Chicago;VALUE=DATE:20270315\r\n"+
 "RRULE:FREQ=DAILY;COUNT=2\r\n"+
 "SUMMARY:Leave\r\nEND:VEVENT\r\nEND:VCALENDAR\r\n"
 out:=calendarRange(t,raw,"2027-03-13T00:00:00Z","2027-03-17T00:00:00Z")
 if len(out.Occurrences)!=2{t.Fatalf("missing all-day recurrences: %+v",out)}
 for _,instance:=range out.Occurrences{
  first,err:=time.Parse(time.RFC3339,instance.Start)
  if err!=nil{t.Fatal(err)}
  last,err:=time.Parse(time.RFC3339,instance.End)
  if err!=nil{t.Fatal(err)}
  loc,err:=time.LoadLocation("America/Chicago")
  if err!=nil{t.Fatal(err)}
  first=first.In(loc)
  last=last.In(loc)
  if !instance.AllDay || first.Hour()!=0 || last.Hour()!=0 ||
    last.YearDay()!=first.AddDate(0,0,1).YearDay(){
    t.Fatalf("local all-day boundary shifted: %+v",instance)
  }
 }
}
