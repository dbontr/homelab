package mcp

import (
 "context"
 "encoding/json"
 "errors"

 "github.com/dbontr/personal-health-mcp/internal/fooddata"
)
func(s *Server)EnableFoodData(client *fooddata.Client){
 if client==nil{return}
 s.food=client
 s.registerFoodTools()
}
func(s *Server)registerFoodTools(){
 txt:=func(desc string)map[string]any{return field("string",desc)}
 s.add("health_food_barcode","Look up a packaged food by barcode with Open Food Facts v3. Nutrient facts describe the source record and may be incomplete.",true,false,[]string{"barcode"},map[string]any{
  "barcode":txt("8 to 14 numeric digits")},
 func(ctx context.Context,a map[string]json.RawMessage)(json.RawMessage,error){
  code,err:=requiredString(a,"barcode");if err!=nil{return nil,err}
  return s.food.Barcode(ctx,code)
 })
 s.add("health_food_barcode_preview","Convert known Open Food Facts per-100g/per-100mL nutrients into a portion preview, preserving unknown micronutrients as missing. Does not save food intake.",true,false,
 []string{"barcode","quantity","basis"},
 map[string]any{"barcode":txt("8 to 14 digit barcode"),
 "quantity":field("number","Portion quantity in grams or milliliters"),
 "basis":selectField("Confirmed label unit","g","mL")},
 func(ctx context.Context,a map[string]json.RawMessage)(json.RawMessage,error){
  barcode,err:=requiredString(a,"barcode");if err!=nil{return nil,err}
  var amount float64
  if err=json.Unmarshal(a["quantity"],&amount);err!=nil{return nil,errors.New("quantity must be numeric")}
  basis,err:=requiredString(a,"basis");if err!=nil{return nil,err}
  raw,err:=s.food.Barcode(ctx,barcode);if err!=nil{return nil,err}
  result,err:=fooddata.ExtractBarcodeNutrients(raw,barcode,amount,basis)
  if err!=nil{return nil,err}
  return send(result)
 })
 s.add("health_food_search","Search USDA FoodData Central for food compositions and available micronutrients. A USDA API key is required.",true,false,[]string{"query"},
 map[string]any{"query":txt("Food or ingredient name"),"limit":field("integer","Maximum 25, default 10")},
 func(ctx context.Context,a map[string]json.RawMessage)(json.RawMessage,error){
  query,err:=requiredString(a,"query");if err!=nil{return nil,err}
  limit,err:=boundedInt(a,"limit",10,1,25);if err!=nil{return nil,err}
  return s.food.USDA(ctx,query,limit)
 })
 s.add("health_food_details","Read the full USDA FoodData Central nutrient record for an FDC food identifier.",true,false,[]string{"fdc_id"},map[string]any{
  "fdc_id":field("integer","USDA FoodData Central integer identifier")},
 func(ctx context.Context,a map[string]json.RawMessage)(json.RawMessage,error){
  var id int64
  if err:=json.Unmarshal(a["fdc_id"],&id);err!=nil||id<=0{return nil,errors.New("fdc_id must be a positive integer")}
  return s.food.USDADetail(ctx,id)
 })
}
