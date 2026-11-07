package mcp

import(
 "context"
 _ "embed"
 "encoding/json"
)

const dashboardURI="ui://personal-health-mcp/dashboard.html"

//go:embed dashboard.html
var dashboardHTML string

func(s *Server)registerDashboard(){
 s.add("health_dashboard","Open an interactive health dashboard with metric graphs, nutrient comparisons and editable targets.",true,false,nil,map[string]any{},
 func(ctx context.Context,a map[string]json.RawMessage)(json.RawMessage,error){
  goals,err:=s.health.Goals(ctx);if err!=nil{return nil,err}
  return send(map[string]any{"view":"health_dashboard","goals":goals})
 })
 entry:=s.tools["health_dashboard"]
 entry.Meta=map[string]any{"ui":map[string]any{"resourceUri":dashboardURI,"visibility":[]string{"model","app"}},
  "ui/resourceUri":dashboardURI}
 s.tools["health_dashboard"]=entry
}
