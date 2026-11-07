package mcp

import(
 "context"
 "encoding/json"

 "github.com/dbontr/personal-health-mcp/internal/health"
)
func(s *Server)registerNutrientCatalog(){
 s.add("health_nutrient_catalog","List supported canonical nutrient identifiers and suggested units. No dietary targets or intake requirements are inferred.",true,false,nil,map[string]any{},
 func(ctx context.Context,a map[string]json.RawMessage)(json.RawMessage,error){
  return send(health.NutrientCatalog())
 })
}
