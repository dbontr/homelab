package fooddata

import(
 "encoding/json"
 "math"
 "testing"
)
func TestBarcodePortionConversionAndMissingNutrients(t *testing.T){
 raw:=json.RawMessage(`{"product":{"product_name":"Example yogurt","nutriments":{
  "proteins_100g":5.8,"vitamin-c_100g":0.003,"vitamin-c_unit":"mg",
  "fat_100g":2,"energy-kcal_100g":89,"unavailable_nutrient_unit":"g"
 }}}`)
 preview,err:=ExtractBarcodeNutrients(raw,"3017620422003",150,"g")
 if err!=nil{t.Fatal(err)}
 if preview.ProductName!="Example yogurt"{t.Fatal("wrong product name")}
 if math.Abs(preview.Nutrients["protein"].Value-8.7)>1e-9{t.Fatalf("protein wrong: %+v",preview)}
 if math.Abs(preview.Nutrients["vitamin_c"].Value-4.5)>1e-9{
  t.Fatalf("vitamin c unit conversion wrong: %+v",preview)
 }
 if _,exists:=preview.Nutrients["vitamin_a_rae"];exists{t.Fatal("fabricated absent micronutrient")}
 if _,err:=ExtractBarcodeNutrients(raw,"3017620422003",-10,"g");err==nil{t.Fatal("negative portion accepted")}
 if _,err:=ExtractBarcodeNutrients(raw,"3017620422003",150,"cups");err==nil{t.Fatal("unknown basis accepted")}
}
