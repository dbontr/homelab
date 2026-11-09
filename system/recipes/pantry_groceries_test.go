package recipes

import (
	"path/filepath"
	"testing"
)

func TestGroceryNeedsUseExactUnitAndDoNotConsumeInventory(t *testing.T) {
	s,err:=Open(filepath.Join(t.TempDir(),"recipes.json"))
	if err!=nil{t.Fatal(err)}
	recipe,err:=s.Save(sample("Tomato supper"))
	if err!=nil{t.Fatal(err)}
	_,err=s.AddMeal(Meal{RecipeID:recipe.ID,Date:"2032-06-08",Servings:4})
	if err!=nil{t.Fatal(err)}
	if _,err=s.SetPantry(PantryItem{Name:"Tomato",Unit:"g",Amount:1000},0);err!=nil{t.Fatal(err)}
	if _,err=s.SetPantry(PantryItem{Name:"Tomato",Unit:"count",Amount:1},1);err!=nil{t.Fatal(err)}
	if _,err=s.SetPantry(PantryItem{Name:"Olive oil",Unit:"tbsp",Amount:5},2);err!=nil{t.Fatal(err)}
	needs,err:=s.GroceryNeeds("2032-06-08","2032-06-08")
	if err!=nil{t.Fatal(err)}
	if needs.PantryRevision!=3 || len(needs.Items)!=2 {
		t.Fatalf("unexpected grocery needs: %+v",needs)
	}
	for _,item:=range needs.Items {
		switch item.Name {
		case "Tomato":
			if item.Required!=4 || item.Available!=1 || item.ToBuy!=3 || item.StockStatus!="insufficient" {
				t.Fatalf("mismatched tomato unit credited to count: %+v",item)
			}
		case "Olive oil":
			if item.Required!=2 || item.Available!=5 || item.ToBuy!=0 || item.StockStatus!="covered" {
				t.Fatalf("covered oil incorrectly put on shopping list: %+v",item)
			}
		default: t.Fatalf("unexpected item: %+v",item)
		}
	}
	again,err:=s.GroceryNeeds("2032-06-08","2032-06-08")
	if err!=nil || again.PantryRevision!=needs.PantryRevision || again.GroceryRevision!=needs.GroceryRevision{
		t.Fatalf("read-only needs mutated state: %+v %v",again,err)
	}
	if len(s.Meals())!=1 || len(s.Pantry().Items)!=3 {
		t.Fatal("grocery preview mutated scheduled meals or recorded inventory")
	}
	if _,err:=s.GroceryNeeds("2032-06-09","2032-06-08");err==nil {
		t.Fatal("invalid grocery range accepted")
	}
}

func TestGroceryNeedsExcludeExpiredStockAndFlagUnrecorded(t *testing.T) {
	s,err:=Open(filepath.Join(t.TempDir(),"recipes.json"))
	if err!=nil{t.Fatal(err)}
	recipe,err:=s.Save(sample("Tomato salad"))
	if err!=nil{t.Fatal(err)}
	if _,err:=s.AddMeal(Meal{RecipeID:recipe.ID,Date:"2032-06-08",Servings:2});err!=nil{t.Fatal(err)}
	if _,err:=s.SetPantry(PantryItem{Name:"Tomato",Unit:"count",Amount:99,Expires:"2020-01-01"},0);err!=nil{t.Fatal(err)}
	needs,err:=s.GroceryNeeds("","")
	if err!=nil{t.Fatal(err)}
	if len(needs.Items)!=2{t.Fatalf("missing ingredients: %+v",needs)}
	for _,item:=range needs.Items{
		switch item.Name{
		case "Tomato":
			if item.StockStatus!="expired" || item.Available!=0 || item.ToBuy!=item.Required {
				t.Fatalf("expired pantry quantity was available: %+v",item)
			}
		case "Olive oil":
			if item.StockStatus!="unrecorded" || item.Available!=0 || item.ToBuy!=item.Required {
				t.Fatalf("unknown stock was invented: %+v",item)
			}
		}
	}
}
