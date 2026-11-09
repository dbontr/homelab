package mcp

import(
 "context"
 _ "embed"
 "encoding/json"
)

const groceryUIURI="ui://personal-health-mcp/groceries.html"

//go:embed grocery_editor.html
var groceryHTML string

func(s *Server)registerGroceryEditor(){
 s.add("health_grocery_editor","Open the interactive grocery-list editor in ChatGPT. Aggregate meal-plan ingredients, edit quantities and mark purchased items.",true,false,nil,map[string]any{},
 func(ctx context.Context,a map[string]json.RawMessage)(json.RawMessage,error){
  lists,err:=s.health.ListGroceries(ctx);if err!=nil{return nil,err}
  plans,err:=s.health.Plans(ctx);if err!=nil{return nil,err}
  return send(map[string]any{"view":"grocery_editor","lists":lists,"plans":plans})
 })
 entry:=s.tools["health_grocery_editor"]
 entry.Meta=map[string]any{
  "ui":map[string]any{"resourceUri":groceryUIURI,"visibility":[]string{"model","app"}},
  "ui/resourceUri":groceryUIURI,
 }
 s.tools["health_grocery_editor"]=entry
}
