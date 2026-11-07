package mcp

import(
 "context"
 "encoding/json"
 "errors"
 "fmt"
 "math"
 "sort"
 "time"

 "github.com/dbontr/personal-health-mcp/internal/health"
)

func midpointStats(points []float64)(float64,float64) {
 sx,sy:=0.0,0.0
 for _,minutes:=range points{
  angle:=minutes*2*math.Pi/1440
  sx+=math.Cos(angle);sy+=math.Sin(angle)
 }
 n:=float64(len(points))
 angle:=math.Atan2(sy,sx)
 if angle<0{angle+=2*math.Pi}
 mean:=angle*1440/(2*math.Pi)
 concentration:=math.Hypot(sx,sy)/n
 spread:=math.Inf(1)
 if concentration>0{spread=math.Sqrt(math.Max(0,-2*math.Log(concentration)))*1440/(2*math.Pi)}
 return mean,spread
}
func toClock(minutes float64)string{
 n:=int(math.Round(minutes))%1440
 return fmt.Sprintf("%02d:%02d",n/60,n%60)
}
func(s *Server)registerPhysiologyTools(){
 txt:=func(d string)map[string]any{return field("string",d)}
 s.add("health_sleep_regularity","Measure variation in recorded bed and wake times. This describes sleep timing, not circadian phase or a travel-adjustment plan.",true,false,
 []string{"timezone"},
 map[string]any{"timezone":txt("IANA timezone, for example America/Chicago"),
 "days":field("integer","Lookback 7 to 90 days, default 30"),
 "include_manual_as_wake_time":field("boolean","Treat non-Google sleep record timestamps as wake times; defaults to false")},
 func(ctx context.Context,a map[string]json.RawMessage)(json.RawMessage,error){
  zone,err:=requiredString(a,"timezone");if err!=nil{return nil,err}
  loc,err:=time.LoadLocation(zone);if err!=nil{return nil,errors.New("unknown IANA timezone")}
  days,err:=boundedInt(a,"days",30,7,90);if err!=nil{return nil,err}
  includeManual:=false
  if len(a["include_manual_as_wake_time"])>0{
   if err=json.Unmarshal(a["include_manual_as_wake_time"],&includeManual);err!=nil{return nil,err}
  }
  now:=time.Now().UTC()
  points,err:=s.health.MetricSeries(ctx,"sleep","duration_hours",now.AddDate(0,0,-days).Format("2006-01-02"),now.Format("2006-01-02"),10000)
  if err!=nil{return nil,err}
  wake:=[]float64{};bed:=[]float64{}
  for _,p:=range points{
   if p.Unit!="h"||p.Value<1||p.Value>16||p.At.After(now){continue}
   if p.Source!="google_health"&&!includeManual{continue}
   w:=p.At.In(loc)
   start:=p.At.Add(-time.Duration(p.Value*float64(time.Hour))).In(loc)
   wake=append(wake,float64(w.Hour()*60+w.Minute()))
   bed=append(bed,float64(start.Hour()*60+start.Minute()))
  }
  out:=map[string]any{"timezone":zone,"observations":len(wake),"days":days,
   "meaning":"Behavioral sleep timing regularity; not a measurement of endogenous circadian phase",
   "timestamp_assumption":"Observation time is sleep end/wake time; non-Google entries excluded unless explicitly included"}
  if len(wake)<5{out["status"]="insufficient_data";return send(out)}
  b,bs:=midpointStats(bed);w,ws:=midpointStats(wake)
  out["status"]="descriptive"
  out["average_bed_clock"]=toClock(b);out["average_wake_clock"]=toClock(w)
  out["bed_clock_dispersion_minutes"]=bs;out["wake_clock_dispersion_minutes"]=ws
  return send(out)
 })
 s.add("health_recovery_review","Compare recent sleep, resting heart rate and HRV with an individual's historical observations. Return factors, not a validated readiness score.",true,false,nil,
 map[string]any{"sleep_target_hours":field("number","Optional self-selected sleep-duration target, between 4 and 12 hours")},
 func(ctx context.Context,a map[string]json.RawMessage)(json.RawMessage,error){
  target:=8.0
  if len(a["sleep_target_hours"])>0{
   if json.Unmarshal(a["sleep_target_hours"],&target)!=nil||target<4||target>12{
    return nil,errors.New("sleep_target_hours must be between 4 and 12")
   }
  }
  now:=time.Now().UTC()
  beginning:=now.AddDate(0,0,-35).Format("2006-01-02")
  ending:=now.Format("2006-01-02")
  signals:=map[string]any{}
  for _,m:=range []struct{domain,metric,unit string}{
   {"sleep","duration_hours","h"},
   {"biometric","resting_heart_rate","bpm"},
   {"biometric","hrv_rmssd","ms"},
   {"biometric","hrv_daily_average","ms"},
  }{
   records,err:=s.health.MetricSeries(ctx,m.domain,m.metric,beginning,ending,10000)
   if err!=nil{return nil,err}
   recent:=[]float64{};baseline:=[]float64{}
   cutoff:=now.AddDate(0,0,-7)
   for _,p:=range records{
    if p.Unit!=m.unit||p.At.After(now){continue}
    if !p.At.Before(cutoff){recent=append(recent,p.Value)}else{baseline=append(baseline,p.Value)}
   }
   report:=map[string]any{"recent_count":len(recent),"baseline_count":len(baseline),"unit":m.unit}
   if len(recent)>=2{
    recentMean,_:=meanSD(recent);report["recent_mean"]=recentMean
    if m.metric=="duration_hours"{report["reported_duration_deficit_hours"]=math.Max(0,target-recentMean)}
   }
   if len(recent)>=2&&len(baseline)>=5{
    rm,_:=meanSD(recent);bm,_:=meanSD(baseline)
    report["baseline_mean"]=bm;report["relative_change_percent"]=0.0
    if bm!=0{report["relative_change_percent"]=100*(rm-bm)/math.Abs(bm)}
   }else{report["status"]="insufficient_baseline"}
   signals[m.metric]=report
  }
  return send(map[string]any{"method":"descriptive comparison of 7-day recent window against preceding 28 days",
   "signals":signals,"sleep_target_hours":target,
   "interpretation":"No validated readiness score. Sleep and wearable signals cannot diagnose recovery or injury."})
 })
 s.add("health_sweat_loss_estimate","Estimate sweat loss from pre/post exercise weight and observed fluid and urine changes. The result is approximate and excludes unmeasured losses.",true,false,
 []string{"before_kg","after_kg","duration_hours","fluid_ml"},
 map[string]any{"before_kg":field("number","Body mass before activity (kg)"),
 "after_kg":field("number","Body mass after activity (kg)"),
 "fluid_ml":field("number","Fluids consumed during activity (mL)"),
 "urine_ml":field("number","Urine produced during activity (mL); defaults to zero if none"),
 "duration_hours":field("number","Workout duration in hours")},
 func(ctx context.Context,a map[string]json.RawMessage)(json.RawMessage,error){
  measures:=map[string]float64{}
  for _,k:=range []string{"before_kg","after_kg","duration_hours","fluid_ml","urine_ml"}{
   if len(a[k])==0 && k=="urine_ml"{measures[k]=0;continue}
   var v float64
   if json.Unmarshal(a[k],&v)!=nil||math.IsNaN(v)||math.IsInf(v,0)||v<0{
    return nil,fmt.Errorf("%s must be a nonnegative finite number",k)
   }
   measures[k]=v
  }
  if measures["before_kg"]<10||measures["after_kg"]<10||measures["duration_hours"]<=0||measures["duration_hours"]>24{
   return nil,errors.New("weight or duration outside supported range")
  }
  loss:=measures["before_kg"]-measures["after_kg"]+
   (measures["fluid_ml"]-measures["urine_ml"])/1000
  if loss<0{return send(map[string]any{"status":"measurement_inconsistent",
   "loss_liters":loss,"note":"The measured mass balance is negative; review intake and weights"})}
  return send(map[string]any{"status":"estimated","sweat_loss_liters":loss,
   "rate_liters_per_hour":loss/measures["duration_hours"],
   "formula":"pre minus post body mass (kg) plus fluid intake (L) minus urine output (L)",
   "uncertainty":"Clothing, respiratory losses, fluid absorption and scale error affect the result; no electrolyte loss is directly measured"})
 })
 s.add("health_progression_preview","Review recent strength training sets and propose a conservative load change only when two different sessions support it. Does not edit the plan.",true,false,
 []string{"exercise","target_repetitions"},
 map[string]any{"exercise":txt("Exact strength exercise"),
 "target_repetitions":field("integer","Expected reps per working set, 1 to 30")},
 func(ctx context.Context,a map[string]json.RawMessage)(json.RawMessage,error){
  name,err:=requiredString(a,"exercise");if err!=nil{return nil,err}
  reps,err:=boundedInt(a,"target_repetitions",0,1,30);if err!=nil{return nil,err}
  all,err:=s.health.ListTrainingSets(ctx,"",name,500,false);if err!=nil{return nil,err}
  distinct:=map[string]health.TrainingSet{}
  now:=time.Now().UTC()
  for _,e:=range all{
   if e.Activity!="strength"||e.LoadKG==nil||e.RPE==nil||e.At.After(now)||
      e.At.Before(now.AddDate(0,0,-60)){continue}
   key:=e.At.Format("2006-01-02")
   if _,ok:=distinct[key];!ok{distinct[key]=e}
  }
  dates:=[]string{}
  for d:=range distinct{dates=append(dates,d)}
  sort.Sort(sort.Reverse(sort.StringSlice(dates)))
  result:=map[string]any{"exercise":name,"distinct_sessions_with_RPE":len(dates),
   "method":"two-session low-fatigue heuristic; not a clinical training prescription",
   "changes_applied":false}
  if len(dates)<2{result["status"]="insufficient_data";return send(result)}
  a1,a2:=distinct[dates[0]],distinct[dates[1]]
  if a1.Repetitions>=reps&&a2.Repetitions>=reps&&*a1.RPE<=7&&*a2.RPE<=7{
   result["status"]="candidate_increase"
   result["reference_load_kg"]=*a1.LoadKG
   result["candidate_load_kg"]=math.Round(*a1.LoadKG*1.025*100)/100
   result["rationale"]="The two most recent dated working sets met the repetition target at RPE 7 or lower"
  }else{
   result["status"]="hold_or_review";result["rationale"]="Working sets did not both meet the specified threshold"
  }
  result["warning"]="Review injury status, technique, training context and available increments before changing a workout plan"
  return send(result)
 })
}
