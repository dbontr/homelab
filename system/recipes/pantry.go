package recipes

import (
	"errors"
	"math"
	"sort"
	"strings"
	"time"
)

// Pantry inventory is separate from the derived grocery checklist. Quantities
// are never converted between units without a measured conversion.
type PantryItem struct {
	Key string `json:"key,omitempty"`
	Name string `json:"name"`
	Unit string `json:"unit,omitempty"`
	Amount float64 `json:"amount"`
	Expires string `json:"expires,omitempty"`
}
type PantryBoard struct {
	Revision int `json:"revision"`
	Items []PantryItem `json:"items"`
}

type NutrientTarget struct {
	Name string `json:"name"`
	Unit string `json:"unit"`
	Remaining float64 `json:"remaining"`
}
type PantryRecommendationRequest struct {
	Servings float64 `json:"servings,omitempty"`
	Targets []NutrientTarget `json:"targets,omitempty"`
	Limit int `json:"limit,omitempty"`
}
type PantryShortage struct {
	Name string `json:"name"`
	Unit string `json:"unit,omitempty"`
	Required float64 `json:"required"`
	Available float64 `json:"available"`
	Shortage float64 `json:"shortage"`
	Reason string `json:"reason"`
}
type PantryRecipeMatch struct {
	RecipeID string `json:"recipe_id"`
	Title string `json:"title"`
	Servings float64 `json:"servings"`
	Score float64 `json:"score"`
	IngredientCoverage float64 `json:"ingredient_coverage"`
	NutrientFit float64 `json:"nutrient_fit"`
	KnownTargets int `json:"known_targets"`
	UnknownTargets []string `json:"unknown_targets"`
	NutritionMethod string `json:"nutrition_method,omitempty"`
	NutritionSource string `json:"nutrition_source,omitempty"`
	Missing []PantryShortage `json:"missing"`
}
type PantryRecipeResults struct {
	PantryRevision int `json:"pantry_revision"`
	Matches []PantryRecipeMatch `json:"matches"`
	Note string `json:"note"`
}

func validatePantryItem(item PantryItem)error{
	if strings.TrimSpace(item.Name)!=item.Name || item.Name=="" ||
		len(item.Name)>160 || strings.ContainsAny(item.Name,"\x00\r\n") ||
		len(item.Unit)>32 || strings.TrimSpace(item.Unit)!=item.Unit ||
		strings.ContainsAny(item.Unit,"\x00\r\n") ||
		!finitePositive(item.Amount) || item.Amount>1e8 ||
		item.Key!="" && item.Key!=groceryKey(item.Name,item.Unit) {
		return errors.New("pantry item must have a name, positive amount and exact unit")
	}
	if item.Expires!="" {
		if _,err:=time.Parse("2006-01-02",item.Expires);err!=nil {
			return errors.New("pantry expiry must be YYYY-MM-DD")
		}
	}
	return nil
}

func pantryBoard(data snapshot)PantryBoard{
	out:=PantryBoard{Revision:data.PantryRevision,Items:make([]PantryItem,0,len(data.Pantry))}
	for _,item:=range data.Pantry{
		item.Key=groceryKey(item.Name,item.Unit)
		out.Items=append(out.Items,item)
	}
	sort.Slice(out.Items,func(i,j int)bool{
		a,b:=out.Items[i],out.Items[j]
		if strings.EqualFold(a.Name,b.Name){return a.Unit<b.Unit}
		return strings.ToLower(a.Name)<strings.ToLower(b.Name)
	})
	return out
}
func (s *Store) Pantry()PantryBoard{
	s.mu.RLock();defer s.mu.RUnlock()
	return pantryBoard(s.data)
}

func (s *Store) SetPantry(item PantryItem,expectedRevision int)(PantryBoard,error){
	if err:=validatePantryItem(item);err!=nil{return PantryBoard{},err}
	s.mu.Lock();defer s.mu.Unlock()
	if expectedRevision!=s.data.PantryRevision{return PantryBoard{},ErrConflict}
	key:=groceryKey(item.Name,item.Unit)
	if _,exists:=s.data.Pantry[key];!exists&&len(s.data.Pantry)>=500 {
		return PantryBoard{},errors.New("pantry inventory capacity reached")
	}
	next:=duplicate(s.data)
	item.Key=""
	next.Pantry[key]=item
	next.PantryRevision++
	if err:=s.save(next);err!=nil{return PantryBoard{},err}
	return pantryBoard(next),nil
}

func (s *Store) RemovePantry(key string,expectedRevision int)(PantryBoard,error){
	if len(key)!=64{return PantryBoard{},errors.New("invalid pantry key")}
	for _,c:=range key{if !strings.ContainsRune("0123456789abcdef",c){return PantryBoard{},errors.New("invalid pantry key")}}
	s.mu.Lock();defer s.mu.Unlock()
	if expectedRevision!=s.data.PantryRevision{return PantryBoard{},ErrConflict}
	if _,exists:=s.data.Pantry[key];!exists{return PantryBoard{},ErrNotFound}
	next:=duplicate(s.data)
	delete(next.Pantry,key)
	next.PantryRevision++
	if err:=s.save(next);err!=nil{return PantryBoard{},err}
	return pantryBoard(next),nil
}

// RecommendFromPantry is an advisory calculation against explicit inventory
// and caller-supplied remaining nutrient targets. It never records a meal or
// treats an unrecorded ingredient as known absent.
func (s *Store) RecommendFromPantry(req PantryRecommendationRequest)(PantryRecipeResults,error){
	if req.Servings==0{req.Servings=1}
	if !finitePositive(req.Servings)||req.Servings>100||
		len(req.Targets)>32 || req.Limit<0 || req.Limit>100 {
		return PantryRecipeResults{},errors.New("invalid recommendation request")
	}
	if req.Limit==0{req.Limit=20}
	targets:=map[string]NutrientTarget{}
	for _,target:=range req.Targets{
		if strings.TrimSpace(target.Name)==""||len(target.Name)>100||
			strings.TrimSpace(target.Unit)==""||len(target.Unit)>30||
			math.IsNaN(target.Remaining)||math.IsInf(target.Remaining,0)||
			target.Remaining<0||target.Remaining>1e9{
			return PantryRecipeResults{},errors.New("invalid nutrient target")
		}
		key:=groceryKey(target.Name,target.Unit)
		if _,exists:=targets[key];exists{return PantryRecipeResults{},errors.New("duplicate nutrient target")}
		targets[key]=target
	}
	s.mu.RLock();defer s.mu.RUnlock()
	result:=PantryRecipeResults{
		PantryRevision:s.data.PantryRevision,
		Matches:make([]PantryRecipeMatch,0),
		Note:"Ingredient units must match exactly; unrecorded and expired supplies are not assumed available. Nutrient targets are caller-supplied and missing nutrient measurements remain unknown. No food is logged.",
	}
	today:=time.Now().UTC().Format("2006-01-02")
	for _,recipe:=range s.data.Recipes{
		match:=PantryRecipeMatch{
			RecipeID:recipe.ID,Title:recipe.Title,Servings:req.Servings,
			Missing:[]PantryShortage{},UnknownTargets:[]string{},
		}
		needs:=map[string]PantryShortage{}
		for _,ingredient:=range recipe.Ingredients{
			key:=groceryKey(ingredient.Name,ingredient.Unit)
			current:=needs[key]
			current.Name=ingredient.Name
			current.Unit=ingredient.Unit
			current.Required+=ingredient.Amount*req.Servings/recipe.Servings
			needs[key]=current
		}
		coverage:=0.0
		for key,need:=range needs{
			stock,recorded:=s.data.Pantry[key]
			if recorded{
				if stock.Expires!=""&&stock.Expires<today{
					need.Reason="recorded stock has expired"
				}else{
					need.Available=stock.Amount
				}
			}else{
				need.Reason="no recorded pantry quantity"
			}
			coverage+=math.Min(1,need.Available/need.Required)
			if need.Available+1e-9<need.Required{
				need.Shortage=math.Max(0,need.Required-need.Available)
				if need.Reason==""{need.Reason="recorded quantity is insufficient"}
				match.Missing=append(match.Missing,need)
			}
		}
		if len(needs)>0{match.IngredientCoverage=coverage/float64(len(needs))}
		sort.Slice(match.Missing,func(i,j int)bool{
			if strings.EqualFold(match.Missing[i].Name,match.Missing[j].Name){return match.Missing[i].Unit<match.Missing[j].Unit}
			return strings.ToLower(match.Missing[i].Name)<strings.ToLower(match.Missing[j].Name)
		})
		known:=0
		fit:=0.0
		if recipe.Nutrition!=nil{
			match.NutritionMethod=recipe.Nutrition.Method
			match.NutritionSource=recipe.Nutrition.Source
		}
		for key,target:=range targets{
			if target.Remaining==0{continue}
			found:=false
			if recipe.Nutrition!=nil{
				for _,nutrient:=range recipe.Nutrition.PerServing{
					if groceryKey(nutrient.Name,nutrient.Unit)!=key{continue}
					known++
					fit+=math.Min(1,nutrient.Amount*req.Servings/target.Remaining)
					found=true
					break
				}
			}
			if !found{
				match.UnknownTargets=append(match.UnknownTargets,target.Name+" ("+target.Unit+")")
			}
		}
		sort.Strings(match.UnknownTargets)
		if known>0{match.NutrientFit=fit/float64(known)}
		match.KnownTargets=known
		match.Score=match.IngredientCoverage
		if known>0{match.Score=0.65*match.IngredientCoverage+0.35*match.NutrientFit}
		result.Matches=append(result.Matches,match)
	}
	sort.Slice(result.Matches,func(i,j int)bool{
		a,b:=result.Matches[i],result.Matches[j]
		if a.Score!=b.Score{return a.Score>b.Score}
		if len(a.Missing)!=len(b.Missing){return len(a.Missing)<len(b.Missing)}
		if a.Title!=b.Title{return a.Title<b.Title}
		return a.RecipeID<b.RecipeID
	})
	if len(result.Matches)>req.Limit{result.Matches=result.Matches[:req.Limit]}
	return result,nil
}
