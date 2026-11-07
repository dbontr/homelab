package fooddata

import(
 "encoding/json"
 "errors"
 "math"
 "strings"
)
type NutrientFact struct{
 Value float64 `json:"value"`
 Unit string `json:"unit"`
}
type BarcodePreview struct{
 Barcode string `json:"barcode"`
 ProductName string `json:"product_name"`
 Quantity float64 `json:"quantity"`
 Basis string `json:"basis"`
 Nutrients map[string]NutrientFact `json:"nutrients"`
 MissingDescription string `json:"missing_description"`
 Source string `json:"source"`
}
type nutrientMapping struct{
 Metric string
 Unit string
 Factor float64
}
var standardNutrients=map[string]nutrientMapping{
 "energy-kcal": {"energy_kcal","kcal",1},
 "proteins":{"protein","g",1},
 "fat":{"total_fat","g",1},
 "saturated-fat":{"saturated_fat","g",1},
 "monounsaturated-fat":{"monounsaturated_fat","g",1},
 "polyunsaturated-fat":{"polyunsaturated_fat","g",1},
 "trans-fat":{"trans_fat","g",1},
 "carbohydrates":{"carbohydrate","g",1},
 "fiber":{"fiber","g",1},
 "sugars":{"sugars","g",1},
 "added-sugars":{"added_sugars","g",1},
 "starch":{"starch","g",1},
 "sodium":{"sodium","mg",1000},
 "potassium":{"potassium","mg",1000},
 "calcium":{"calcium","mg",1000},
 "magnesium":{"magnesium","mg",1000},
 "iron":{"iron","mg",1000},
 "phosphorus":{"phosphorus","mg",1000},
 "zinc":{"zinc","mg",1000},
 "copper":{"copper","mg",1000},
 "selenium":{"selenium","µg",1000000},
 "iodine":{"iodine","µg",1000000},
 "vitamin-c":{"vitamin_c","mg",1000},
 "vitamin-d":{"vitamin_d","µg",1000000},
 "vitamin-e":{"vitamin_e","mg",1000},
 "vitamin-k":{"vitamin_k","µg",1000000},
 "vitamin-b1":{"thiamin_b1","mg",1000},
 "vitamin-b2":{"riboflavin_b2","mg",1000},
 "vitamin-b3":{"niacin_b3","mg",1000},
 "vitamin-b5":{"pantothenic_acid_b5","mg",1000},
 "vitamin-b6":{"vitamin_b6","mg",1000},
 "vitamin-b7":{"biotin_b7","µg",1000000},
 "vitamin-b12":{"vitamin_b12","µg",1000000},
 "cholesterol":{"cholesterol","mg",1000},
 "caffeine":{"caffeine","mg",1000},
}
func ExtractBarcodeNutrients(raw json.RawMessage,barcode string,quantity float64,basis string)(BarcodePreview,error){
 if !barcodePattern.MatchString(barcode){return BarcodePreview{},errors.New("barcode must be 8 to 14 digits")}
 if math.IsNaN(quantity)||math.IsInf(quantity,0)||quantity<=0||quantity>10000{
  return BarcodePreview{},errors.New("portion quantity must be positive and no more than 10000")
 }
 if basis!="g"&&basis!="mL"{return BarcodePreview{},errors.New("basis must be g or mL; confirm the product label")}
 var payload struct{
  Product struct{
   ProductName string `json:"product_name"`
   Nutriments map[string]json.RawMessage `json:"nutriments"`
  } `json:"product"`
 }
 if err:=json.Unmarshal(raw,&payload);err!=nil{return BarcodePreview{},err}
 if strings.TrimSpace(payload.Product.ProductName)==""{
  return BarcodePreview{},errors.New("product name is unavailable; confirm identity before logging")
 }
 nutrients:=map[string]NutrientFact{}
 for source,mapping:=range standardNutrients{
  rawValue,ok:=payload.Product.Nutriments[source+"_100g"]
  if !ok{continue}
  var value float64
  if json.Unmarshal(rawValue,&value)!=nil||math.IsNaN(value)||math.IsInf(value,0)||value<0{continue}
  normalized:=value*(quantity/100)*mapping.Factor
  if math.IsNaN(normalized)||math.IsInf(normalized,0){continue}
  nutrients[mapping.Metric]=NutrientFact{Value:normalized,Unit:mapping.Unit}
 }
 return BarcodePreview{
  Barcode:barcode,ProductName:payload.Product.ProductName,
  Quantity:quantity,Basis:basis,Nutrients:nutrients,
  MissingDescription:"Nutrients absent from the source are unknown; this preview is not an exhaustive composition analysis.",
  Source:"Open Food Facts normalized as-sold per-100g or per-100mL data; portion basis and identity require review",
 },nil
}
