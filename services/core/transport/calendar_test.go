package transport

import (
 "net/http"
 "net/http/httptest"
 "path/filepath"
 "strings"
 "testing"
 "github.com/dbontr/homelab-new/system/store"
 "github.com/dbontr/homelab-new/system/internal/calendarhttp"
 "github.com/dbontr/homelab-new/system/calendar"
 "encoding/json"
 "context"
 "sync/atomic"
)
func TestReadOnlyCalendarFeed(t *testing.T){
 shared:=httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter,r *http.Request){
  if r.Method!="GET"{t.Fatalf("calendar request method %s",r.Method)}
  w.Header().Set("Content-Type","text/calendar")
  _,_=w.Write([]byte("BEGIN:VCALENDAR\r\nVERSION:2.0\r\nBEGIN:VEVENT\r\nUID:1\r\nDTSTART:20261109T120000Z\r\nDTEND:20261109T130000Z\r\nSUMMARY:Seminar\r\nEND:VEVENT\r\nEND:VCALENDAR\r\n"))
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


func TestCalendarServiceSuppliesNativeMCPAndRecurrenceViews(t *testing.T) {
	const serviceToken="separate-calendar-service-token-with-sufficient-length"
	feed,err:=calendar.Parse("BEGIN:VCALENDAR\r\nVERSION:2.0\r\nBEGIN:VEVENT\r\nUID:1\r\nDTSTART:20261109T120000Z\r\nDTEND:20261109T130000Z\r\nSUMMARY:Seminar\r\nEND:VEVENT\r\nEND:VCALENDAR\r\n")
	if err!=nil{t.Fatal(err)}
	var calls atomic.Int32
	private:=httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter,r *http.Request){
		calls.Add(1)
		if r.URL.Path!="/v1/calendar"||r.Header.Get("Authorization")!="Bearer "+serviceToken{
			t.Error("calendar was read without the dedicated service identity")
		}
		w.Header().Set("Content-Type","application/json")
		_ = json.NewEncoder(w).Encode(feed)
	}))
	defer private.Close()
	client,err:=calendarhttp.NewClient(private.URL,serviceToken)
	if err!=nil{t.Fatal(err)}
	db,err:=store.Open(filepath.Join(t.TempDir(),"state.json"),"UTC")
	if err!=nil{t.Fatal(err)}
	server,err:=New(db,strings.Repeat("a",40),nil)
	if err!=nil{t.Fatal(err)}
	if err:=server.SetCalendarService(client.Fetch);err!=nil{t.Fatal(err)}
	if err:=server.SetCalendarURL(private.URL);err==nil{
		t.Fatal("calendar source was replaced after selection")
	}
	handler:=server.Handler()
	token:=strings.Repeat("a",40)
	native:=request(handler,"GET","/v1/calendar","",token)
	if native.Code!=http.StatusOK||!strings.Contains(native.Body.String(),"Seminar"){
		t.Fatalf("native calendar unavailable: %d %s",native.Code,native.Body.String())
	}
	window:="/v1/calendar/occurrences?start=2026-11-09T00:00:00Z&end=2026-11-10T00:00:00Z"
	occurrences:=request(handler,"GET",window,"",token)
	if occurrences.Code!=http.StatusOK||!strings.Contains(occurrences.Body.String(),"Seminar"){
		t.Fatalf("calendar occurrences unavailable: %d %s",occurrences.Code,occurrences.Body.String())
	}
	mcp:=request(handler,"POST","/mcp",`{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"calendar_feed","arguments":{}}}`,token)
	if mcp.Code!=http.StatusOK||!strings.Contains(mcp.Body.String(),"Seminar"){
		t.Fatalf("unified MCP calendar feed unavailable: %d %s",mcp.Code,mcp.Body.String())
	}
	if calls.Load()!=3{t.Fatalf("calendar service received %d calls, want 3",calls.Load())}
	if _,err:=client.Fetch(context.Background());err!=nil{t.Fatal(err)}
}
