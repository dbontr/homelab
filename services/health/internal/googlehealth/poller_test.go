package googlehealth

import(
 "context"
 "net/http"
 "net/http/httptest"
 "path/filepath"
 "strings"
 "testing"
 "time"

 "github.com/dbontr/homelab-new/system/health/internal/health"
)
func TestBuildTimeFiltersByRecordKind(t *testing.T){
 at,_:=time.Parse(time.RFC3339,"2026-11-05T12:00:00Z")
 cases:=map[string]string{
  "sleep":`sleep.interval.end_time >= "2026-11-05T12:00:00Z"`,
  "weight":`weight.sample_time.physical_time >= "2026-11-05T12:00:00Z"`,
  "daily-resting-heart-rate":`daily_resting_heart_rate.date >= "2026-11-05"`,
 }
 for dataType,want:=range cases{
  result,err:=BuildTimeFilter(dataType,at)
  if err!=nil||result!=want{t.Fatalf("%s: %q %v",dataType,result,err)}
 }
 if _,err:=BuildTimeFilter("../secret",at);err==nil{t.Fatal("unknown type allowed")}
}
func TestSyncPollerPersistsContinuation(t *testing.T){
 calls:=0
 server:=httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter,r *http.Request){
  if r.Method!=http.MethodGet{t.Errorf("unexpected write %s",r.Method)}
  calls++
  if !strings.Contains(r.URL.Query().Get("filter"),"steps.interval.start_time >="){t.Error("missing time filter")}
  w.Header().Set("Content-Type","application/json")
  if r.URL.Query().Get("pageToken")==""{
   _,_=w.Write([]byte(`{"dataPoints":[{"name":"steps-1","steps":{"interval":{"endTime":"2026-11-05T01:00:00Z"},"count":120}}],"nextPageToken":"next-page"}`))
  }else{
   _,_=w.Write([]byte(`{"dataPoints":[{"name":"steps-2","steps":{"interval":{"endTime":"2026-11-05T02:00:00Z"},"count":240}}]}`))
  }
 }))
 defer server.Close()
 db,err:=health.Open(filepath.Join(t.TempDir(),"data.db"));if err!=nil{t.Fatal(err)}
 defer db.Close()
 client,err:=New(Config{AccessToken:"test",HTTP:server.Client(),BaseURL:server.URL+"/v4"})
 if err!=nil{t.Fatal(err)}
 cfg:=PollConfig{DataTypes:[]string{"steps"},Interval:time.Hour,Lookback:48*time.Hour,MaxPagesPerType:1}
 first,err:=client.PollOnce(context.Background(),db,cfg)
 if err!=nil||len(first)!=1||first[0].NextPageToken!="next-page"{t.Fatalf("first: %+v %v",first,err)}
 cursor,err:=db.GetSyncCursor(context.Background(),"google_health:steps")
 if err!=nil||cursor.PageToken!="next-page"{t.Fatalf("cursor: %+v %v",cursor,err)}
 second,err:=client.PollOnce(context.Background(),db,cfg)
 if err!=nil||len(second)!=1||second[0].NextPageToken!=""{t.Fatalf("second: %+v %v",second,err)}
 if _,err=db.GetSyncCursor(context.Background(),"google_health:steps");!health.IsSyncCursorMissing(err){t.Fatalf("cursor not cleared: %v",err)}
 records,err:=db.List(context.Background(),"activity","steps_interval",20,false)
 if err!=nil||len(records)!=2||calls!=2{t.Fatalf("synced: %+v calls %d error %v",records,calls,err)}
}
