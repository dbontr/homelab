package transport

import (
 "net/http"
 "net/http/httptest"
 "path/filepath"
 "strings"
 "testing"
 "github.com/dbontr/homelab-new/system/store"
)
func TestReadOnlyCalendarFeed(t *testing.T){
 shared:=httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter,r *http.Request){
  if r.Method!="GET"{t.Fatalf("calendar request method %s",r.Method)}
  w.Header().Set("Content-Type","text/calendar")
  _,_=w.Write([]byte("BEGIN:VCALENDAR\r\nVERSION:2.0\r\nBEGIN:VEVENT\r\nUID:1\r\nDTSTART:20261109T120000Z\r\nSUMMARY:Seminar\r\nEND:VEVENT\r\nEND:VCALENDAR\r\n"))
 }))
 defer shared.Close()
 db,e:=store.Open(filepath.Join(t.TempDir(),"state.json"),"UTC");if e!=nil{t.Fatal(e)}
 s,e:=New(db,strings.Repeat("a",40),nil);if e!=nil{t.Fatal(e)}
 s.client=shared.Client()
 if e:=s.SetCalendarURL(shared.URL);e!=nil{t.Fatal(e)}
 h:=s.Handler();token:=strings.Repeat("a",40)
 w:=request(h,"GET","/v1/calendar","",token)
 if w.Code!=200||!strings.Contains(w.Body.String(),"Seminar"){t.Fatalf("native feed %s",w.Body.String())}
 w=request(h,"POST","/mcp",`{"jsonrpc":"2.0","id":1,"method":"tools/list"}`,token)
 if !strings.Contains(w.Body.String(),"calendar_feed"){t.Fatalf("calendar tool not exposed %s",w.Body.String())}
}
