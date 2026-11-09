package store

import (
 "testing"
 "time"
)

func TestPreviousWeekReportUsesOwnerCalendarWeekAcrossDST(t *testing.T){
 s,_:=testStore(t)
 cases:=[]struct{
  instant string
  week string
 }{
  {"2027-03-15T02:00:00Z","2027-03-01"},
  {"2027-03-15T16:00:00Z","2027-03-08"},
  {"2027-11-08T02:00:00Z","2027-10-25"},
  {"2027-11-08T16:00:00Z","2027-11-01"},
 }
 for _,tc:=range cases{
  at,err:=time.Parse(time.RFC3339,tc.instant)
  if err!=nil{t.Fatal(err)}
  report,err:=s.PreviousWeekReport(at)
  if err!=nil{t.Fatal(err)}
  if report.WeekOf!=tc.week{
   t.Fatalf("wrong completed week for %s: %s wanted %s",tc.instant,report.WeekOf,tc.week)
  }
  if report.TotalCompleted!=0||len(report.Days)!=7{
   t.Fatalf("missing personal data was inferred as activity: %+v",report)
  }
 }
}
