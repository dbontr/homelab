package observability

import (
 "context"
 "fmt"
 "net/http"
 "net/http/httptest"
 "strings"
 "testing"
 "time"
)

func TestSecurityAuditCountsKnownEventsWithoutExposingLogLines(t *testing.T){
 now:=time.Now().UnixNano()
 handler:=http.HandlerFunc(func(w http.ResponseWriter,r *http.Request){
  if r.URL.Path!="/loki/api/v1/query_range"{t.Fatalf("unexpected path %s",r.URL.Path)}
  if !strings.Contains(r.URL.Query().Get("query"),"security audit"){t.Fatal("security log selection missing")}
  w.Header().Set("Content-Type","application/json")
  fmt.Fprintf(w,`{"status":"success","data":{"resultType":"streams","result":[{"stream":{"job":"systemd"},"values":[["%d","security audit owner_authentication denied credential=SHOULD_NOT_LEAK"],["%d","security audit owner_authentication denied"],["%d","security audit owner_authentication denied"],["%d","security audit web_session revoked"]]}]}}`,now,now-1,now-2,now-3)
 })
 server:=httptest.NewServer(handler)
 defer server.Close()
 client,err:=New("",server.URL)
 if err!=nil{t.Fatal(err)}
 report,err:=client.SecurityAudit(context.Background())
 if err!=nil{t.Fatal(err)}
 if report.Window!="24h"||report.Truncated||len(report.Events)!=2{t.Fatalf("unexpected report %+v",report)}
 found:=false
 for _,event:=range report.Events{
  if event.Event=="owner_authentication denied"{
   found=true
   if event.Count!=3||event.Severity!="warning"{t.Fatalf("unexpected denial event %+v",event)}
  }
  if strings.Contains(event.Event,"SHOULD_NOT_LEAK"){t.Fatal("raw log content leaked")}
 }
 if !found{t.Fatal("authentication denials omitted")}
}
