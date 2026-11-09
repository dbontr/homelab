package fooddata

import (
 "context"
 "encoding/json"
 "errors"
 "math"
 "net/http"
 "net/url"
 "strings"
 "testing"
)

func usdaFood(id int64,protein *float64,vitaminC *float64)json.RawMessage{
 entries:=[]map[string]any{
  {"nutrient":map[string]any{"id":1008,"unitName":"kcal"},"amount":200},
  {"nutrient":map[string]any{"id":2047,"unitName":"KCAL"},"amount":210},
 }
 if protein!=nil{entries=append(entries,map[string]any{
  "nutrient":map[string]any{"id":1003,"unitName":"G"},"amount":*protein,
 })}
 if vitaminC!=nil{entries=append(entries,map[string]any{
  "nutrient":map[string]any{"id":1162,"unitName":"MG"},"amount":*vitaminC,
 })}
 data,_:=json.Marshal(map[string]any{
  "fdcId":id,"description":"Measured food","dataType":"Foundation",
  "foodNutrients":entries,
 })
 return data
}
func double(value float64)*float64{return &value}

func TestRecipeEstimateKeepsUnknownMicronutrientsIncomplete(t *testing.T){
 raw:=[]json.RawMessage{usdaFood(101,double(20),double(30)),usdaFood(202,double(10),nil)}
 items:=[]USDAIngredient{
  {IngredientIndex:0,FDCID:101,Grams:100},
  {IngredientIndex:1,FDCID:202,Grams:50},
 }
 got,err:=EstimateRecipeNutrients(raw,items,2)
 if err!=nil{t.Fatal(err)}
 if len(got.Ingredients)!=2||len(got.SHA256)!=64{
  t.Fatalf("source evidence missing: %+v",got)
 }
 known:=map[string]float64{}
 for _,entry:=range got.PerServing{known[entry.Name]=entry.Amount}
 if known["protein"]!=12.5||known["energy_kcal"]!=157.5{
  t.Fatalf("per-serving USDA quantity incorrect: %+v",known)
 }
 if _,ok:=known["vitamin_c"];ok{t.Fatal("unknown micronutrient became an invented measurement")}
 if len(got.Incomplete)!=1||got.Incomplete[0]!="vitamin_c"{
  t.Fatalf("missing nutrient was suppressed: %+v",got.Incomplete)
 }
 if got.Source!="USDA FoodData Central"||got.Method!="estimated"{
  t.Fatalf("composition was represented as measured: %+v",got)
 }
 again,err:=EstimateRecipeNutrients(raw,items,2)
 if err!=nil||again.SHA256!=got.SHA256{t.Fatal("preview identity is nondeterministic")}
}
func TestRecipeEstimateRejectsUnsafeMappingsAndMismatchedFood(t *testing.T){
 raw:=[]json.RawMessage{usdaFood(101,double(20),nil),usdaFood(202,double(10),nil)}
 candidates:=[][]USDAIngredient{
  {{IngredientIndex:0,FDCID:101,Grams:100},{IngredientIndex:0,FDCID:202,Grams:50}},
  {{IngredientIndex:0,FDCID:101,Grams:math.NaN()},{IngredientIndex:1,FDCID:202,Grams:50}},
  {{IngredientIndex:0,FDCID:101,Grams:100},{IngredientIndex:1,FDCID:202,Grams:-5}},
  {{IngredientIndex:0,FDCID:101,Grams:100},{IngredientIndex:1,FDCID:404,Grams:50}},
 }
 for _,items:=range candidates{
  if _,err:=EstimateRecipeNutrients(raw,items,2);err==nil{
   t.Fatalf("invalid mapping was accepted: %+v",items)
  }
 }
 if _,err:=EstimateRecipeNutrients(raw,[]USDAIngredient{{IngredientIndex:0,FDCID:101,Grams:100}},0);err==nil{
  t.Fatal("zero yield was accepted")
 }
}
type failFoodTransport struct{}
func (failFoodTransport)RoundTrip(r *http.Request)(*http.Response,error){
 return nil,&url.Error{Op:"Get",URL:r.URL.String(),Err:errors.New("offline")}
}
func TestUSDANetworkErrorDoesNotExposeAPIKey(t *testing.T){
 secret:="never-output-this-api-key"
 client:=New(Config{USDAKey:secret,HTTP:&http.Client{Transport:failFoodTransport{}}})
 _,err:=client.USDADetail(context.Background(),101)
 if err==nil||strings.Contains(err.Error(),secret){
  t.Fatalf("USDA query leaked credential: %v",err)
 }
}
