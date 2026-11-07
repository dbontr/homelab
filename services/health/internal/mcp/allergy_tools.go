package mcp

import(
 "context"
 "encoding/json"
 "errors"
 "strings"

 "github.com/dbontr/personal-health-mcp/internal/health"
)
type Allergy struct{
 Ingredient string `json:"ingredient"`
 Reaction string `json:"reaction,omitempty"`
 Severity string `json:"severity"`
 ConfirmedByClinician bool `json:"confirmed_by_clinician"`
}
func(s *Server)registerAllergyTools(){
 txt:=func(d string)map[string]any{return field("string",d)}
 s.add("health_allergy_record","Record a known or suspected dietary allergen. This entry is informational and cannot certify food safety or diagnose an allergy.",false,false,
 []string{"ingredient","severity"},
 map[string]any{"ingredient":txt("Allergen or sensitivity name"),
  "severity":selectField("Reported reaction severity","unknown","mild","moderate","severe"),
  "reaction":txt("Reaction description, if known"),
  "confirmed_by_clinician":field("boolean","Was this clinically confirmed?"),
  "observed_at":txt("Optional RFC3339 timestamp, defaults to now")},
 func(ctx context.Context,a map[string]json.RawMessage)(json.RawMessage,error){
  ingredient,err:=requiredString(a,"ingredient")
  if err!=nil||len(ingredient)>200{return nil,errors.New("ingredient is required, up to 200 characters")}
  severity,err:=requiredString(a,"severity");if err!=nil{return nil,err}
  if severity!="unknown"&&severity!="mild"&&severity!="moderate"&&severity!="severe"{return nil,errors.New("invalid severity")}
  allergy:=Allergy{Ingredient:ingredient,Severity:severity,Reaction:optionalString(a,"reaction","")}
  if len(allergy.Reaction)>2000{return nil,errors.New("reaction description too long")}
  if len(a["confirmed_by_clinician"])>0{
   if json.Unmarshal(a["confirmed_by_clinician"],&allergy.ConfirmedByClinician)!=nil{
    return nil,errors.New("confirmed_by_clinician must be boolean")
   }
  }
  at,err:=dateOrNow(a["observed_at"]);if err!=nil{return nil,err}
  meta,_:=json.Marshal(allergy)
  saved,err:=s.health.SaveJournal(ctx,health.JournalEntry{Category:"allergy",Title:ingredient,
   OccurredAt:at,Attributes:meta,Source:"manual"},0)
  if err!=nil{return nil,err}
  return send(saved)
 })
 s.add("health_food_allergen_screen","Flag possible textual matches between user-reported allergens and supplied ingredient descriptions. A non-match does not establish food safety, including cross-contamination or synonym risks.",true,false,
 []string{"ingredients"},
 map[string]any{"ingredients":map[string]any{"type":"array","items":map[string]any{"type":"string"},
  "description":"Full disclosed ingredient names or label text, not just a recipe title"}},
 func(ctx context.Context,a map[string]json.RawMessage)(json.RawMessage,error){
  var ingredients []string
  if json.Unmarshal(a["ingredients"],&ingredients)!=nil||len(ingredients)==0||len(ingredients)>300{
   return nil,errors.New("ingredients must contain 1 to 300 strings")
  }
  for _,item:=range ingredients{if len(item)>1000{return nil,errors.New("ingredient description too long")}}
  entries,err:=s.health.ListJournal(ctx,"allergy",200)
  if err!=nil{return nil,err}
  matches:=[]map[string]string{}
  for _,entry:=range entries{
   var allergy Allergy
   if json.Unmarshal(entry.Attributes,&allergy)!=nil{continue}
   needle:=strings.ToLower(strings.TrimSpace(allergy.Ingredient))
   if needle==""{continue}
   for _,ingredient:=range ingredients{
    if strings.Contains(strings.ToLower(ingredient),needle){
     matches=append(matches,map[string]string{
       "allergen":allergy.Ingredient,"ingredient_text":ingredient,
       "severity":allergy.Severity,"record_id":entry.ID})
    }
   }
  }
  return send(map[string]any{
   "matches":matches,"allergy_records_considered":len(entries),
   "status":"manual_review_required",
   "record_limit_reached":len(entries)==200,
   "warnings":[]string{
     "A non-match never means a product is allergen-free.",
     "Synonyms, derivatives, processing aids and cross-contact cannot be ruled out by text matching.",
     "Check the manufacturer's label and obtain appropriate clinical advice for severe allergies.",
   },
  })
 })
}
