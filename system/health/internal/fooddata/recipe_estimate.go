package fooddata

import (
 "context"
 "crypto/sha256"
 "encoding/hex"
 "encoding/json"
 "errors"
 "fmt"
 "math"
 "sort"
 "strings"
 "time"
)

// USDAIngredient is an explicitly selected FoodData Central ingredient and
// edible mass. Names, densities, and cooking yields are never guessed.
type USDAIngredient struct {
 IngredientIndex int `json:"ingredient_index"`
 FDCID int64 `json:"fdc_id"`
 Grams float64 `json:"grams"`
}
type RecipeNutrient struct {
 Name string `json:"name"`
 Amount float64 `json:"amount"`
 Unit string `json:"unit"`
}
type USDAIngredientEvidence struct {
 IngredientIndex int `json:"ingredient_index"`
 FDCID int64 `json:"fdc_id"`
 Description string `json:"description"`
 DataType string `json:"data_type"`
 Grams float64 `json:"grams"`
}
type RecipeNutrientEstimate struct {
 Method string `json:"method"`
 Source string `json:"source"`
 Servings float64 `json:"servings"`
 PerServing []RecipeNutrient `json:"per_serving"`
 Incomplete []string `json:"incomplete"`
 Ingredients []USDAIngredientEvidence `json:"ingredients"`
 Limitations string `json:"limitations"`
 SHA256 string `json:"sha256"`
}
type fdcNutrientDefinition struct{
 Metric string
 Unit string
}
var fdcNutrientMap=map[int64]fdcNutrientDefinition{
 1003:{"protein","g"},
 1004:{"total_fat","g"},
 1005:{"carbohydrate","g"},
 1008:{"energy_kcal","kcal"},
 2047:{"energy_kcal","kcal"},
 2048:{"energy_kcal","kcal"},
 1079:{"fiber","g"},
 2000:{"sugars","g"},
 1258:{"saturated_fat","g"},
 1292:{"monounsaturated_fat","g"},
 1293:{"polyunsaturated_fat","g"},
 1253:{"cholesterol","mg"},
 1093:{"sodium","mg"},
 1092:{"potassium","mg"},
 1087:{"calcium","mg"},
 1090:{"magnesium","mg"},
 1091:{"phosphorus","mg"},
 1089:{"iron","mg"},
 1095:{"zinc","mg"},
 1098:{"copper","mg"},
 1101:{"manganese","mg"},
 1103:{"selenium","µg"},
 1106:{"vitamin_a_rae","µg"},
 1162:{"vitamin_c","mg"},
 1114:{"vitamin_d","µg"},
 1109:{"vitamin_e","mg"},
 1185:{"vitamin_k","µg"},
 1165:{"thiamin_b1","mg"},
 1166:{"riboflavin_b2","mg"},
 1167:{"niacin_b3","mg"},
 1170:{"pantothenic_acid_b5","mg"},
 1175:{"vitamin_b6","mg"},
 1178:{"vitamin_b12","µg"},
 1177:{"folate_dfe","µg"},
 1180:{"choline","mg"},
}
type fdcDetail struct{
 FDCID int64 `json:"fdcId"`
 Description string `json:"description"`
 DataType string `json:"dataType"`
 Nutrients []struct{
  Nutrient struct{
   ID int64 `json:"id"`
   UnitName string `json:"unitName"`
  } `json:"nutrient"`
  Amount *float64 `json:"amount"`
 } `json:"foodNutrients"`
}
func unitEquals(got,expected string)bool{
 normalized:=strings.ToLower(strings.TrimSpace(got))
 if expected=="µg"{return normalized=="ug"||normalized=="µg"||normalized=="mcg"}
 return normalized==expected
}
func fdcDetailFacts(raw json.RawMessage,expectedID int64)(fdcDetail,map[string]float64,error){
 var food fdcDetail
 if err:=json.Unmarshal(raw,&food);err!=nil{return food,nil,errors.New("invalid USDA food details")}
 if food.FDCID!=expectedID||strings.TrimSpace(food.Description)==""||
  len(food.Description)>300||len(food.Nutrients)>500 {
  return food,nil,errors.New("USDA food identity or nutrient list is invalid")
 }
 facts:=map[string]float64{}
 energyPriority:=-1
 for _,entry:=range food.Nutrients{
  def,known:=fdcNutrientMap[entry.Nutrient.ID]
  if !known||entry.Amount==nil||!unitEquals(entry.Nutrient.UnitName,def.Unit){
   continue
  }
  value:=*entry.Amount
  if math.IsInf(value,0)||math.IsNaN(value)||value<0||value>1e9{
   continue
  }
  if def.Metric=="energy_kcal"{
   priority:=0
   if entry.Nutrient.ID==2048{priority=1}
   if entry.Nutrient.ID==2047{priority=2}
   if priority<=energyPriority{continue}
   energyPriority=priority
  }else{
   if _,exists:=facts[def.Metric];exists{
    // Conflicting duplicate measurements must not be arbitrarily selected.
    delete(facts,def.Metric)
    continue
   }
  }
  facts[def.Metric]=value
 }
 return food,facts,nil
}

// EstimateRecipeNutrients combines reported per-100g values. A nutrient is
// reported as complete only if every explicitly selected food contains it.
func EstimateRecipeNutrients(raw []json.RawMessage,items []USDAIngredient,servings float64)(RecipeNutrientEstimate,error){
 var empty RecipeNutrientEstimate
 if len(items)<1||len(items)>12||len(raw)!=len(items)||
  math.IsNaN(servings)||math.IsInf(servings,0)||servings<=0||servings>200{
  return empty,errors.New("invalid USDA recipe portion or ingredient count")
 }
 totals:=map[string]float64{}
 covered:=map[string]int{}
 evidence:=make([]USDAIngredientEvidence,0,len(items))
 used:=map[int]bool{}
 for i,item:=range items{
  if item.IngredientIndex<0||item.IngredientIndex>1000||used[item.IngredientIndex]||
   item.FDCID<=0||math.IsNaN(item.Grams)||math.IsInf(item.Grams,0)||
   item.Grams<=0||item.Grams>10000{
   return empty,errors.New("invalid or duplicate recipe ingredient mapping")
  }
  used[item.IngredientIndex]=true
  food,facts,err:=fdcDetailFacts(raw[i],item.FDCID)
  if err!=nil{return empty,err}
  evidence=append(evidence,USDAIngredientEvidence{
   IngredientIndex:item.IngredientIndex,FDCID:item.FDCID,
   Description:food.Description,DataType:food.DataType,Grams:item.Grams,
  })
  for nutrient,value:=range facts{
   totals[nutrient]+=value*item.Grams/100/servings
   covered[nutrient]++
  }
 }
 out:=RecipeNutrientEstimate{
  Method:"estimated",Source:"USDA FoodData Central",
  Servings:servings,PerServing:[]RecipeNutrient{},Incomplete:[]string{},
  Ingredients:evidence,
  Limitations:"Owner-selected food identities and edible gram amounts. Calculations use reported per-100g compositions; cooking retention, moisture change, missing nutrients, and unmeasured ingredients are not inferred. Only nutrients reported for every selected ingredient are included.",
 }
 keys:=make([]string,0,len(totals))
 for key:=range totals{keys=append(keys,key)}
 sort.Strings(keys)
 for _,key:=range keys{
  if covered[key]!=len(items){
   out.Incomplete=append(out.Incomplete,key)
   continue
  }
  amount:=totals[key]
  if math.IsNaN(amount)||math.IsInf(amount,0)||amount<0||amount>1e10{
   return empty,errors.New("USDA combined nutrient exceeds safe range")
  }
  unit:=""
  for _,def:=range fdcNutrientMap{if def.Metric==key{unit=def.Unit;break}}
  if unit==""{return empty,errors.New("unknown canonical nutrient")}
  out.PerServing=append(out.PerServing,RecipeNutrient{Name:key,Amount:amount,Unit:unit})
 }
 data,err:=json.Marshal(out)
 if err!=nil{return empty,err}
 sum:=sha256.Sum256(data)
 out.SHA256=hex.EncodeToString(sum[:])
 return out,nil
}

// RecipeEstimate fetches only explicitly selected USDA IDs; it neither
// performs automatic ingredient matching nor writes food measurements.
func(c *Client)RecipeEstimate(ctx context.Context,items []USDAIngredient,servings float64)(RecipeNutrientEstimate,error){
 if len(items)<1||len(items)>12{return RecipeNutrientEstimate{},errors.New("recipe ingredient limit exceeded")}
 bounded,cancel:=context.WithTimeout(ctx,30*time.Second)
 defer cancel()
 raw:=make([]json.RawMessage,0,len(items))
 for _,item:=range items{
  if item.FDCID<=0||item.Grams<=0||math.IsNaN(item.Grams)||math.IsInf(item.Grams,0)||
   item.Grams>10000||item.IngredientIndex<0{
   return RecipeNutrientEstimate{},errors.New("invalid USDA ingredient selection")
  }
  details,err:=c.USDADetail(bounded,item.FDCID)
  if err!=nil{return RecipeNutrientEstimate{},fmt.Errorf("USDA food %d unavailable: %w",item.FDCID,err)}
  raw=append(raw,details)
 }
 return EstimateRecipeNutrients(raw,items,servings)
}
