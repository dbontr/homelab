package calendar

import (
 "strings"
 "testing"
)
func TestRecurringEventsRemainExplicit(t *testing.T){
 raw:="BEGIN:VCALENDAR\r\nVERSION:2.0\r\nBEGIN:VEVENT\r\nUID:1\r\nDTSTART;TZID=America/Chicago:20261109T130000\r\nDTEND;TZID=America/Chicago:20261109T140000\r\nSUMMARY:Research\r\n discussion\r\nRRULE:FREQ=WEEKLY;BYDAY=MO\r\nEND:VEVENT\r\nEND:VCALENDAR\r\n"
 feed,e:=Parse(raw);if e!=nil{t.Fatal(e)}
 if len(feed.Events)!=1||!strings.Contains(feed.Events[0].Summary,"discussion")||!feed.RecurrencesUnexpanded{t.Fatalf("invalid feed %#v",feed)}
}
func TestValidateSource(t *testing.T){
 if _,e:=Parse("bad");e==nil{t.Fatal("accepted invalid feed")}
 if e:=ValidateURL("http://example.com/share");e==nil{t.Fatal("accepted non-TLS URL")}
}
