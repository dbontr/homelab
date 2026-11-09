package mcp

import (
 "context"
 _ "embed"
 "encoding/json"
)

const cognitiveUIURI="ui://personal-health-mcp/cognitive-test.html"

//go:embed cognitive_test.html
var cognitiveHTML string

func (s *Server) registerCognitiveTest(){
 s.add("health_cognitive_test","Open an interactive simple visual reaction-time task. Results are exploratory device-timing estimates, not clinical cognitive measurements.",true,false,nil,map[string]any{},
 func(ctx context.Context,a map[string]json.RawMessage)(json.RawMessage,error){
  observations,err:=s.health.List(ctx,"cognition","simple_reaction_ms",20,false)
  if err!=nil{return nil,err}
  return send(map[string]any{"view":"cognitive_test","history":observations})
 })
 entry:=s.tools["health_cognitive_test"]
 entry.Meta=map[string]any{
  "ui":map[string]any{"resourceUri":cognitiveUIURI,"visibility":[]string{"model","app"}},
  "ui/resourceUri":cognitiveUIURI,
 }
 s.tools["health_cognitive_test"]=entry
}
