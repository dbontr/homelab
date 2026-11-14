package transport

import (
 "crypto/sha256"
 "encoding/hex"
 "encoding/json"
 "net/http"
 "net/http/httptest"
 "path/filepath"
 "strings"
 "testing"

 "github.com/dbontr/homelab-new/system/recipes"
 "github.com/dbontr/homelab-new/system/store"
)

func TestUSDAMappingRequiresAllIngredientsAndCurrentRevision(t *testing.T){
 record:=recipes.Recipe{
  ID:"recipe-one",Title:"Supper",Revision:3,Servings:2,
  Ingredients:[]recipes.Ingredient{{Name:"Rice",Amount:100,Unit:"g"},{Name:"Beans",Amount:100,Unit:"g"}},
 }
 valid:=USDARecipeRequest{ExpectedRevision:3,Ingredients:[]USDAIngredientSelection{
  {IngredientIndex:0,FDCID:101,Grams:100},{IngredientIndex:1,FDCID:202,Grams:100},
 }}
 if err:=validateUSDASelection(record,valid);err!=nil{t.Fatal(err)}
 for _,changed:=range []USDARecipeRequest{
  {ExpectedRevision:2,Ingredients:valid.Ingredients},
  {ExpectedRevision:3,Ingredients:valid.Ingredients[:1]},
  {ExpectedRevision:3,Ingredients:[]USDAIngredientSelection{{IngredientIndex:0,FDCID:101,Grams:100},{IngredientIndex:0,FDCID:202,Grams:100}}},
  {ExpectedRevision:3,Ingredients:[]USDAIngredientSelection{{IngredientIndex:0,FDCID:101,Grams:100},{IngredientIndex:1,FDCID:202,Grams:0}}},
 } {
  if validateUSDASelection(record,changed)==nil{
   t.Fatalf("unsafe ingredient mapping was accepted: %+v",changed)
  }
 }
}

func TestUSDARecipeReviewPersistsOnlyAfterDigestConfirmation(t *testing.T){
 health:=httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter,r *http.Request){
  var call struct{
   ID json.RawMessage `json:"id"`
   Method string `json:"method"`
   Params struct {
    Name string `json:"name"`
    Arguments map[string]any `json:"arguments"`
   } `json:"params"`
  }
  if json.NewDecoder(r.Body).Decode(&call)!=nil{http.Error(w,"bad request",400);return}
  var payload any
  switch call.Method {
  case "tools/list":
   payload=map[string]any{"tools":[]any{map[string]any{
    "name":"health_food_recipe_estimate","inputSchema":map[string]any{"type":"object"},
   }}}
  case "tools/call":
   if call.Params.Name!="health_food_recipe_estimate"{http.Error(w,"unknown tool",400);return}
   est:=usdaRecipeEstimate{
    Method:"estimated",Source:"USDA FoodData Central",Servings:2,
    PerServing:[]recipes.Nutrient{{Name:"protein",Amount:10,Unit:"g"}},
    Incomplete:[]string{"vitamin_c"},
    Ingredients:[]usdaEvidence{{
     IngredientIndex:0,FDCID:101,Description:"Beans, cooked",DataType:"Foundation",Grams:200,
    }},
    Limitations:"Owner-confirmed edible gram quantity; unreported nutrients unknown.",
   }
   raw,_:=json.Marshal(est)
   digest:=sha256.Sum256(raw)
   est.SHA256=hex.EncodeToString(digest[:])
   encoded,_:=json.Marshal(est)
   payload=map[string]any{"content":[]any{map[string]any{"type":"text","text":string(encoded)}}}
  default: http.Error(w,"unknown method",400);return
  }
  w.Header().Set("Content-Type","application/json")
  _=json.NewEncoder(w).Encode(map[string]any{"jsonrpc":"2.0","id":call.ID,"result":payload})
 }))
 defer health.Close()
 state,err:=store.Open(filepath.Join(t.TempDir(),"state.json"),"UTC")
 if err!=nil{t.Fatal(err)}
 s,err:=New(state,strings.Repeat("t",40),[]Backend{{Name:"health",URL:health.URL}})
 if err!=nil{t.Fatal(err)}
 recipesDB,err:=recipes.Open(filepath.Join(t.TempDir(),"recipes","recipes.json"))
 if err!=nil{t.Fatal(err)}
 saved,err:=recipesDB.Save(recipes.Recipe{
  Title:"Beans",Servings:2,
  Ingredients:[]recipes.Ingredient{{Name:"Beans",Amount:200,Unit:"g"}},
  Steps:[]recipes.Step{{Text:"Cook beans."}},
 })
 if err!=nil{t.Fatal(err)}
 s.SetRecipes(recipesDB)
 h:=s.Handler()
 url:="/v1/recipes/"+saved.ID
 selection:=USDARecipeRequest{ExpectedRevision:saved.Revision,Ingredients:[]USDAIngredientSelection{
  {IngredientIndex:0,FDCID:101,Grams:200},
 }}
 body,_:=json.Marshal(selection)
 if got:=request(h,"POST",url+"/usda-preview",string(body),"");got.Code!=401{
  t.Fatalf("anonymous preview allowed: %d",got.Code)
 }
 preview:=request(h,"POST",url+"/usda-preview",string(body),strings.Repeat("t",40))
 if preview.Code!=200{
  t.Fatalf("USDA preview unavailable: %d %s",preview.Code,preview.Body.String())
 }
 var out usdaRecipeEstimate
 if err:=json.Unmarshal(preview.Body.Bytes(),&out);err!=nil{t.Fatal(err)}
 if len(out.PerServing)!=1||out.PerServing[0].Amount!=10||len(out.SHA256)!=64{
  t.Fatalf("incomplete preview: %+v",out)
 }
 if got:=request(h,"POST",url+"/usda-apply",string(body),strings.Repeat("t",40));got.Code!=400{
  t.Fatalf("unconfirmed nutrition was saved: %d",got.Code)
 }
 selection.Confirm=true
 selection.ExpectedSHA256=strings.Repeat("0",64)
 body,_=json.Marshal(selection)
 if got:=request(h,"POST",url+"/usda-apply",string(body),strings.Repeat("t",40));got.Code!=400{
  t.Fatalf("stale digest was accepted: %d",got.Code)
 }
 unchanged,_:=recipesDB.Get(saved.ID)
 if unchanged.Revision!=1 || unchanged.Nutrition!=nil{t.Fatal("preview silently changed recipe")}
 selection.ExpectedSHA256=out.SHA256
 body,_=json.Marshal(selection)
 result:=request(h,"POST",url+"/usda-apply",string(body),strings.Repeat("t",40))
 if result.Code!=200{
  t.Fatalf("reviewed nutrition save failed: %d %s",result.Code,result.Body.String())
 }
 current,_:=recipesDB.Get(saved.ID)
 if current.Revision!=2||current.Nutrition==nil||
  current.Nutrition.Method!="estimated"||current.Nutrition.PerServing[0].Name!="protein"{
  t.Fatalf("confirmed estimate not persisted: %+v",current)
 }
 if len(recipesDB.Meals())!=0{t.Fatal("recipe nutrient estimate logged consumption")}
 if got:=request(h,"POST",url+"/usda-apply",string(body),strings.Repeat("t",40));got.Code==200{
  t.Fatal("stale approved revision saved twice")
 }
}
