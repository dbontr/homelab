package mcp

import (
 "context"
 "encoding/json"
 "errors"
 "math"
 "sort"
 "time"

 "github.com/dbontr/homelab-new/system/health/internal/health"
)
func (s *Server) registerAnalyticTools() {
 txt:=func(desc string)map[string]any{return field("string",desc)}
 s.add("health_nutrient_daily","Summarize logged nutrients for a UTC date and compare compatible units with goals. Missing nutrients are unknown, not zero.",true,false,[]string{"date"},
 map[string]any{"date":txt("UTC calendar date, YYYY-MM-DD")},
 func(ctx context.Context,a map[string]json.RawMessage)(json.RawMessage,error){
  day,err:=requiredString(a,"date");if err!=nil{return nil,err}
  if _,err=time.Parse("2006-01-02",day);err!=nil{return nil,errors.New("date must be YYYY-MM-DD")}
  totals,err:=s.health.NutritionTotals(ctx,day);if err!=nil{return nil,err}
  goals,err:=s.health.Goals(ctx);if err!=nil{return nil,err}
  comparison:=map[string]any{}
  for metric,goal:=range goals{
   observed,ok:=totals[metric]
   if !ok{comparison[metric]=map[string]any{"status":"unknown","reason":"no observations"};continue}
   g,ok:=goal.(map[string]any);if !ok{continue}
   unit,_:=g["unit"].(string);desired,_:=g["value"].(float64)
   amount,ok:=observed[unit]
   if !ok{comparison[metric]=map[string]any{"status":"unit_mismatch","recorded":observed,"goal":goal};continue}
   comparison[metric]=map[string]any{"status":"logged","logged":amount,"target":desired,"unit":unit,
    "note":"Logged intake may be incomplete; totals are not proof of total consumption."}
  }
  return send(map[string]any{"date":day,"units_and_totals":totals,"goals":comparison,"coverage":"logged observations only","aggregation":"all current records for the UTC date"})
 })
 s.add("health_anomaly_review","Find descriptive deviations from an individual's prior baseline. No clinical diagnosis is made.",true,false,[]string{"domain","metric"},
 map[string]any{"domain":txt("Observation domain"),"metric":txt("Measurement name"),
  "baseline_days":field("integer","Baseline window, default 60, 21 to 180"),
  "recent_days":field("integer","Recent window, default 7, 1 to 14")},
 func(ctx context.Context,a map[string]json.RawMessage)(json.RawMessage,error){
  domain,err:=requiredString(a,"domain");if err!=nil{return nil,err}
  metric,err:=requiredString(a,"metric");if err!=nil{return nil,err}
  baseline,err:=boundedInt(a,"baseline_days",60,21,180);if err!=nil{return nil,err}
  recent,err:=boundedInt(a,"recent_days",7,1,14);if err!=nil{return nil,err}
  vals,err:=s.health.List(ctx,domain,metric,500,false);if err!=nil{return nil,err}
  now:=time.Now().UTC();start:=now.AddDate(0,0,-(baseline+recent));split:=now.AddDate(0,0,-recent)
  old:=[]float64{};fresh:=[]float64{};unit:=""
  for _,r:=range vals{
   if r.At.After(now)||r.At.Before(start){continue}
   if unit!=""&&unit!=r.Unit{continue}
   if unit==""{unit=r.Unit}
   if r.At.Before(split){old=append(old,r.Value)}else{fresh=append(fresh,r.Value)}
  }
  report:=map[string]any{"domain":domain,"metric":metric,"unit":unit,"baseline_samples":len(old),
   "recent_samples":len(fresh),"method":"descriptive deviation, no diagnosis"}
  if len(old)<7||len(fresh)<2{
   report["status"]="insufficient_data";return send(report)
  }
  mean,sd:=meanSD(old);recentMean,_:=meanSD(fresh)
  delta:=recentMean-mean
  report["baseline_mean"]=mean;report["baseline_sd"]=sd;report["recent_mean"]=recentMean;report["delta"]=delta
  if sd>0{z:=delta/sd;report["standardized_deviation"]=z;report["flagged"]=math.Abs(z)>=2}else{
   report["flagged"]=delta!=0;report["note"]="Constant baseline; z-score is undefined"
  }
  report["status"]="evaluated"
  return send(report)
 })
 s.add("health_tdee_estimate","Estimate energy expenditure using a 14–42-day series of documented caloric intake and weight trend. Incomplete nutrition logs invalidate the result.",true,false,nil,
 map[string]any{"days":field("integer","Analysis period 14 to 42 days; default 28")},
 func(ctx context.Context,a map[string]json.RawMessage)(json.RawMessage,error){
  days,err:=boundedInt(a,"days",28,14,42);if err!=nil{return nil,err}
  calories,err:=s.health.List(ctx,"nutrition","energy_kcal",500,false);if err!=nil{return nil,err}
  weights,err:=s.health.List(ctx,"biometric","weight",500,false);if err!=nil{return nil,err}
  now:=time.Now().UTC();start:=now.AddDate(0,0,-days)
  // Daily completeness is not ascertainable from calories alone.
  totals:=map[string]float64{}
  for _,r:=range calories{
   if r.Unit!="kcal"||r.At.Before(start)||r.At.After(now){continue}
   totals[r.At.UTC().Format("2006-01-02")]+=r.Value
  }
  valid:=[]health.Record{}
  for _,r:=range weights{
   if r.Unit=="kg"&&!r.At.Before(start)&&!r.At.After(now){valid=append(valid,r)}
  }
  report:=map[string]any{"method":"intake and weight-trend approximation","days":days,
   "days_with_energy_logs":len(totals),"weight_samples":len(valid),
   "quality_warning":"Requires comprehensive intake logs and stable hydration; uncertain when logs are incomplete"}
  if len(totals)<int(math.Ceil(float64(days)*0.8))||len(valid)<4{
   report["status"]="insufficient_data";return send(report)
  }
  sort.Slice(valid,func(i,j int)bool{return valid[i].At.Before(valid[j].At)})
  elapsed:=valid[len(valid)-1].At.Sub(valid[0].At).Hours()/24
  if elapsed<float64(days)*0.65{report["status"]="insufficient_weight_span";return send(report)}
  sum:=0.0;for _,v:=range totals{sum+=v}
  intake:=sum/float64(len(totals))
  change:=valid[len(valid)-1].Value-valid[0].Value
  // 7700 kcal/kg is a rough effective energy density, not a physiological constant.
  expenditure:=intake-7700*change/elapsed
  if expenditure<=0||expenditure>10000{
   report["status"]="implausible_result";return send(report)
  }
  report["status"]="estimated";report["daily_intake_mean_kcal"]=intake
  report["weight_change_kg"]=change;report["weight_span_days"]=elapsed
  report["estimated_tdee_kcal_per_day"]=math.Round(expenditure)
  report["uncertainty"]="high; water balance, metabolic adaptation, and intake under-reporting affect this estimate"
  return send(report)
 })
}
func meanSD(v []float64)(float64,float64){
 mean:=0.0
 for _,x:=range v{mean+=x}
 mean/=float64(len(v));sum:=0.0
 for _,x:=range v{d:=x-mean;sum+=d*d}
 return mean,math.Sqrt(sum/float64(len(v)))
}
