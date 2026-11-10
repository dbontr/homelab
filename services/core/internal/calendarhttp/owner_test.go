package calendarhttp

import (
 "context"
 "errors"
 "net/http"
 "net/http/httptest"
 "strings"
 "testing"

 "github.com/dbontr/homelab-new/system/calendar"
)

func TestOwnerCalendarViewsShareFeedAndRespectAvailability(t *testing.T){
 feed,err:=calendar.Parse("BEGIN:VCALENDAR\r\nVERSION:2.0\r\nBEGIN:VEVENT\r\nUID:1\r\nDTSTART:20261109T120000Z\r\nSUMMARY:Seminar\r\nEND:VEVENT\r\nEND:VCALENDAR\r\n")
 if err!=nil{t.Fatal(err)}
 var failure error
 calls:=0
 mux:=http.NewServeMux()
 RegisterOwnerRoutes(mux,func(context.Context)(calendar.Feed,error){
  calls++
  if failure!=nil{return calendar.Feed{},failure}
  return feed,nil
 })
 for _,path:=range []string{"/v1/calendar","/v1/calendar/occurrences?start=2026-11-09T00:00:00Z&end=2026-11-10T00:00:00Z"}{
  got:=httptest.NewRecorder()
  mux.ServeHTTP(got,httptest.NewRequest("GET",path,nil))
  if got.Code!=200||!strings.Contains(got.Body.String(),"Seminar"){t.Fatalf("%s: %d %s",path,got.Code,got.Body.String())}
 }
 if calls!=2{t.Fatalf("feed read %d times, want two",calls)}
 failure=ErrNotConfigured
 for _,path:=range []string{"/v1/calendar","/v1/calendar/occurrences?start=2026-11-09T00:00:00Z&end=2026-11-10T00:00:00Z"}{
  got:=httptest.NewRecorder();mux.ServeHTTP(got,httptest.NewRequest("GET",path,nil))
  if got.Code!=503{t.Fatalf("%s: expected unavailable, got %d",path,got.Code)}
 }
 failure=errors.New("provider failed")
 got:=httptest.NewRecorder();mux.ServeHTTP(got,httptest.NewRequest("GET","/v1/calendar",nil))
 if got.Code!=502{t.Fatalf("upstream failure: %d",got.Code)}
}

func TestOwnerCalendarRejectsInvalidAndWriteRequests(t *testing.T){
 mux:=http.NewServeMux()
 calls:=0
 RegisterOwnerRoutes(mux,func(context.Context)(calendar.Feed,error){calls++;return calendar.Feed{},nil})
 for _,path:=range []string{
  "/v1/calendar/occurrences?start=bad&end=2026-11-10T00:00:00Z",
  "/v1/calendar/occurrences?start=2026-11-10T00:00:00Z&end=2026-11-09T00:00:00Z",
  "/v1/calendar/occurrences?start=2026-11-09T00:00:00Z&end=2028-01-10T00:00:00Z",
 }{
  got:=httptest.NewRecorder();mux.ServeHTTP(got,httptest.NewRequest("GET",path,nil))
  if got.Code!=400{t.Errorf("%s: got %d, expected 400",path,got.Code)}
 }
 if calls!=0{t.Fatal("invalid ranges contacted the calendar provider")}
 got:=httptest.NewRecorder();mux.ServeHTTP(got,httptest.NewRequest("POST","/v1/calendar",nil))
 if got.Code!=http.StatusMethodNotAllowed{t.Fatalf("calendar mutation admitted: %d",got.Code)}
}
