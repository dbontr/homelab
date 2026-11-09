package recipes

import (
 "encoding/json"
 "errors"
 "fmt"
 "html"
 "regexp"
 "strconv"
 "strings"
)

// ParseSchemaOrg extracts a schema.org Recipe from JSON-LD or an HTML document.
// It never follows links, executes scripts, or writes to the store.
// The caller must review the draft and explicitly save it.
func ParseSchemaOrg(source string)(Recipe,error){
 if len(source)>512<<10{return Recipe{},errors.New("recipe source exceeds 512 KiB")}
 blocks:=[]string{strings.TrimSpace(source)}
 if strings.Contains(strings.ToLower(source),"<script"){
  blocks=nil
  re:=regexp.MustCompile(`(?is)<script\b([^>]*)>(.*?)</script\s*>`)
  for _,part:=range re.FindAllStringSubmatch(source,-1){
   if strings.Contains(strings.ToLower(part[1]),"application/ld+json"){
    blocks=append(blocks,strings.TrimSpace(part[2]))
   }
  }
 }
 for _,block:=range blocks{
  var document any
  if json.Unmarshal([]byte(block),&document)!=nil{continue}
  if record,ok:=findRecipe(document,0);ok{
   parsed,err:=schemaRecipe(record)
   if err==nil{return parsed,nil}
   return Recipe{},err
  }
 }
 return Recipe{},errors.New("schema.org Recipe data was not found")
}

func findRecipe(value any,depth int)(map[string]any,bool){
 if depth>12{return nil,false}
 switch node:=value.(type){
 case []any:
  for _,child:=range node{if found,ok:=findRecipe(child,depth+1);ok{return found,true}}
 case map[string]any:
  if schemaType(node["@type"]){return node,true}
  for _,key:=range []string{"@graph","mainEntity","mainEntityOfPage","itemListElement"}{
   if found,ok:=findRecipe(node[key],depth+1);ok{return found,true}
  }
 }
 return nil,false
}

func schemaType(value any)bool{
 switch kind:=value.(type){
 case string:return strings.EqualFold(kind,"Recipe")||strings.HasSuffix(strings.ToLower(kind),"/recipe")
 case []any:
  for _,item:=range kind{if schemaType(item){return true}}
 }
 return false
}

func schemaString(value any)string{
 switch v:=value.(type){
 case string:return strings.TrimSpace(html.UnescapeString(v))
 case float64:return strconv.FormatFloat(v,'f',-1,64)
 case map[string]any:return schemaString(v["@value"])
 case []any:
  if len(v)>0{return schemaString(v[0])}
 }
 return ""
}

func schemaStrings(value any)[]string{
 switch v:=value.(type){
 case []any:
  out:=make([]string,0,len(v))
  for _,x:=range v{if s:=schemaString(x);s!=""{out=append(out,s)}}
  return out
 case string:
  if strings.TrimSpace(v)!=""{return []string{v}}
 }
 return nil
}

var ingredientPrefix=regexp.MustCompile(`^\s*((?:\d+(?:[.,]\d+)?)(?:\s+\d+/\d+)?|\d+/\d+|[¼½¾⅓⅔⅛⅜⅝⅞])\s*(.*)$`)
var unitTokens=map[string]bool{
 "tsp":true,"tsp.":true,"teaspoon":true,"teaspoons":true,
 "tbsp":true,"tbsp.":true,"tablespoon":true,"tablespoons":true,
 "cup":true,"cups":true,"g":true,"gram":true,"grams":true,
 "kg":true,"ml":true,"l":true,"liter":true,"liters":true,
 "oz":true,"lb":true,"lbs":true,"pound":true,"pounds":true,
 "piece":true,"pieces":true,"clove":true,"cloves":true,
 "pinch":true,"pinches":true,"can":true,"cans":true,
}
var glyphFractions=map[string]float64{"¼":.25,"½":.5,"¾":.75,"⅓":1.0/3.0,"⅔":2.0/3.0,"⅛":.125,"⅜":.375,"⅝":.625,"⅞":.875}

func parseQuantity(text string)(float64,bool){
 if number,ok:=glyphFractions[text];ok{return number,true}
 pieces:=strings.Fields(strings.ReplaceAll(text,",","."))
 total:=0.0
 for _,piece:=range pieces{
  if parts:=strings.Split(piece,"/");len(parts)==2{
   a,errA:=strconv.ParseFloat(parts[0],64)
   b,errB:=strconv.ParseFloat(parts[1],64)
   if errA!=nil||errB!=nil||b==0{return 0,false}
   total+=a/b;continue
  }
  n,err:=strconv.ParseFloat(piece,64);if err!=nil{return 0,false}
  total+=n
 }
 return total,total>0
}

func parseIngredient(text string)Ingredient{
 text=strings.TrimSpace(html.UnescapeString(text))
 result:=Ingredient{Name:text,Amount:0,Notes:"Quantity is not specified; review before saving."}
 match:=ingredientPrefix.FindStringSubmatch(text)
 if len(match)!=3{return result}
 amount,ok:=parseQuantity(match[1])
 if !ok{return result}
 tail:=strings.TrimSpace(match[2])
 pieces:=strings.Fields(tail)
 unit:=""
 if len(pieces)>1&&unitTokens[strings.ToLower(pieces[0])]{
  unit=pieces[0]
  tail=strings.TrimSpace(strings.TrimPrefix(tail,pieces[0]))
 }
 if tail==""{return result}
 result.Name=tail
 result.Amount=amount
 result.Unit=unit
 result.Notes=""
 return result
}

func schemaSteps(value any)[]Step{
 if text,ok:=value.(string);ok{
  lines:=strings.Split(text,string(rune(10)))
  out:=[]Step{}
  for _,line:=range lines{
   if clean:=strings.TrimSpace(line);clean!=""{out=append(out,Step{Text:clean})}
  }
  return out
 }
 sequence,ok:=value.([]any)
 if !ok{sequence=[]any{value}}
 out:=[]Step{}
 for _,raw:=range sequence{
  switch entry:=raw.(type){
  case string:
   if text:=strings.TrimSpace(html.UnescapeString(entry));text!=""{out=append(out,Step{Text:text})}
  case map[string]any:
   if nested,ok:=entry["itemListElement"];ok{out=append(out,schemaSteps(nested)...);continue}
   text:=schemaString(entry["text"])
   if text==""{text=schemaString(entry["name"])}
   if text!=""{out=append(out,Step{Text:text})}
  }
 }
 return out
}

var durationParts=regexp.MustCompile(`^P(?:([0-9]+)D)?(?:T(?:([0-9]+)H)?(?:([0-9]+)M)?)?$`)

func schemaMinutes(value any)int{
 text:=schemaString(value)
 fields:=durationParts.FindStringSubmatch(text)
 if len(fields)!=4{return 0}
 n:=func(v string)int{if v==""{return 0};i,_:=strconv.Atoi(v);return i}
 result:=n(fields[1])*1440+n(fields[2])*60+n(fields[3])
 if result<0||result>10080{return 0}
 return result
}

func schemaRecipe(data map[string]any)(Recipe,error){
 item:=Recipe{
  Title:schemaString(data["name"]),
  Summary:schemaString(data["description"]),
  Servings:1,
  PrepMinutes:schemaMinutes(data["prepTime"]),
  CookMinutes:schemaMinutes(data["cookTime"]),
  Steps:schemaSteps(data["recipeInstructions"]),
  SourceURL:schemaString(data["url"]),
 }
 if item.SourceURL!="" && !strings.HasPrefix(item.SourceURL,"https://"){item.SourceURL=""}
 if value:=schemaString(data["recipeYield"]);value!=""{
  first:=strings.Fields(value)
  if len(first)>0{if n,ok:=parseQuantity(first[0]);ok{item.Servings=n}}
 }
 for _,raw:=range schemaStrings(data["recipeIngredient"]){
  item.Ingredients=append(item.Ingredients,parseIngredient(raw))
 }
 if item.Title==""||len(item.Ingredients)==0||len(item.Steps)==0{
  return Recipe{},errors.New("recipe is missing title, ingredients or instructions")
 }
 // A draft can contain unspecified quantities; validation on Save requires
 // the owner to supply exact amounts before a shopping list is generated.
 if len(item.Ingredients)>250||len(item.Steps)>200{return Recipe{},errors.New("recipe exceeds supported size")}
 if item.Servings<=0||item.Servings>1000{return Recipe{},fmt.Errorf("invalid recipe yield")}
 return item,nil
}
