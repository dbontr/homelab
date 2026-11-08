package calendar

import (
 "context"
 "net/http"
 "errors"
 "net/url"
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


type calendarFailTransport func(*http.Request)(*http.Response,error)
func(f calendarFailTransport)RoundTrip(req *http.Request)(*http.Response,error){return f(req)}

func TestCalendarFailureDoesNotExposePrivateSharingURL(t *testing.T) {
 secretURL:="https://calendar.example/private?token=do-not-expose"
 client:=&http.Client{Transport:calendarFailTransport(func(*http.Request)(*http.Response,error){
  return nil,&url.Error{Op:"Get",URL:secretURL,Err:errors.New("network denied")}
 })}
 _,err:=Fetch(context.Background(),client,secretURL)
 if err==nil||strings.Contains(err.Error(),"do-not-expose")||strings.Contains(err.Error(),secretURL){
    t.Fatalf("calendar error exposed private link or reported success: %v",err)
 }
}
