package mcp

import(
 "context"
 "encoding/json"
 "errors"
 "math"

 "github.com/dbontr/personal-health-mcp/internal/health"
)
func(s *Server)registerTrainingTools(){
 txt:=func(d string)map[string]any{return field("string",d)}
 s.add("health_training_set_log","Record a strength set, cardio interval or mobility activity with actual training values. Records retain correction history.",false,false,
 []string{"exercise","activity","at"},
 map[string]any{
 "plan_id":txt("Optional linked workout plan ID"),"session_id":txt("Linked planned session ID"),
 "exercise":txt("Exercise name"),"activity":selectField("Activity category","strength","cardio","mobility"),
 "set_number":field("integer","One-based set index"),"repetitions":field("integer","Performed repetitions"),
 "load_kg":field("number","Weight, kilograms"),"duration_seconds":field("number","Elapsed seconds"),
 "distance_meters":field("number","Distance in meters"),"rpe":field("number","Reported exertion from 0 to 10"),
 "at":txt("RFC3339 time"),"source":txt("Source, defaults to manual"),
 "client_key":txt("Optional unique client entry ID preventing repeated imports"),
 "notes":txt("Notes"),"replaces":txt("ID of a training entry to correct")},
 func(ctx context.Context,a map[string]json.RawMessage)(json.RawMessage,error){
  var record health.TrainingSet
  if err:=decodeFields(a,&record);err!=nil{return nil,err}
  if record.Source==""{record.Source="manual"}
  saved,err:=s.health.AddTrainingSet(ctx,record);if err!=nil{return nil,err}
  return send(saved)
 })
 s.add("health_training_set_list","Read measured or self-reported strength sets, cardio intervals and mobility logs.",true,false,nil,
 map[string]any{"plan_id":txt("Optional plan ID"),"exercise":txt("Optional exact exercise name"),
 "limit":field("integer","1 to 500, default 100"),"include_history":field("boolean","Include superseded entries")},
 func(ctx context.Context,a map[string]json.RawMessage)(json.RawMessage,error){
  n,err:=boundedInt(a,"limit",100,1,500);if err!=nil{return nil,err}
  var history bool
  if len(a["include_history"])>0{
   if err=json.Unmarshal(a["include_history"],&history);err!=nil{return nil,errors.New("include_history must be boolean")}
  }
  entries,err:=s.health.ListTrainingSets(ctx,optionalString(a,"plan_id",""),optionalString(a,"exercise",""),n,history)
  if err!=nil{return nil,err}
  return send(entries)
 })
 s.add("health_training_progress","Summarize logged strength training volume and an estimated one-repetition maximum for an exercise. An estimate is not a measured maximum.",true,false,[]string{"exercise"},
 map[string]any{"exercise":txt("Exact exercise name"),"limit":field("integer","Number of sets, default 100, at most 500")},
 func(ctx context.Context,a map[string]json.RawMessage)(json.RawMessage,error){
  name,err:=requiredString(a,"exercise");if err!=nil{return nil,err}
  n,err:=boundedInt(a,"limit",100,1,500);if err!=nil{return nil,err}
  sets,err:=s.health.ListTrainingSets(ctx,"",name,n,false);if err!=nil{return nil,err}
  result:=map[string]any{"exercise":name,"sample_count":len(sets),
    "method":"descriptive logged volume and Epley one-repetition estimate for sets of 1–10 repetitions"}
  volume:=0.0;maxEstimated:=0.0;cardioMeters:=0.0;cardioSeconds:=0.0
  for _,entry:=range sets{
   if entry.Activity=="strength"&&entry.LoadKG!=nil&&entry.Repetitions>0{
    volume+=*entry.LoadKG*float64(entry.Repetitions)
    if entry.Repetitions<=10{
     estimate:=*entry.LoadKG*(1+float64(entry.Repetitions)/30)
     if estimate>maxEstimated{maxEstimated=estimate}
    }
   }
   if entry.Activity=="cardio"{
    if entry.DistanceMeters!=nil{cardioMeters+=*entry.DistanceMeters}
    if entry.DurationSeconds!=nil{cardioSeconds+=*entry.DurationSeconds}
   }
  }
  result["total_recorded_volume_kg_reps"]=volume;result["cardio_meters"]=cardioMeters
  result["cardio_seconds"]=cardioSeconds
  if maxEstimated>0{
   result["estimated_one_rep_max_kg"]=math.Round(maxEstimated*10)/10
   result["estimate_warning"]="Technique, fatigue, and training proximity to failure alter accuracy"
  }
  result["evidence"]="Logged sets only; incomplete logs reduce coverage"
  return send(result)
 })
}
