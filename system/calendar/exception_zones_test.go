package calendar

import (
 "strings"
 "testing"
)

func TestCalendarExceptionsUseTheirOwnTimeZones(t *testing.T){
 raw:=strings.Join([]string{
  "BEGIN:VCALENDAR",
  "VERSION:2.0",
  "BEGIN:VEVENT",
  "UID:seminar",
  "DTSTART;TZID=America/Chicago:20270308T090000",
  "DTEND;TZID=America/Chicago:20270308T100000",
  "RRULE:FREQ=WEEKLY;COUNT=3;BYDAY=MO",
  "EXDATE;TZID=America/New_York:20270315T100000",
  "RDATE;TZID=America/New_York:20270323T100000",
  "SUMMARY:Seminar",
  "END:VEVENT",
  "BEGIN:VEVENT",
  "UID:seminar",
  "RECURRENCE-ID;TZID=America/Chicago:20270322T090000",
  "DTSTART;TZID=UTC:20270322T160000",
  "DTEND;TZID=UTC:20270322T170000",
  "SUMMARY:Rescheduled seminar",
  "END:VEVENT",
  "END:VCALENDAR",
 },"\r\n")+"\r\n"
 feed,err:=Parse(raw)
 if err!=nil{t.Fatal(err)}
 if got:=feed.Events[0].ExcludedTimeZones;len(got)!=1||got[0]!="America/New_York"{
  t.Fatalf("exception zone was not parsed: %v",got)
 }
 if got:=feed.Events[1].RecurrenceIDTimeZone;got!="America/Chicago"{
  t.Fatalf("recurrence identity zone was lost: %s",got)
 }
 expanded:=calendarRange(t,raw,"2027-03-01T00:00:00Z","2027-04-01T00:00:00Z")
 if len(expanded.Limitations)!=0||len(expanded.Occurrences)!=3{
  t.Fatalf("calendar exceptions were not applied: %+v",expanded)
 }
 expected:=[]struct{start,title string}{
  {"2027-03-08T09:00:00-06:00","Seminar"},
  {"2027-03-22T16:00:00Z","Rescheduled seminar"},
  {"2027-03-23T10:00:00-04:00","Seminar"},
 }
 for i,want:=range expected{
  got:=expanded.Occurrences[i]
  if got.Start!=want.start||got.Summary!=want.title{
   t.Fatalf("occurrence %d: got %+v, expected %+v",i,got,want)
  }
 }
}

func TestRescheduledEventUsesMasterZoneWhenRecurrenceIDHasNoTZID(t *testing.T){
 raw:=strings.Join([]string{
  "BEGIN:VCALENDAR",
  "BEGIN:VEVENT",
  "UID:cohort",
  "DTSTART;TZID=America/Chicago:20270308T090000",
  "DTEND;TZID=America/Chicago:20270308T100000",
  "RRULE:FREQ=WEEKLY;COUNT=2;BYDAY=MO",
  "END:VEVENT",
  "BEGIN:VEVENT",
  "UID:cohort",
  "RECURRENCE-ID:20270315T090000",
  "DTSTART;TZID=UTC:20270315T180000",
  "DTEND;TZID=UTC:20270315T190000",
  "END:VEVENT",
  "END:VCALENDAR",
 },"\r\n")
 expanded:=calendarRange(t,raw,"2027-03-01T00:00:00Z","2027-04-01T00:00:00Z")
 if len(expanded.Occurrences)!=2||len(expanded.Limitations)!=0{
  t.Fatalf("master zone was not used for recurrence identity: %+v",expanded)
 }
 if expanded.Occurrences[1].Start!="2027-03-15T18:00:00Z"{
  t.Fatalf("rescheduled instant or exception was lost: %+v",expanded.Occurrences)
 }
}

func TestCalendarOccurrencesAreChronologicalAcrossOffsets(t *testing.T){
 raw:=strings.Join([]string{
  "BEGIN:VCALENDAR",
  "BEGIN:VEVENT",
  "UID:late-utc",
  "DTSTART:20270103T220000Z",
  "DTEND:20270103T230000Z",
  "END:VEVENT",
  "BEGIN:VEVENT",
  "UID:early-west",
  "DTSTART;TZID=America/Los_Angeles:20270103T120000",
  "DTEND;TZID=America/Los_Angeles:20270103T130000",
  "END:VEVENT",
  "END:VCALENDAR",
 },"\r\n")
 expanded:=calendarRange(t,raw,"2027-01-03T00:00:00Z","2027-01-04T00:00:00Z")
 if len(expanded.Occurrences)!=2||expanded.Occurrences[0].UID!="early-west"||
  expanded.Occurrences[1].UID!="late-utc"{
  t.Fatalf("mixed UTC offsets were ordered as strings rather than instants: %+v",expanded.Occurrences)
 }
}
