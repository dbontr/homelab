package transport

import (
	"net/http"

	"github.com/dbontr/homelab-new/system/recipes"
)

func (s *Server) pantryCollection(w http.ResponseWriter,r *http.Request){
	if s.recipes==nil{writeError(w,503,"recipe storage is unavailable");return}
	if r.Method==http.MethodGet{
		writeJSON(w,200,s.recipes.Pantry());return
	}
	var request struct{
		Item recipes.PantryItem `json:"item"`
		ExpectedRevision int `json:"expected_revision"`
	}
	if err:=decode(r,&request);err!=nil{writeError(w,400,err.Error());return}
	board,err:=s.recipes.SetPantry(request.Item,request.ExpectedRevision)
	if err!=nil{recipeFailure(w,err);return}
	writeJSON(w,200,board)
}
func (s *Server) pantryItem(w http.ResponseWriter,r *http.Request){
	if s.recipes==nil{writeError(w,503,"recipe storage is unavailable");return}
	var request struct{ExpectedRevision int `json:"expected_revision"`}
	if err:=decode(r,&request);err!=nil{writeError(w,400,err.Error());return}
	board,err:=s.recipes.RemovePantry(r.PathValue("key"),request.ExpectedRevision)
	if err!=nil{recipeFailure(w,err);return}
	writeJSON(w,200,board)
}
func (s *Server) pantryRecommendations(w http.ResponseWriter,r *http.Request){
	if s.recipes==nil{writeError(w,503,"recipe storage is unavailable");return}
	var request recipes.PantryRecommendationRequest
	if err:=decode(r,&request);err!=nil{writeError(w,400,err.Error());return}
	result,err:=s.recipes.RecommendFromPantry(request)
	if err!=nil{recipeFailure(w,err);return}
	writeJSON(w,200,result)
}
