package googlehealth

import (
 "context"
 "net/http"
 "net/http/httptest"
 "path/filepath"
 "testing"

 "github.com/dbontr/homelab-new/system/health/internal/health"
)
func TestReadOnlySyncAndIdempotentImport(t *testing.T){
 calls:=0
 upstream:=httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter,r *http.Request){
  if r.Method!=http.MethodGet{t.Errorf("unexpected upstream write method: %s",r.Method)}
  if r.URL.Path!="/v4/users/me/dataTypes/heart-rate/dataPoints"{t.Errorf("unexpected path: %s",r.URL.Path)}
  if r.Header.Get("Authorization")!="Bearer testtoken"{t.Error("authorization missing")}
  calls++
  w.Header().Set("Content-Type","application/json")
  _,_=w.Write([]byte(`{"dataPoints":[{"name":"users/me/dataTypes/heart-rate/dataPoints/1","heartRate":{"sampleTime":{"physicalTime":"2026-10-10T01:00:00Z"},"beatsPerMinute":"72"}}]}`))
 }))
 defer upstream.Close()
 c,err:=New(Config{AccessToken:"testtoken",BaseURL:upstream.URL+"/v4",HTTP:upstream.Client()})
 if err!=nil{t.Fatal(err)}
 db,err:=health.Open(filepath.Join(t.TempDir(),"health.db"));if err!=nil{t.Fatal(err)}
 defer db.Close()
 result,err:=c.Sync(context.Background(),db,"heart-rate","", "",1)
 if err!=nil||result.Imported!=1{t.Fatalf("first import: %+v %v",result,err)}
 second,err:=c.Sync(context.Background(),db,"heart-rate","", "",1)
 if err!=nil||second.Existing!=1{t.Fatalf("idempotent import: %+v %v",second,err)}
 recs,err:=db.List(context.Background(),"biometric","heart_rate",10,false)
 if err!=nil||len(recs)!=1||recs[0].Value!=72{t.Fatalf("normalized heart rate: %+v %v",recs,err)}
 if calls!=2{t.Fatal("unexpected request count")}
}
func TestRejectsUnknownDataTypes(t *testing.T){
 c,err:=New(Config{AccessToken:"testtoken"})
 if err!=nil{t.Fatal(err)}
 if _,err:=c.Page(context.Background(),"../secrets","","");err==nil{t.Fatal("invalid type accepted")}
}

func TestExpiredAccessTokenRefreshesOnce(t *testing.T){
 refreshes:=0
 upstream:=httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter,r *http.Request){
  switch r.URL.Path {
  case "/token":
   refreshes++
   if r.Method!=http.MethodPost{t.Fatal("refresh must use POST")}
   _,_=w.Write([]byte(`{"access_token":"renewed","expires_in":3600}`))
  case "/v4/users/me/dataTypes/steps/dataPoints":
   if r.Header.Get("Authorization")!="Bearer renewed"{w.WriteHeader(http.StatusUnauthorized);return}
   _,_=w.Write([]byte(`{"dataPoints":[]}`))
  default:t.Fatalf("unexpected path: %s",r.URL.Path)
  }
 }))
 defer upstream.Close()
 client,err:=New(Config{AccessToken:"expired",RefreshToken:"refresh",ClientID:"client",ClientSecret:"secret",
  HTTP:upstream.Client(),BaseURL:upstream.URL+"/v4",TokenURL:upstream.URL+"/token"})
 if err!=nil{t.Fatal(err)}
 page,err:=client.Page(context.Background(),"steps","","")
 if err!=nil||len(page.DataPoints)!=0{t.Fatalf("token refresh retry: %+v %v",page,err)}
 if refreshes!=1{t.Errorf("expected 1 token refresh, got %d",refreshes)}
}

func TestNormalizeDayPrecisionAndVO2Method(t *testing.T){
 r,err:=normalize("daily-resting-heart-rate",[]byte("{\"dailyRestingHeartRate\":{\"date\":{\"year\":2026,\"month\":11,\"day\":5},\"beatsPerMinute\":\"54\"}}"))
 if err!=nil{t.Fatal(err)}
 if r.At.Format("2006-01-02")!="2026-11-05"||r.Value!=54||r.Method!="estimated"{
  t.Fatalf("daily resting heart rate: %+v",r)
 }
 r,err=normalize("vo2-max",[]byte("{\"vo2Max\":{\"sampleTime\":{\"physicalTime\":\"2026-11-05T18:00:00Z\"},\"measurementMethod\":\"METABOLIC_CART\",\"vo2Max\":43.2}}"))
 if err!=nil{t.Fatal(err)}
 if r.Method!="measured"||r.Value!=43.2||r.Metric!="vo2_max"{t.Fatalf("laboratory VO2 max: %+v",r)}
 r,err=normalize("vo2-max",[]byte("{\"vo2Max\":{\"sampleTime\":{\"physicalTime\":\"2026-11-05T18:00:00Z\"},\"measurementMethod\":\"GOOGLE_DEMOGRAPHIC\",\"vo2Max\":44}}"))
 if err!=nil{t.Fatal(err)}
 if r.Method!="estimated"{t.Fatalf("estimate mislabeled: %+v",r)}
}
func TestGoogleNormalizationRequiresRealDate(t *testing.T){
 _,err:=normalize("daily-resting-heart-rate",[]byte("{\"dailyRestingHeartRate\":{\"date\":{\"year\":2026,\"month\":2,\"day\":30},\"beatsPerMinute\":\"60\"}}"))
 if err==nil{t.Fatal("fabricated invalid observation timestamp")}
}
