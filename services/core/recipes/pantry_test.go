package recipes

import (
	"errors"
	"path/filepath"
	"testing"
)

func TestPantryInventoryRevisionDurabilityAndImport(t *testing.T){
	path:=filepath.Join(t.TempDir(),"recipes.json")
	s,err:=Open(path)
	if err!=nil{t.Fatal(err)}
	item:=PantryItem{Name:"Tomato",Unit:"count",Amount:3}
	board,err:=s.SetPantry(item,0)
	if err!=nil||board.Revision!=1||len(board.Items)!=1{t.Fatalf("pantry write failed: %+v %v",board,err)}
	if _,err:=s.SetPantry(PantryItem{Name:"Olive oil",Unit:"tbsp",Amount:2},0);!errors.Is(err,ErrConflict){
		t.Fatalf("stale pantry update accepted: %v",err)
	}
	reopened,err:=Open(path)
	if err!=nil{t.Fatal(err)}
	if got:=reopened.Pantry();got.Revision!=1||len(got.Items)!=1||got.Items[0].Amount!=3{
		t.Fatalf("pantry did not survive restart: %+v",got)
	}
	archive:=reopened.Export()
	target,err:=Open(filepath.Join(t.TempDir(),"copy.json"))
	if err!=nil{t.Fatal(err)}
	if _,err:=target.Import(archive);err!=nil{t.Fatal(err)}
	if got:=target.Pantry();len(got.Items)!=1||got.Items[0].Unit!="count"{
		t.Fatalf("pantry did not export: %+v",got)
	}
	if _,err:=target.Import(archive);!errors.Is(err,ErrConflict){
		t.Fatalf("duplicate pantry import overwrote records: %v",err)
	}
	key:=groceryKey(item.Name,item.Unit)
	after,err:=reopened.RemovePantry(key,1)
	if err!=nil||after.Revision!=2||len(after.Items)!=0{
		t.Fatalf("pantry deletion failed: %+v %v",after,err)
	}
	if _,err:=reopened.RemovePantry(key,1);!errors.Is(err,ErrConflict){t.Fatal("stale pantry delete accepted")}
}

func TestPantryRecommendationPreservesUnitsAndNutritionProvenance(t *testing.T){
	s,err:=Open(filepath.Join(t.TempDir(),"recipes.json"))
	if err!=nil{t.Fatal(err)}
	r,err:=s.Save(sample("Tomato salad"))
	if err!=nil{t.Fatal(err)}
	if _,err:=s.SetPantry(PantryItem{Name:"Tomato",Unit:"count",Amount:1},0);err!=nil{t.Fatal(err)}
	if _,err:=s.SetPantry(PantryItem{Name:"Olive oil",Unit:"tbsp",Amount:3},1);err!=nil{t.Fatal(err)}
	result,err:=s.RecommendFromPantry(PantryRecommendationRequest{
		Servings:1,Targets:[]NutrientTarget{
			{Name:"protein",Unit:"g",Remaining:4},
			{Name:"vitamin c",Unit:"mg",Remaining:30},
		},
	})
	if err!=nil||len(result.Matches)!=1{t.Fatalf("recommendation failed: %+v %v",result,err)}
	got:=result.Matches[0]
	if got.RecipeID!=r.ID||got.IngredientCoverage!=1||got.KnownTargets!=1||
		got.NutrientFit!=1||got.Score!=1||len(got.Missing)!=0||
		got.NutritionMethod!="estimated"||len(got.UnknownTargets)!=1{
		t.Fatalf("nutrition or inventory misrepresented: %+v",got)
	}
	if _,err:=s.SetPantry(PantryItem{Name:"Tomato",Unit:"g",Amount:1000},2);err!=nil{t.Fatal(err)}
	if _,err:=s.RemovePantry(groceryKey("Tomato","count"),3);err!=nil{t.Fatal(err)}
	result,err=s.RecommendFromPantry(PantryRecommendationRequest{Servings:1})
	if err!=nil{t.Fatal(err)}
	if len(result.Matches[0].Missing)!=1||result.Matches[0].Missing[0].Name!="Tomato" {
		t.Fatalf("unit conversion was incorrectly assumed: %+v",result.Matches[0])
	}
	if len(s.Meals())!=0{t.Fatal("recipe recommendation recorded consumption")}
}

func TestPantryExpiryIsNotAssumedAvailable(t *testing.T){
	s,err:=Open(filepath.Join(t.TempDir(),"recipes.json"))
	if err!=nil{t.Fatal(err)}
	if _,err:=s.Save(sample("Tomato salad"));err!=nil{t.Fatal(err)}
	if _,err:=s.SetPantry(PantryItem{Name:"Tomato",Unit:"count",Amount:100,Expires:"2020-01-01"},0);err!=nil{t.Fatal(err)}
	got,err:=s.RecommendFromPantry(PantryRecommendationRequest{})
	if err!=nil{t.Fatal(err)}
	if len(got.Matches)!=1||len(got.Matches[0].Missing)!=2{
		t.Fatalf("expired stock was used as available: %+v",got)
	}
	for _,bad:=range []PantryItem{
		{Name:" Rice ",Unit:"g",Amount:1},
		{Name:"Rice",Unit:"g",Amount:-1},
		{Name:"Rice",Unit:"g",Amount:1,Expires:"2026-15-99"},
	}{
		if _,err:=s.SetPantry(bad,1);err==nil{t.Fatalf("invalid inventory accepted: %+v",bad)}
	}
}
