package transport

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dbontr/homelab-new/system/recipes"
	"github.com/dbontr/homelab-new/system/store"
)

func TestPantryHTTPRequiresAuthorizationAndRevision(t *testing.T){
	db,err:=store.Open(filepath.Join(t.TempDir(),"records.json"),"UTC")
	if err!=nil{t.Fatal(err)}
	s,err:=New(db,strings.Repeat("p",40),nil)
	if err!=nil{t.Fatal(err)}
	r,err:=recipes.Open(filepath.Join(t.TempDir(),"recipes.json"))
	if err!=nil{t.Fatal(err)}
	if _,err:=r.Save(recipes.Recipe{
		Title:"Rice",Servings:2,
		Ingredients:[]recipes.Ingredient{{Name:"Rice",Amount:200,Unit:"g"}},
		Steps:[]recipes.Step{{Text:"Boil the rice."}},
	});err!=nil{t.Fatal(err)}
	s.SetRecipes(r)
	h:=s.Handler()
	token:=strings.Repeat("p",40)
	if res:=request(h,"GET","/v1/pantry","","");res.Code!=401{
		t.Fatalf("anonymous pantry listing accepted: %d",res.Code)
	}
	body:=`{"item":{"name":"Rice","unit":"g","amount":500},"expected_revision":0}`
	if res:=request(h,"PUT","/v1/pantry",body,"");res.Code!=401{
		t.Fatalf("anonymous pantry write accepted: %d",res.Code)
	}
	if res:=request(h,"PUT","/v1/pantry",body,token);res.Code!=200{
		t.Fatalf("pantry update failed: %d %s",res.Code,res.Body.String())
	}
	if res:=request(h,"PUT","/v1/pantry",body,token);res.Code!=409{
		t.Fatalf("stale pantry write accepted: %d",res.Code)
	}
	plan:=request(h,"POST","/v1/recipes/recommend",`{"servings":1,"limit":5}`,token)
	if plan.Code!=200||!strings.Contains(plan.Body.String(),`"ingredient_coverage":1`){
		t.Fatalf("pantry recommendations unavailable: %d %s",plan.Code,plan.Body.String())
	}
	if len(r.Meals())!=0{t.Fatal("recommendation mutated meal records")}
	visible:=request(h,"GET","/v1/pantry","",token)
	var board struct{Items []struct{Key string `json:"key"`;Name string `json:"name"`}}
	if err:=json.Unmarshal(visible.Body.Bytes(),&board);err!=nil{t.Fatal(err)}
	if len(board.Items)!=1 || board.Items[0].Key==""{t.Fatal("pantry key absent from owner view")}
	key:=board.Items[0].Key
	removed:=request(h,"DELETE","/v1/pantry/items/"+key,`{"expected_revision":1}`,token)
	if removed.Code!=200||!strings.Contains(removed.Body.String(),`"items":[]`){
		t.Fatalf("pantry removal failed: %d %s",removed.Code,removed.Body.String())
	}
}


func TestGroceryNeedsHTTPIsPrivateAndReadOnly(t *testing.T) {
	db,err:=store.Open(filepath.Join(t.TempDir(),"tasks.json"),"UTC")
	if err!=nil{t.Fatal(err)}
	s,err:=New(db,strings.Repeat("n",40),nil)
	if err!=nil{t.Fatal(err)}
	r,err:=recipes.Open(filepath.Join(t.TempDir(),"recipes.json"))
	if err!=nil{t.Fatal(err)}
	recipe,err:=r.Save(recipes.Recipe{
		Title:"Rice",Servings:2,
		Ingredients:[]recipes.Ingredient{{Name:"Rice",Amount:200,Unit:"g"}},
		Steps:[]recipes.Step{{Text:"Cook."}},
	})
	if err!=nil{t.Fatal(err)}
	if _,err=r.AddMeal(recipes.Meal{RecipeID:recipe.ID,Date:"2032-06-08",Servings:2});err!=nil{t.Fatal(err)}
	s.SetRecipes(r)
	h:=s.Handler()
	path:="/v1/groceries/needs?start=2032-06-08&end=2032-06-08"
	if result:=request(h,"GET",path,"","");result.Code!=401{
		t.Fatalf("anonymous grocery inventory read accepted: %d",result.Code)
	}
	result:=request(h,"GET",path,"",strings.Repeat("n",40))
	if result.Code!=200||!strings.Contains(result.Body.String(),`"to_buy":200`) ||
		!strings.Contains(result.Body.String(),`"stock_status":"unrecorded"`) {
		t.Fatalf("grocery needs response incorrect: %d %s",result.Code,result.Body.String())
	}
	bad:=request(h,"GET","/v1/groceries/needs?start=2032-06-09&end=2032-06-08","",strings.Repeat("n",40))
	if bad.Code!=400 {t.Fatalf("invalid date range accepted: %d",bad.Code)}
}
