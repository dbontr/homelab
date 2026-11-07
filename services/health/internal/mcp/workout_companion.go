package mcp

import(
 "context"
 _ "embed"
 "encoding/json"
)

const companionUIURI="ui://personal-health-mcp/workout-companion.html"

//go:embed workout_companion.html
var companionHTML string

func(s *Server)registerWorkoutCompanion(){
 s.add("health_workout_companion","Open a workout session inside ChatGPT. Log each strength set or cardio interval, manage rest timers, and explicitly complete the session.",true,false,nil,map[string]any{},
 func(ctx context.Context,a map[string]json.RawMessage)(json.RawMessage,error){
  all,err:=s.health.Plans(ctx);if err!=nil{return nil,err}
  workouts:=make([]any,0,len(all))
  for _,p:=range all{if p.Kind=="workout"{workouts=append(workouts,p)}}
  return send(map[string]any{"view":"workout_companion","plans":workouts})
 })
 entry:=s.tools["health_workout_companion"]
 entry.Meta=map[string]any{"ui":map[string]any{"resourceUri":companionUIURI,"visibility":[]string{"model","app"}},
  "ui/resourceUri":companionUIURI}
 s.tools["health_workout_companion"]=entry
}
