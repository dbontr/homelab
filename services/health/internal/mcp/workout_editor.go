package mcp

import (
 "context"
 _ "embed"
 "encoding/json"
)

const healthUIURI="ui://personal-health-mcp/workout-editor.html"

//go:embed workout_editor.html
var workoutHTML string

func (s *Server) registerWorkoutEditor(){
 s.add("health_workout_editor","Open an interactive workout-plan editor inside a compatible MCP Apps host. Plans are versioned, and saves reject conflicting revisions.",true,false,nil,map[string]any{},
 func(ctx context.Context,a map[string]json.RawMessage)(json.RawMessage,error){
  plans,err:=s.health.Plans(ctx);if err!=nil{return nil,err}
  filtered:=make([]any,0,len(plans))
  for _,p:=range plans{if p.Kind=="workout"{filtered=append(filtered,p)}}
  return send(map[string]any{"view":"workout_editor","plans":filtered})
 })
 entry:=s.tools["health_workout_editor"]
 entry.Meta=map[string]any{
  "ui":map[string]any{"resourceUri":healthUIURI,"visibility":[]string{"model","app"}},
  "ui/resourceUri":healthUIURI,
 }
 s.tools["health_workout_editor"]=entry
}
