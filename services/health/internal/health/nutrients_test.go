package health

import(
 "context"
 "testing"
 "time"
)
func TestNutritionTotalsDoesNotTruncateAtFiveHundredRecords(t *testing.T){
 s:=testStore(t);ctx:=context.Background()
 at,_:=time.Parse(time.RFC3339,"2026-11-05T12:00:00Z")
 for i:=0;i<510;i++{
  _,err:=s.Add(ctx,Record{Domain:"nutrition",Metric:"protein",Value:1,Unit:"g",
    At:at,Source:"manual",Method:"reported"})
  if err!=nil{t.Fatal(err)}
 }
 totals,err:=s.NutritionTotals(ctx,"2026-11-05")
 if err!=nil{t.Fatal(err)}
 if totals["protein"]["g"]!=510{t.Fatalf("truncated nutrient total: %+v",totals)}
}
func TestMetricSeriesHonorsRangeAndSupersededValues(t *testing.T){
 s:=testStore(t);ctx:=context.Background()
 at,_:=time.Parse(time.RFC3339,"2026-11-04T12:00:00Z")
 old,err:=s.Add(ctx,Record{Domain:"biometric",Metric:"weight",Value:81,Unit:"kg",
  At:at,Source:"manual",Method:"reported"})
 if err!=nil{t.Fatal(err)}
 _,err=s.Add(ctx,Record{Domain:"biometric",Metric:"weight",Value:80,Unit:"kg",
  At:at,Source:"manual",Method:"reported",Replaces:old.ID})
 if err!=nil{t.Fatal(err)}
 results,err:=s.MetricSeries(ctx,"biometric","weight","2026-11-01","2026-11-08",100)
 if err!=nil||len(results)!=1||results[0].Value!=80{t.Fatalf("series: %+v %v",results,err)}
 results,err=s.MetricSeries(ctx,"biometric","weight","2026-11-05","2026-11-08",100)
 if err!=nil||len(results)!=0{t.Fatalf("window filtering: %+v %v",results,err)}
}
