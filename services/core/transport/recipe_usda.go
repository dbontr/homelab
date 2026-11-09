package transport

import (
 "context"
 "crypto/sha256"
 "encoding/hex"
 "encoding/json"
 "errors"
 "fmt"
 "math"
 "net/http"
 "strings"

 "github.com/dbontr/homelab-new/system/recipes"
)
type USDAIngredientSelection struct {
 IngredientIndex int `json:"ingredient_index"`
 FDCID int64 `json:"fdc_id"`
 Grams float64 `json:"grams"`
}
type USDARecipeRequest struct {
 ExpectedRevision int `json:"expected_revision"`
 Ingredients []USDAIngredientSelection `json:"ingredients"`
 ExpectedSHA256 string `json:"expected_sha256,omitempty"`
 Confirm bool `json:"confirm,omitempty"`
}
type usdaEvidence struct {
 IngredientIndex int `json:"ingredient_index"`
 FDCID int64 `json:"fdc_id"`
 Description string `json:"description"`
 DataType string `json:"data_type"`
 Grams float64 `json:"grams"`
}
type usdaRecipeEstimate struct {
 Method string `json:"method"`
 Source string `json:"source"`
 Servings float64 `json:"servings"`
 PerServing []recipes.Nutrient `json:"per_serving"`
 Incomplete []string `json:"incomplete"`
 Ingredients []usdaEvidence `json:"ingredients"`
 Limitations string `json:"limitations"`
 SHA256 string `json:"sha256"`
}
func validateUSDASelection(r recipes.Recipe,input USDARecipeRequest)error{
 if input.ExpectedRevision!=r.Revision || len(r.Ingredients)<1||len(r.Ingredients)>12 ||
  len(input.Ingredients)!=len(r.Ingredients) {
  return errors.New("recipe revision or ingredient count is invalid")
 }
 seen:=make([]bool,len(r.Ingredients))
 for _,item:=range input.Ingredients{
  if item.IngredientIndex<0||item.IngredientIndex>=len(seen)||
   seen[item.IngredientIndex]||item.FDCID<=0||item.Grams<=0||
   math.IsNaN(item.Grams)||math.IsInf(item.Grams,0)||item.Grams>10000{
   return errors.New("each recipe ingredient requires one reviewed USDA ID and edible gram quantity")
  }
  seen[item.IngredientIndex]=true
 }
 return nil
}
func validateUSDAEstimate(recipe recipes.Recipe,input USDARecipeRequest,est usdaRecipeEstimate)error{
 if est.Method!="estimated"||est.Source!="USDA FoodData Central"||
  est.Servings!=recipe.Servings||len(est.SHA256)!=64||
  len(est.Ingredients)!=len(input.Ingredients)||
  len(est.PerServing)==0||len(est.PerServing)>150{
  return errors.New("USDA estimate source, portion or nutrient completeness is invalid")
 }
 original:=est.SHA256
 est.SHA256=""
 raw,err:=json.Marshal(est)
 if err!=nil{return err}
 digest:=sha256.Sum256(raw)
 if hex.EncodeToString(digest[:])!=original{return errors.New("USDA estimate checksum mismatch")}
 for i,evidence:=range est.Ingredients{
  chosen:=input.Ingredients[i]
  if evidence.IngredientIndex!=chosen.IngredientIndex||evidence.FDCID!=chosen.FDCID||
   evidence.Grams!=chosen.Grams||strings.TrimSpace(evidence.Description)==""{
   return errors.New("USDA ingredient selection does not match reviewed evidence")
  }
 }
 unique:=map[string]bool{}
 for _,n:=range est.PerServing{
  if strings.TrimSpace(n.Name)==""||n.Amount<0||math.IsInf(n.Amount,0)||
   math.IsNaN(n.Amount)||n.Unit==""||unique[n.Name]{
   return errors.New("USDA estimate contains invalid or duplicate nutrients")
  }
  unique[n.Name]=true
 }
 return nil
}
func(s *Server)recipeUSDAPreview(ctx context.Context,recipeID string,input USDARecipeRequest)(usdaRecipeEstimate,recipes.Recipe,error){
 var empty usdaRecipeEstimate
 if s.recipes==nil{return empty,recipes.Recipe{},errors.New("recipe storage is unavailable")}
 recipe,err:=s.recipes.Get(recipeID)
 if err!=nil{return empty,recipe,err}
 if err:=validateUSDASelection(recipe,input);err!=nil{return empty,recipe,err}
 if !s.hasBackend("health"){return empty,recipe,errors.New("Health food data is unavailable")}
 raw,err:=s.backendObject(ctx,"health__health_food_recipe_estimate",map[string]any{
  "ingredients":input.Ingredients,
  "servings":recipe.Servings,
 },64<<10)
 if err!=nil{return empty,recipe,err}
 payload,err:=json.Marshal(raw)
 if err!=nil{return empty,recipe,err}
 var est usdaRecipeEstimate
 if err:=json.Unmarshal(payload,&est);err!=nil{return empty,recipe,err}
 if err:=validateUSDAEstimate(recipe,input,est);err!=nil{return empty,recipe,err}
 return est,recipe,nil
}
func(s *Server)recipeUSDATool(ctx context.Context,recipeID string,input USDARecipeRequest,apply bool)(any,error){
 est,record,err:=s.recipeUSDAPreview(ctx,recipeID,input)
 if err!=nil{return nil,err}
 if !apply{return est,nil}
 if !input.Confirm||input.ExpectedSHA256!=est.SHA256{
  return nil,errors.New("saving an estimate requires confirmation of the current USDA evidence digest")
 }
 if len(est.PerServing)==0{return nil,errors.New("no complete nutrients are available for this recipe")}
 ids:=make([]string,len(input.Ingredients))
 for _,item:=range input.Ingredients{ids[item.IngredientIndex]=fmt.Sprintf("%d",item.FDCID)}
 record.Nutrition=&recipes.Nutrition{
  Method:"estimated",
  Source:"USDA FoodData Central FDC IDs "+strings.Join(ids,",")+"; owner-reviewed edible grams; unreported nutrients unknown",
  PerServing:est.PerServing,
 }
 saved,err:=s.recipes.Save(record)
 if err!=nil{return nil,err}
 return map[string]any{
  "recipe":saved,"source_sha256":est.SHA256,
  "missing_nutrients":est.Incomplete,"consumption_logged":false,
 },nil
}
func(s *Server)recipeUSDAHTTP(w http.ResponseWriter,r *http.Request){
 var input USDARecipeRequest
 if err:=decode(r,&input);err!=nil{writeError(w,400,err.Error());return}
 apply:=strings.HasSuffix(r.URL.Path,"/usda-apply")
 output,err:=s.recipeUSDATool(r.Context(),r.PathValue("id"),input,apply)
 if errors.Is(err,recipes.ErrNotFound){writeError(w,404,"recipe not found");return}
 if errors.Is(err,recipes.ErrConflict){writeError(w,409,"recipe revision changed");return}
 if err!=nil{writeError(w,400,err.Error());return}
 if apply{writeJSON(w,200,output)}else{writeJSON(w,200,output)}
}
