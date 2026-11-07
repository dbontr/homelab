package mcp

import (
 "context"
 "encoding/json"
 "errors"
 "fmt"
 "math"
 "strings"
 "time"

 "github.com/dbontr/personal-health-mcp/internal/health"
)

func decodeFields(a map[string]json.RawMessage,v any) error {
 data,err:=json.Marshal(a);if err!=nil{return err}
 return json.Unmarshal(data,v)
}
func send(v any) (json.RawMessage,error) {return json.Marshal(v)}
func dateOrNow(raw json.RawMessage) (time.Time,error) {
 if len(raw)==0 {return time.Now().UTC(),nil}
 var v string
 if err:=json.Unmarshal(raw,&v);err!=nil{return time.Time{},errors.New("at must be an RFC3339 timestamp")}
 date,err:=time.Parse(time.RFC3339,v)
 if err!=nil {return time.Time{},errors.New("at must be an RFC3339 timestamp")}
 return date,nil
}
func (s *Server) registerHealthTools() {
 txt:=func(description string) map[string]any {return field("string",description)}
 measure:=map[string]any{
  "domain":txt("Observation domain, such as sleep, nutrition, workout, symptom, medication, cognition or biometric."),
  "metric":txt("Lowercase metric identifier such as duration_hours, protein or resting_heart_rate."),
  "value":field("number","Recorded numeric value."),
  "unit":txt("Measurement unit, such as h, g, mg, kg, bpm, count, or score."),
  "at":txt("Observation time, RFC3339. Defaults to the current UTC time."),
  "source":txt("Origin of the observation, such as manual, google_health, or a device name."),
  "source_id":txt("Stable upstream identifier for idempotent imports."),
  "method":selectField("How the value was obtained.","measured","estimated","reported"),
  "confidence":field("number","Optional confidence in [0,1], not a guarantee of accuracy."),
  "metadata":map[string]any{"type":"object","description":"Context and evidence. Do not invent unmeasured nutrient values.","additionalProperties":true},
  "replaces":txt("ID of an existing record to correct without deleting its history."),
 }
 s.add("health_record_add","Save a measured, estimated, or self-reported health observation. Missing nutrient measurements must not be entered as zero.",false,false,[]string{"domain","metric","value","unit"},measure,
  func(ctx context.Context,a map[string]json.RawMessage)(json.RawMessage,error){
   var r health.Record
   if err:=decodeFields(a,&r);err!=nil{return nil,err}
   if r.At.IsZero(){r.At=time.Now().UTC()}
   if r.Source==""{r.Source="manual"}
   if r.Method==""{r.Method="reported"}
   result,err:=s.health.Add(ctx,r)
   if err!=nil{return nil,err}
   return send(result)
  })
 s.add("health_record_list","Read current observations with timestamps, units, sources and uncertainty metadata.",true,false,nil,
 map[string]any{"domain":txt("Optional domain filter"),"metric":txt("Optional metric filter"),"limit":field("integer","Maximum 500; default 100"),"include_history":field("boolean","Include superseded records")},
 func(ctx context.Context,a map[string]json.RawMessage)(json.RawMessage,error){
  n,err:=boundedInt(a,"limit",100,1,500);if err!=nil{return nil,err}
  var history bool;_ =json.Unmarshal(a["include_history"],&history)
  records,err:=s.health.List(ctx,optionalString(a,"domain",""),optionalString(a,"metric",""),n,history)
  if err!=nil{return nil,err}
  return send(records)
 })
 s.add("health_goal_set","Set the target for a measurable metric, including macro- and micronutrients.",false,false,[]string{"metric","value","unit"},
 map[string]any{"metric":txt("Metric name"),"value":field("number","Target value"),"unit":txt("Unit")},
 func(ctx context.Context,a map[string]json.RawMessage)(json.RawMessage,error){
  metric,err:=requiredString(a,"metric");if err!=nil{return nil,err}
  var value float64
  if err=json.Unmarshal(a["value"],&value);err!=nil{return nil,errors.New("value must be a number")}
  unit,err:=requiredString(a,"unit");if err!=nil{return nil,err}
  if err=s.health.SetGoal(ctx,metric,value,unit);err!=nil{return nil,err}
  return send(map[string]any{"metric":metric,"value":value,"unit":unit})
 })
 s.add("health_goal_list","Read nutrient and physiological targets.",true,false,nil,map[string]any{},
 func(ctx context.Context,a map[string]json.RawMessage)(json.RawMessage,error){
  goals,err:=s.health.Goals(ctx);if err!=nil{return nil,err};return send(goals)
 })
 sessionFields:=map[string]any{
 "id":txt("Stable session ID"),
 "date":txt("Local calendar date YYYY-MM-DD"),
 "title":txt("Session or meal title"),
 "items":map[string]any{"type":"array","items":map[string]any{"type":"object","properties":map[string]any{
   "id":txt("Optional meal item ID"),"name":txt("Food or ingredient"),"quantity":field("number","Positive amount"),
   "unit":txt("Unit"),"recipe_id":txt("Linked RecipeSage recipe"),"notes":txt("Meal description"),
 },"required":[]string{"name","quantity","unit"}}},
 "exercises":map[string]any{"type":"array","items":map[string]any{"type":"object","properties":map[string]any{
   "name":txt("Exercise name"),"sets":field("integer","Number of sets"),"repetitions":field("integer","Repetitions per set"),"target":txt("Intensity, load, pace or other target")}}},
 }
 s.add("health_plan_save","Create or revise an interactive workout or meal plan using optimistic revision checks. Existing sessions are retained unless explicitly replaced.",false,false,[]string{"kind","title","sessions","expected_revision"},
 map[string]any{"id":txt("Plan ID for edits; omit for creation"),"kind":selectField("Plan category","workout","meal"),"title":txt("Plan title"),
 "sessions":map[string]any{"type":"array","items":map[string]any{"type":"object","properties":sessionFields,"required":[]string{"id","date","title"}}},
 "expected_revision":field("integer","Zero for creation; current revision for edits")},
 func(ctx context.Context,a map[string]json.RawMessage)(json.RawMessage,error){
  var p health.Plan
  if err:=decodeFields(a,&p);err!=nil{return nil,err}
  var version int
  if err:=json.Unmarshal(a["expected_revision"],&version);err!=nil{return nil,errors.New("expected_revision must be an integer")}
  saved,err:=s.health.SavePlan(ctx,p,version)
  if err!=nil{return nil,err}
  return send(saved)
 })
 s.add("health_plan_get","Get a saved workout or meal plan with its current revision.",true,false,[]string{"id"},map[string]any{"id":txt("Plan ID")},
 func(ctx context.Context,a map[string]json.RawMessage)(json.RawMessage,error){
  id,err:=requiredString(a,"id");if err!=nil{return nil,err}
  p,err:=s.health.GetPlan(ctx,id);if err!=nil{return nil,err};return send(p)
 })
 s.add("health_plan_list","List workout and meal plans.",true,false,nil,map[string]any{},
 func(ctx context.Context,a map[string]json.RawMessage)(json.RawMessage,error){
  p,err:=s.health.Plans(ctx);if err!=nil{return nil,err};return send(p)
 })
 s.add("health_workout_pending","Find scheduled workouts that are due but lack a matching log or an explicit completion decision. A missing log is not evidence of a missed workout.",true,false,[]string{"plan_id"},
 map[string]any{"plan_id":txt("Workout plan ID"),"as_of":txt("Optional RFC3339 reference time")},
 func(ctx context.Context,a map[string]json.RawMessage)(json.RawMessage,error){
  id,err:=requiredString(a,"plan_id");if err!=nil{return nil,err}
  at,err:=dateOrNow(a["as_of"]);if err!=nil{return nil,err}
  pending,err:=s.health.Pending(ctx,id,at);if err!=nil{return nil,err};return send(pending)
 })
 s.add("health_workout_decide","Confirm whether a scheduled session was completed, missed, or intentionally skipped. Never classify an unlogged workout as missed automatically.",false,false,[]string{"plan_id","session_id","state","confirm"},
 map[string]any{"plan_id":txt("Workout plan ID"),"session_id":txt("Session ID"),
 "state":selectField("User-confirmed session outcome","completed","missed","skipped"),
 "confirm":field("boolean","Must be true after asking the user about the missing log")},
 func(ctx context.Context,a map[string]json.RawMessage)(json.RawMessage,error){
  if err:=confirmed(a);err!=nil{return nil,errors.New("session status changes require confirm=true")}
  pid,err:=requiredString(a,"plan_id");if err!=nil{return nil,err}
  sid,err:=requiredString(a,"session_id");if err!=nil{return nil,err}
  state,err:=requiredString(a,"state");if err!=nil{return nil,err}
  if err=s.health.DecideSession(ctx,pid,sid,state);err!=nil{return nil,err}
  return send(map[string]string{"plan_id":pid,"session_id":sid,"state":state})
 })
 s.add("health_vo2_estimate","Estimate VO2 max from a timed maximal field test. This is not a laboratory measurement; report the test protocol and uncertainty.",true,false,[]string{"test","value"},
 map[string]any{"test":selectField("Validated input protocol","cooper_12min","run_1_5mile"),"value":field("number","Meters covered in 12 minutes, or minutes to complete 1.5 miles")},
 func(ctx context.Context,a map[string]json.RawMessage)(json.RawMessage,error){
  test,err:=requiredString(a,"test");if err!=nil{return nil,err}
  var v float64
  if json.Unmarshal(a["value"],&v)!=nil || math.IsNaN(v)||math.IsInf(v,0)||v<=0{return nil,errors.New("value must be positive and finite")}
  var estimate float64;var protocol string
  switch test {
  case "cooper_12min":
   if v<400||v>5000{return nil,errors.New("distance outside supported field-test range")}
   estimate=(v-504.9)/44.73;protocol="Cooper 12-minute maximal run (distance in meters)"
  case "run_1_5mile":
   if v<5||v>40{return nil,errors.New("time outside supported field-test range")}
   estimate=3.5+483/v;protocol="1.5-mile maximal run (time in minutes)"
  default:return nil,errors.New("unsupported test")
  }
  return send(map[string]any{"vo2_max_ml_kg_min":math.Round(estimate*10)/10,"method":"estimated",
   "protocol":protocol,"confidence":"field-test estimate; individual error varies",
   "note":"Do not substitute this estimate for a clinical exercise test or infer a precise value from resting heart rate alone."})
 })
 s.add("health_sleep_debt","Calculate sleep shortfall only over days with recorded sleep. Missing days remain unknown, not zero.",true,false,[]string{"target_hours"},
 map[string]any{"target_hours":field("number","Personal sleep target per recorded day"),"days":field("integer","Calendar days to examine, 1 to 30")},
 func(ctx context.Context,a map[string]json.RawMessage)(json.RawMessage,error){
  var target float64
  if json.Unmarshal(a["target_hours"],&target)!=nil||target<=0||target>24{return nil,errors.New("target_hours must be between 0 and 24")}
  days,err:=boundedInt(a,"days",7,1,30);if err!=nil{return nil,err}
  records,err:=s.health.List(ctx,"sleep","duration_hours",500,false);if err!=nil{return nil,err}
  now:=time.Now().UTC()
  dates:=map[string]float64{}
  for _,r:=range records {
   if r.At.After(now)||r.At.Before(now.AddDate(0,0,-days)){continue}
   key:=r.At.Format("2006-01-02")
   dates[key]+=r.Value
  }
  deficit:=0.0;total:=0.0
  for _,v:=range dates{total+=v;if v<target{deficit+=target-v}}
  return send(map[string]any{"observed_days":len(dates),"requested_days":days,"sleep_hours":total,
   "estimated_shortfall_hours":math.Round(deficit*100)/100,"method":"descriptive",
   "missing_days":"not treated as zero","note":"This is a descriptive shortfall, not a physiological diagnosis of accumulated sleep debt."})
 })
 s.add("health_metric_trend","Return chronological values for charting and inspection, preserving sources and units.",true,false,[]string{"domain","metric"},
 map[string]any{"domain":txt("Domain"),"metric":txt("Metric"),"limit":field("integer","1 to 500")},
 func(ctx context.Context,a map[string]json.RawMessage)(json.RawMessage,error){
  d,err:=requiredString(a,"domain");if err!=nil{return nil,err}
  m,err:=requiredString(a,"metric");if err!=nil{return nil,err}
  n,err:=boundedInt(a,"limit",100,1,500);if err!=nil{return nil,err}
  list,err:=s.health.List(ctx,d,m,n,false);if err!=nil{return nil,err}
  return send(map[string]any{"domain":d,"metric":m,"points":list,"count":len(list)})
 })
 s.add("health_workout_log","Save a completed session with a link to its planned identity. A real log prevents false missed-session classification.",false,false,[]string{"plan_id","session_id","at","confirm"},
 map[string]any{"plan_id":txt("Plan ID"),"session_id":txt("Session ID"),
 "confirm":field("boolean","Must be true after confirming session completion"),
 "at":txt("RFC3339 completion time"),"notes":txt("Session observations"),"duration_minutes":field("number","Elapsed session minutes")},
 func(ctx context.Context,a map[string]json.RawMessage)(json.RawMessage,error){
  pid,err:=requiredString(a,"plan_id");if err!=nil{return nil,err}
  sid,err:=requiredString(a,"session_id");if err!=nil{return nil,err}
  if err:=confirmed(a);err!=nil{return nil,errors.New("confirm=true is required for completed workout")}
  plan,err:=s.health.GetPlan(ctx,pid);if err!=nil{return nil,err}
  if plan.Kind!="workout"{return nil,errors.New("plan is not a workout")}
  scheduledDate:=""
  for _,session:=range plan.Sessions{if session.ID==sid{scheduledDate=session.Date;break}}
  if scheduledDate==""{return nil,errors.New("session does not exist in workout plan")}
  at,err:=dateOrNow(a["at"]);if err!=nil{return nil,err}
  var duration float64
  if len(a["duration_minutes"])>0 && json.Unmarshal(a["duration_minutes"],&duration)!=nil{return nil,errors.New("duration_minutes must be numeric")}
  if duration<0||duration>1440{return nil,errors.New("duration out of range")}
  metadata,_:=json.Marshal(map[string]any{"plan_id":pid,"session_id":sid,"scheduled_date":scheduledDate,"notes":optionalString(a,"notes","")})
  rec,err:=s.health.Add(ctx,health.Record{Domain:"workout",Metric:"session",Value:duration,
   Unit:"minutes",At:at,Source:"workout_session",SourceID:pid+":"+sid+":"+scheduledDate,
   Method:"reported",Metadata:metadata})
  if err!=nil{return nil,err}
  return send(rec)
 })
 s.add("health_nutrition_log","Save food nutrients as measured or estimated observations. Only provide nutrient values supported by the product label, database, recipe, or explicitly identified estimation.",false,false,
 []string{"food","nutrients","units","method"},
 map[string]any{"food":txt("Food name"),"nutrients":map[string]any{"type":"object","additionalProperties":map[string]any{"type":"number"},"description":"Nutrient key/value map; omit unknown nutrients"},
 "units":map[string]any{"type":"object","additionalProperties":map[string]any{"type":"string"},"description":"Unit per nutrient"},
 "method":selectField("Evidence type","measured","estimated","reported"),
 "at":txt("Optional RFC3339 time"),"source":txt("Food label, nutrition database, recipe, or visual estimate"),"notes":txt("Portion size and evidence")},
 func(ctx context.Context,a map[string]json.RawMessage)(json.RawMessage,error){
  food,err:=requiredString(a,"food");if err!=nil{return nil,err}
  var nutrients map[string]float64
  var units map[string]string
  if json.Unmarshal(a["nutrients"],&nutrients)!=nil||len(nutrients)==0||len(nutrients)>150{return nil,errors.New("nutrients must be a nonempty numeric map of at most 150 entries")}
  if json.Unmarshal(a["units"],&units)!=nil{return nil,errors.New("units must be a map")}
  method,err:=requiredString(a,"method");if err!=nil{return nil,err}
  at,err:=dateOrNow(a["at"]);if err!=nil{return nil,err}
  mealID,err:=health.NewID();if err!=nil{return nil,err}
  source:=optionalString(a,"source","manual")
  if source==""{source="manual"}
  meta,_:=json.Marshal(map[string]any{"food":food,"meal_id":mealID,"notes":optionalString(a,"notes","")})
  records:=make([]health.Record,0,len(nutrients))
  for key,value:=range nutrients{
   if strings.TrimSpace(units[key])==""{return nil,fmt.Errorf("unit missing for %s",key)}
   records=append(records,health.Record{Domain:"nutrition",Metric:key,Value:value,Unit:units[key],
    At:at,Source:source,Method:method,Metadata:meta})
  }
  saved,err:=s.health.AddMany(ctx,records);if err!=nil{return nil,err}
  return send(map[string]any{"meal_id":mealID,"food":food,"measurements":saved})
 })
}
