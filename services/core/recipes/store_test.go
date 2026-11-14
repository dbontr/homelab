package recipes

import (
	"errors"
	"os"
	"math"
	"path/filepath"
	"testing"
)

func privateRecipeTestPath(t *testing.T, name string) string {
 t.Helper()
 directory := t.TempDir()
 if err := os.Chmod(directory, 0700); err != nil {
  t.Fatal(err)
 }
 return filepath.Join(directory, name)
}

func sample(title string) Recipe {
	return Recipe{Title: title, Servings: 2, Ingredients: []Ingredient{
		{Name:"Tomato", Amount:2, Unit:"count"},
		{Name:"Olive oil", Amount:1, Unit:"tbsp"},
	}, Steps: []Step{{Text:"Combine ingredients."}},
		Nutrition: &Nutrition{Method:"estimated", Source:"Manual calculation",
			PerServing:[]Nutrient{{Name:"protein",Amount:4,Unit:"g"}}}}
}

func TestRecipeLifecycleAndPersistence(t *testing.T) {
	path := privateRecipeTestPath(t, "recipes.json")
	store, err := Open(path)
	if err != nil { t.Fatal(err) }
	recipe, err := store.Save(sample("Tomato salad"))
	if err != nil { t.Fatal(err) }
	if recipe.ID == "" || recipe.Revision != 1 { t.Fatalf("invalid new recipe: %+v", recipe) }
	if len(store.List("salad")) != 1 || len(store.List("unknown")) != 0 { t.Fatal("search results incorrect") }
	copy, err := store.Get(recipe.ID)
	if err != nil { t.Fatal(err) }
	copy.Ingredients[0].Name = "Overwritten"
	if old, _ := store.Get(recipe.ID); old.Ingredients[0].Name != "Tomato" { t.Fatal("store aliases external slices") }
	recipe.Title = "Tomato and olive salad"
	changed, err := store.Save(recipe)
	if err != nil || changed.Revision != 2 { t.Fatalf("failed revision update: %v", err) }
	if _, err := store.Save(recipe); !errors.Is(err, ErrConflict) { t.Fatal("lost-update guard failed") }
	reopened, err := Open(path)
	if err != nil { t.Fatal(err) }
	if item, err := reopened.Get(recipe.ID); err != nil || item.Revision != 2 { t.Fatalf("persistence: %+v %v", item, err) }
	if err := reopened.Delete(recipe.ID, 1); !errors.Is(err, ErrConflict) { t.Fatal("stale delete accepted") }
	if err := reopened.Delete(recipe.ID, 2); err != nil { t.Fatal(err) }
}

func TestMealPlanAndGroceryConsolidation(t *testing.T) {
	store, err := Open(privateRecipeTestPath(t, "recipes.json"))
	if err != nil { t.Fatal(err) }
	a, _ := store.Save(sample("Salad"))
	b, _ := store.Save(sample("Another salad"))
	for _, pair := range []struct{id,date string; servings float64}{
		{a.ID,"2026-11-10",4},{b.ID,"2026-11-11",2},
	} {
		if _, err := store.AddMeal(Meal{RecipeID:pair.id,Date:pair.date,Servings:pair.servings}); err != nil { t.Fatal(err) }
	}
	if err := store.Delete(a.ID, a.Revision); !errors.Is(err, ErrConflict) { t.Fatal("scheduled recipe deleted") }
	items, err := store.Groceries("2026-11-10","2026-11-11")
	if err != nil { t.Fatal(err) }
	if len(items) != 2 { t.Fatalf("expected consolidated entries: %#v", items) }
	for _, item := range items {
		if item.Name=="Tomato" && item.Amount != 6 { t.Fatalf("serving scaling: %#v", item) }
		if item.Name=="Olive oil" && item.Amount != 3 { t.Fatalf("serving scaling: %#v", item) }
		if len(item.RecipeIDs) != 2 { t.Fatalf("recipe traceability: %#v", item) }
	}
	if limited,_:=store.Groceries("2026-11-10","2026-11-10");len(limited)==0 || limited[0].Amount == items[0].Amount {
		t.Fatal("date range did not change ingredient totals")
	}
	if _,err:=store.Groceries("2026-11-11","2026-11-10");err==nil { t.Fatal("invalid date range accepted") }
}

func TestNutritionAndImportAreExplicit(t *testing.T) {
	s, _ := Open(privateRecipeTestPath(t,"one.json"))
	r, _ := s.Save(sample("Soup"))
	values, err := s.Nutrition(r.ID, 3)
	if err != nil || values.Method!="estimated" || len(values.Nutrients)!=1 || values.Nutrients[0].Amount!=12 {
		t.Fatalf("nutrition provenance/portion: %#v %v",values,err)
	}
	if _,err:=s.Nutrition(r.ID, math.Inf(1));err==nil { t.Fatal("infinite portion accepted") }
	archive:=s.Export()
	target,_:=Open(privateRecipeTestPath(t,"two.json"))
	n,err:=target.Import(archive)
	if err!=nil||n!=1{t.Fatalf("import: %d %v",n,err)}
	if _,err:=target.Import(archive);!errors.Is(err,ErrConflict){t.Fatal("duplicate import overwrote data")}
	if len(target.List(""))!=1 { t.Fatal("conflicting import damaged data") }
}

func TestRecipeValidation(t *testing.T) {
	s,_:=Open(privateRecipeTestPath(t,"recipes.json"))
	for _, item:=range []Recipe{
		{Title:"Empty",Servings:2},
		{Title:"Bad quantity",Servings:2,Ingredients:[]Ingredient{{Name:"Rice",Amount:-1}},Steps:[]Step{{Text:"Cook"}}},
		{Title:"Bad URL",Servings:2,Ingredients:[]Ingredient{{Name:"Rice",Amount:1}},Steps:[]Step{{Text:"Cook"}},SourceURL:"http://example.org/recipe"},
	} {
		if _,err:=s.Save(item);err==nil { t.Fatalf("invalid recipe accepted: %+v",item) }
	}
}

func TestGroceryChecklistRevisionAndRestart(t *testing.T) {
 path:=privateRecipeTestPath(t,"recipes.json")
 s,err:=Open(path);if err!=nil{t.Fatal(err)}
 recipe,err:=s.Save(sample("Broth"));if err!=nil{t.Fatal(err)}
 if _,err=s.AddMeal(Meal{RecipeID:recipe.ID,Date:"2026-11-14",Servings:2});err!=nil{t.Fatal(err)}
 board,err:=s.Board("","");if err!=nil||len(board.Items)!=2{t.Fatalf("board: %+v %v",board,err)}
 key:=board.Items[0].Key
 updated,err:=s.MarkGrocery(key,true,board.Revision);if err!=nil||updated.Revision!=1{t.Fatalf("check: %+v %v",updated,err)}
 if _,err:=s.MarkGrocery(key,false,board.Revision);!errors.Is(err,ErrConflict){t.Fatal("lost update accepted")}
 reopened,err:=Open(path);if err!=nil{t.Fatal(err)}
 persisted,err:=reopened.Board("","");if err!=nil||!persisted.Items[0].Checked{t.Fatalf("not persisted: %+v %v",persisted,err)}
 if _,err:=reopened.MarkGrocery("not-a-key",true,1);err==nil{t.Fatal("invalid grocery key accepted")}
}

func TestStateDirectoryAllowsOnlyConfiguredPrivateSymlink(t *testing.T) {
    base:=t.TempDir()
    target:=filepath.Join(base,"private")
    if err:=os.Mkdir(target,0700);err!=nil{t.Fatal(err)}
    link:=filepath.Join(base,"state")
    if err:=os.Symlink(target,link);err!=nil{t.Fatal(err)}
    if _,err:=Open(filepath.Join(link,"recipes.json"));err==nil{
        t.Fatal("unconfigured symlink to recipe data was accepted")
    }
    t.Setenv("STATE_DIRECTORY",link)
    if _,err:=Open(filepath.Join(link,"recipes.json"));err!=nil{
        t.Fatalf("systemd state directory mapping rejected: %v",err)
    }
    if err:=os.Chmod(target,0755);err!=nil{t.Fatal(err)}
    if _,err:=Open(filepath.Join(link,"recipes.json"));err==nil{
        t.Fatal("public recipe state directory was accepted")
    }
}
