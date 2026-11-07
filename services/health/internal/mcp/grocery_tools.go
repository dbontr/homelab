package mcp

import(
 "context"
 "encoding/json"
 "errors"

 "github.com/dbontr/personal-health-mcp/internal/health"
)
func(s *Server)registerGroceryTools(){
 txt:=func(d string)map[string]any{return field("string",d)}
 items:=map[string]any{"type":"array","items":map[string]any{
 "type":"object","properties":map[string]any{
 "id":txt("Unique item ID"),"name":txt("Food or ingredient"),
 "quantity":field("number","Positive quantity"),"unit":txt("Unit"),
 "checked":field("boolean","Whether item has been purchased"),
 "recipe_id":txt("Optional RecipeSage recipe ID")},
 "required":[]string{"id","name","quantity","unit"}}}
 s.add("health_grocery_from_plan","Group meal-plan ingredients by name, unit and recipe reference into a proposed shopping list. Does not infer RecipeSage recipe ingredients.",true,false,[]string{"plan_id"},
 map[string]any{"plan_id":txt("Meal plan ID")},
 func(ctx context.Context,a map[string]json.RawMessage)(json.RawMessage,error){
  id,err:=requiredString(a,"plan_id");if err!=nil{return nil,err}
  entries,err:=s.health.GroceryFromPlan(ctx,id);if err!=nil{return nil,err}
  return send(entries)
 })
 s.add("health_grocery_save","Create or revise a local grocery list with revision conflict detection. A RecipeSage shopping list can be managed separately.",false,false,[]string{"title","items","expected_revision"},
 map[string]any{"id":txt("Existing list ID for updates"),"title":txt("Grocery list name"),
  "items":items,"expected_revision":field("integer","0 for creation; current revision for editing")},
 func(ctx context.Context,a map[string]json.RawMessage)(json.RawMessage,error){
  var list health.GroceryList
  if err:=decodeFields(a,&list);err!=nil{return nil,err}
  var revision int
  if json.Unmarshal(a["expected_revision"],&revision)!=nil{return nil,errors.New("expected_revision must be an integer")}
  result,err:=s.health.SaveGrocery(ctx,list,revision);if err!=nil{return nil,err}
  return send(result)
 })
 s.add("health_grocery_get","Read a grocery list including purchase status and revision.",true,false,[]string{"id"},
 map[string]any{"id":txt("Grocery list ID")},
 func(ctx context.Context,a map[string]json.RawMessage)(json.RawMessage,error){
  id,err:=requiredString(a,"id");if err!=nil{return nil,err}
  list,err:=s.health.GetGrocery(ctx,id);if err!=nil{return nil,err}
  return send(list)
 })
 s.add("health_grocery_list","List all active local grocery lists.",true,false,nil,map[string]any{},
 func(ctx context.Context,a map[string]json.RawMessage)(json.RawMessage,error){
  lists,err:=s.health.ListGroceries(ctx);if err!=nil{return nil,err}
  return send(lists)
 })
}
