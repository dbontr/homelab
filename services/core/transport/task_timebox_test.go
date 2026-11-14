package transport

import (
 "encoding/json"
 "net/http"
 "net/http/httptest"
 "path/filepath"
 "strings"
 "testing"

 "github.com/dbontr/homelab-new/system/store"
)

func TestTaskConflictPreviewAndCalendarUncertainty(t *testing.T){
 tasks,err:=store.Open(filepath.Join(t.TempDir(),"tasks.json"),"UTC")
 if err!=nil{t.Fatal(err)}
 item,err:=tasks.AddTask(store.TaskInput{Title:"Prepare report",Scheduled:"2026-11-09T12:30:00Z",Minutes:60})
 if err!=nil{t.Fatal(err)}
 s,err:=New(tasks,strings.Repeat("t",40),nil);if err!=nil{t.Fatal(err)}
 h:=s.Handler()
 path:="/v1/tasks/conflicts?start=2026-11-09T12:00:00Z&minutes=30"
 w:=request(h,"GET",path,"",strings.Repeat("t",40))
 if w.Code!=200{t.Fatalf("preview HTTP %d: %s",w.Code,w.Body.String())}
 var view ConflictPreview
 if err:=json.Unmarshal(w.Body.Bytes(),&view);err!=nil{t.Fatal(err)}
 if len(view.Conflicts)!=0{t.Fatalf("touching endpoints cannot overlap: %+v",view)}
 w=request(h,"GET","/v1/tasks/conflicts?start=2026-11-09T12:35:00Z&minutes=25","",strings.Repeat("t",40))
 if err:=json.Unmarshal(w.Body.Bytes(),&view);err!=nil{t.Fatal(err)}
 if len(view.Conflicts)!=1||view.Conflicts[0].Source!="task"{t.Fatalf("missing task conflict: %+v",view)}
 w=request(h,"GET","/v1/tasks/conflicts?start=2026-11-09T12:35:00Z&minutes=25&exclude_id="+item.ID,"",strings.Repeat("t",40))
 if err:=json.Unmarshal(w.Body.Bytes(),&view);err!=nil{t.Fatal(err)}
 if len(view.Conflicts)!=0{t.Fatal("editing a timebox conflicts with itself")}
}

func TestTaskConflictPreviewExpandsSupportedCalendarRecurrence(t *testing.T){
 source:=httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter,_ *http.Request){
  _,_=w.Write([]byte("BEGIN:VCALENDAR\r\nBEGIN:VEVENT\r\nUID:1\r\nDTSTART:20261109T120000Z\r\nDTEND:20261109T140000Z\r\nSUMMARY:Lecture\r\nRRULE:FREQ=WEEKLY\r\nEND:VEVENT\r\nEND:VCALENDAR\r\n"))
 }))
 defer source.Close()
 tasks,err:=store.Open(filepath.Join(t.TempDir(),"tasks.json"),"UTC")
 if err!=nil{t.Fatal(err)}
 s,err:=New(tasks,strings.Repeat("t",40),nil);if err!=nil{t.Fatal(err)}
 s.client=source.Client()
 if err:=s.SetCalendarURL(source.URL);err!=nil{t.Fatal(err)}
 w:=request(s.Handler(),"GET","/v1/tasks/conflicts?start=2026-11-16T12:30:00Z&minutes=20","",strings.Repeat("t",40))
 if w.Code!=200{t.Fatalf("preview HTTP %d: %s",w.Code,w.Body.String())}
 var result ConflictPreview
 if err:=json.Unmarshal(w.Body.Bytes(),&result);err!=nil{t.Fatal(err)}
 if len(result.Conflicts)!=1||result.Conflicts[0].Source!="calendar"{
  t.Fatalf("calendar event absent: %+v",result)
 }
 if len(result.Limitations)!=0{t.Fatalf("supported weekly recurrence had unexpected limitations: %+v",result.Limitations)}
}
