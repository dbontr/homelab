package transport

import (
 "encoding/json"
 "errors"
 "fmt"
 "net/http"
 "time"

 "github.com/dbontr/homelab-new/system/recipes"
)

// Food proposals are deliberately independent of Health measurements.
// Proposing a recipe does not confirm consumption or record any nutrients.
func (s *Server) uiRecipeNutritionProposal(w http.ResponseWriter,r *http.Request){
 if s.recipes==nil{writeError(w,503,"recipe storage unavailable");return}
 var req struct{
  Servings float64 `json:"servings"`
  ObservedAt string `json:"observed_at"`
 }
 if err:=decode(r,&req);err!=nil{writeError(w,400,err.Error());return}
 instant:=time.Now().UTC()
 var err error
 if req.ObservedAt!=""{instant,err=time.Parse(time.RFC3339,req.ObservedAt)}
 if err!=nil||instant.After(time.Now().Add(24*time.Hour)){
  writeError(w,400,"a valid meal time is required");return
 }
 record,err:=s.recipes.Get(r.PathValue("id"))
 if errors.Is(err,recipes.ErrNotFound){writeError(w,404,"recipe not found");return}
 if err!=nil{writeError(w,400,err.Error());return}
 preview,err:=s.recipes.Nutrition(record.ID,req.Servings)
 if err!=nil{writeError(w,400,"recipe has no supported nutrient estimates for this portion");return}
 nutrients:=map[string]float64{}
 units:=map[string]string{}
 for _,nutrient:=range preview.Nutrients {
  if _,exists:=nutrients[nutrient.Name];exists{writeError(w,400,"recipe contains duplicate nutrient identifiers");return}
  nutrients[nutrient.Name]=nutrient.Amount
  units[nutrient.Name]=nutrient.Unit
 }
 if len(nutrients)==0{writeError(w,400,"recipe has no supported nutrient values");return}
 payload:=map[string]any{
  "food":record.Title,
  "portion":fmt.Sprintf("%g servings",req.Servings),
  "origin":"recipe",
  "nutrients":nutrients,
  "units":units,
  "observed_at":instant.Format(time.RFC3339),
  "notes":fmt.Sprintf("Recipe %s revision %d; nutrition %s; source %s. All unrecorded nutrients are unknown.",record.ID,record.Revision,preview.Method,preview.Source),
 }
 raw,err:=json.Marshal(payload);if err!=nil{writeError(w,500,"could not encode recipe");return}
 output:=s.invoke(r.Context(),"health__health_food_propose",raw,json.RawMessage("1"))
 if failed,_:=output["isError"].(bool);failed{writeError(w,502,"Health did not accept the food proposal");return}
 writeJSON(w,201,output)
}

func (s *Server) uiFoodProposals(w http.ResponseWriter,r *http.Request){
 s.uiBackendRead(w,r,"health","health_food_proposal_list")
}

func (s *Server) uiFoodConfirm(w http.ResponseWriter,r *http.Request){
 var input struct{Confirm bool `json:"confirm"`}
 if err:=decode(r,&input);err!=nil{writeError(w,400,err.Error());return}
 if !input.Confirm{writeError(w,400,"explicit confirmation is required");return}
 raw,err:=json.Marshal(map[string]any{"id":r.PathValue("id"),"confirm":true})
 if err!=nil{writeError(w,500,"invalid confirmation");return}
 output:=s.invoke(r.Context(),"health__health_food_confirm",raw,json.RawMessage("1"))
 if failed,_:=output["isError"].(bool);failed{writeError(w,502,"Health did not confirm the meal");return}
 writeJSON(w,200,output)
}
