package health

import(
 "context"
 "errors"
 "fmt"
 "time"
)

type NutrientDefinition struct{
 Metric string `json:"metric"`
 Name string `json:"name"`
 Group string `json:"group"`
 TypicalUnit string `json:"typical_unit"`
}
var nutrientDefinitions=[]NutrientDefinition{
 {"energy_kcal","Energy","energy","kcal"},
 {"protein","Protein","macronutrients","g"},
 {"total_fat","Total fat","macronutrients","g"},
 {"saturated_fat","Saturated fat","macronutrients","g"},
 {"monounsaturated_fat","Monounsaturated fat","macronutrients","g"},
 {"polyunsaturated_fat","Polyunsaturated fat","macronutrients","g"},
 {"trans_fat","Trans fat","macronutrients","g"},
 {"carbohydrate","Total carbohydrate","macronutrients","g"},
 {"fiber","Dietary fiber","macronutrients","g"},
 {"sugars","Total sugars","macronutrients","g"},
 {"added_sugars","Added sugars","macronutrients","g"},
 {"starch","Starch","macronutrients","g"},
 {"water","Water","hydration","mL"},
 {"sodium","Sodium","minerals","mg"},
 {"potassium","Potassium","minerals","mg"},
 {"chloride","Chloride","minerals","mg"},
 {"calcium","Calcium","minerals","mg"},
 {"phosphorus","Phosphorus","minerals","mg"},
 {"magnesium","Magnesium","minerals","mg"},
 {"iron","Iron","minerals","mg"},
 {"zinc","Zinc","minerals","mg"},
 {"copper","Copper","minerals","mg"},
 {"manganese","Manganese","minerals","mg"},
 {"selenium","Selenium","minerals","µg"},
 {"iodine","Iodine","minerals","µg"},
 {"fluoride","Fluoride","minerals","mg"},
 {"chromium","Chromium","minerals","µg"},
 {"molybdenum","Molybdenum","minerals","µg"},
 {"vitamin_a_rae","Vitamin A (retinol activity equivalents)","vitamins","µg"},
 {"vitamin_c","Vitamin C","vitamins","mg"},
 {"vitamin_d","Vitamin D","vitamins","µg"},
 {"vitamin_e","Vitamin E (alpha-tocopherol)","vitamins","mg"},
 {"vitamin_k","Vitamin K","vitamins","µg"},
 {"thiamin_b1","Thiamin (B1)","vitamins","mg"},
 {"riboflavin_b2","Riboflavin (B2)","vitamins","mg"},
 {"niacin_b3","Niacin (B3)","vitamins","mg"},
 {"pantothenic_acid_b5","Pantothenic acid (B5)","vitamins","mg"},
 {"vitamin_b6","Vitamin B6","vitamins","mg"},
 {"biotin_b7","Biotin (B7)","vitamins","µg"},
 {"folate_dfe","Folate (dietary folate equivalents)","vitamins","µg"},
 {"vitamin_b12","Vitamin B12","vitamins","µg"},
 {"choline","Choline","vitamins","mg"},
 {"betaine","Betaine","other","mg"},
 {"omega_3_ala","Omega-3 ALA","fatty_acids","g"},
 {"omega_3_epa","Omega-3 EPA","fatty_acids","mg"},
 {"omega_3_dha","Omega-3 DHA","fatty_acids","mg"},
 {"omega_6_linoleic","Omega-6 linoleic acid","fatty_acids","g"},
 {"cholesterol","Cholesterol","other","mg"},
 {"caffeine","Caffeine","other","mg"},
 {"alcohol","Alcohol","other","g"},
 {"taurine","Taurine","other","mg"},
 {"creatine","Creatine","other","g"},
 {"leucine","Leucine","amino_acids","g"},
 {"isoleucine","Isoleucine","amino_acids","g"},
 {"valine","Valine","amino_acids","g"},
 {"lysine","Lysine","amino_acids","g"},
 {"methionine","Methionine","amino_acids","g"},
 {"threonine","Threonine","amino_acids","g"},
 {"tryptophan","Tryptophan","amino_acids","g"},
 {"phenylalanine","Phenylalanine","amino_acids","g"},
 {"histidine","Histidine","amino_acids","g"},
 {"arginine","Arginine","amino_acids","g"},
 {"glycine","Glycine","amino_acids","g"},
 {"proline","Proline","amino_acids","g"},
 {"tyrosine","Tyrosine","amino_acids","g"},
 {"serine","Serine","amino_acids","g"},
 {"glutamic_acid","Glutamic acid","amino_acids","g"},
 {"aspartic_acid","Aspartic acid","amino_acids","g"},
 {"cystine","Cystine","amino_acids","g"},
}

func NutrientCatalog()[]NutrientDefinition{
 result:=make([]NutrientDefinition,len(nutrientDefinitions))
 copy(result,nutrientDefinitions)
 return result
}
// NutritionTotals sums all current records on the requested UTC date.
// A total represents observed entries, not proof of complete daily consumption.
func(s *Store) NutritionTotals(ctx context.Context,day string)(map[string]map[string]float64,error){
 t,err:=time.Parse("2006-01-02",day)
 if err!=nil||t.Format("2006-01-02")!=day{return nil,errors.New("date must be YYYY-MM-DD")}
 rows,err:=s.db.QueryContext(ctx,`SELECT metric,unit,SUM(value)
  FROM records WHERE domain='nutrition' AND date(at)=?
  AND id NOT IN (SELECT replaces FROM records WHERE replaces IS NOT NULL)
  GROUP BY metric,unit ORDER BY metric,unit`,day)
 if err!=nil{return nil,fmt.Errorf("query nutrient totals: %w",err)}
 defer rows.Close()
 totals:=map[string]map[string]float64{}
 for rows.Next(){
  var metric,unit string;var amount float64
  if err=rows.Scan(&metric,&unit,&amount);err!=nil{return nil,err}
  if totals[metric]==nil{totals[metric]=map[string]float64{}}
  totals[metric][unit]=amount
 }
 return totals,rows.Err()
}
