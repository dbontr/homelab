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

func TestUnsupportedRecurrenceRemainsExplicit(t *testing.T) {
 for _,rule:=range []string{
  "FREQ=DAILY;BYSETPOS=-1;BYDAY=MO",
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
