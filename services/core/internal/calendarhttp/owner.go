package calendarhttp

import (
 "context"
 "errors"
 "net/http"
 "time"

 "github.com/dbontr/homelab-new/system/calendar"
 "github.com/dbontr/homelab-new/system/internal/httpjson"
)

// ErrNotConfigured distinguishes a disabled calendar feed from an upstream
// failure, so optional calendar service outages do not appear configured.
var ErrNotConfigured = errors.New("calendar is not configured")

// RegisterOwnerRoutes attaches read-only calendar views to an authenticated
// router. Authentication is enforced by the owner service before dispatch.
func RegisterOwnerRoutes(mux *http.ServeMux, fetch func(context.Context)(calendar.Feed,error)) {
 if mux==nil||fetch==nil{panic("calendar routes require a router and source")}
 mux.HandleFunc("GET /v1/calendar",func(w http.ResponseWriter,r *http.Request){
  feed,err:=fetch(r.Context())
  if errors.Is(err,ErrNotConfigured){httpjson.Error(w,503,"calendar is not configured");return}
  if err!=nil{httpjson.Error(w,502,"calendar feed unavailable");return}
  httpjson.Write(w,200,feed)
 })
 mux.HandleFunc("GET /v1/calendar/occurrences",func(w http.ResponseWriter,r *http.Request){
  fromRaw,untilRaw:=r.URL.Query().Get("start"),r.URL.Query().Get("end")
  if fromRaw==""&&untilRaw==""{
   now:=time.Now().UTC()
   fromRaw=now.Format(time.RFC3339)
   untilRaw=now.AddDate(0,0,30).Format(time.RFC3339)
  }
  from,err1:=time.Parse(time.RFC3339,fromRaw)
  until,err2:=time.Parse(time.RFC3339,untilRaw)
  if err1!=nil||err2!=nil||!until.After(from)||until.Sub(from)>366*24*time.Hour{
   httpjson.Error(w,400,"calendar range must be RFC3339 and within 366 days");return
  }
  feed,err:=fetch(r.Context())
  if errors.Is(err,ErrNotConfigured){httpjson.Error(w,503,"calendar is not configured");return}
  if err!=nil{httpjson.Error(w,502,"calendar feed unavailable");return}
  expanded,err:=calendar.Expand(feed,from,until)
  if err!=nil{httpjson.Error(w,422,"calendar range cannot be expanded");return}
  httpjson.Write(w,200,expanded)
 })
}
