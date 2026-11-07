package mcp

import(
 "context"
 "encoding/json"
 "errors"

 "github.com/dbontr/personal-health-mcp/internal/health"
)
func(s *Server)registerFoodProposalTools(){
 txt:=func(d string)map[string]any{return field("string",d)}
 s.add("health_food_propose","Save a food-photo, barcode, recipe or manually inferred meal as an unconfirmed proposal. No nutrition measurements enter the daily log until explicitly confirmed.",false,false,
 []string{"food","portion","origin","nutrients","units","observed_at"},
 map[string]any{
 "food":txt("Identified food name"),"portion":txt("Human-readable quantity and portion evidence"),
 "origin":selectField("Inference origin","photo_estimate","barcode_label","recipe","manual_estimate"),
 "nutrients":map[string]any{"type":"object","additionalProperties":map[string]any{"type":"number"},
  "description":"Only supported nutrient values; unknowns must be omitted"},
 "units":map[string]any{"type":"object","additionalProperties":map[string]any{"type":"string"}},
 "observed_at":txt("RFC3339 meal time"),
 "evidence_id":txt("Optional uploaded evidence ID"),
 "notes":txt("Estimation method, portion limitations, and known missing values")},
 func(ctx context.Context,a map[string]json.RawMessage)(json.RawMessage,error){
  var p health.FoodProposal
  if err:=decodeFields(a,&p);err!=nil{return nil,err}
  proposal,err:=s.health.SaveFoodProposal(ctx,p)
  if err!=nil{return nil,err}
  return send(proposal)
 })
 s.add("health_food_proposal_list","List proposed or confirmed food entries before choosing what to save as nutrition measurements.",true,false,nil,
 map[string]any{"status":selectField("Filter","pending","confirmed","discarded"),
 "limit":field("integer","1 to 100, default 25")},
 func(ctx context.Context,a map[string]json.RawMessage)(json.RawMessage,error){
  n,err:=boundedInt(a,"limit",25,1,100);if err!=nil{return nil,err}
  items,err:=s.health.ListFoodProposals(ctx,optionalString(a,"status",""),n)
  if err!=nil{return nil,err}
  return send(items)
 })
 s.add("health_food_proposal_get","Read an individual food inference proposal and its evidence.",true,false,
 []string{"id"},map[string]any{"id":txt("Food proposal ID")},
 func(ctx context.Context,a map[string]json.RawMessage)(json.RawMessage,error){
  id,err:=requiredString(a,"id");if err!=nil{return nil,err}
  item,err:=s.health.GetFoodProposal(ctx,id);if err!=nil{return nil,err}
  return send(item)
 })
 s.add("health_food_confirm","Explicitly confirm a proposed meal and add its observed nutrients to the daily log. Confirmation is idempotent.",false,false,
 []string{"id","confirm"},
 map[string]any{"id":txt("Food proposal ID"),"confirm":field("boolean","Must be true after reviewing identity, portion and nutrients")},
 func(ctx context.Context,a map[string]json.RawMessage)(json.RawMessage,error){
  if err:=confirmed(a);err!=nil{return nil,errors.New("food proposal requires confirm=true")}
  id,err:=requiredString(a,"id");if err!=nil{return nil,err}
  count,err:=s.health.ConfirmFoodProposal(ctx,id);if err!=nil{return nil,err}
  return send(map[string]any{"id":id,"new_measurements":count,"confirmed":true})
 })
 s.add("health_food_discard","Discard an unconfirmed food proposal without adding nutrition entries.",false,false,[]string{"id","confirm"},
 map[string]any{"id":txt("Food proposal ID"),"confirm":field("boolean","Must be true")},
 func(ctx context.Context,a map[string]json.RawMessage)(json.RawMessage,error){
  if err:=confirmed(a);err!=nil{return nil,errors.New("discard requires confirm=true")}
  id,err:=requiredString(a,"id");if err!=nil{return nil,err}
  if err=s.health.DiscardFoodProposal(ctx,id);err!=nil{return nil,err}
  return send(map[string]any{"id":id,"status":"discarded"})
 })
}
