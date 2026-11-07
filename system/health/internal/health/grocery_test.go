package health

import(
 "context"
 "errors"
 "testing"
)
func TestGroceryFromMealPlanAndVersioning(t *testing.T){
 s:=testStore(t);ctx:=context.Background()
 p,err:=s.SavePlan(ctx,Plan{Kind:"meal",Title:"Weekly meals",Sessions:[]Session{
  {ID:"breakfast",Date:"2026-11-01",Title:"Breakfast",Items:[]MealItem{{Name:"Oats",Quantity:80,Unit:"g"},{Name:"Milk",Quantity:250,Unit:"mL"}}},
  {ID:"lunch",Date:"2026-11-02",Title:"Lunch",Items:[]MealItem{{Name:"Oats",Quantity:40,Unit:"g"}}},
 }},0)
 if err!=nil{t.Fatal(err)}
 items,err:=s.GroceryFromPlan(ctx,p.ID);if err!=nil{t.Fatal(err)}
 if len(items)!=2{t.Fatalf("unexpected grocery items: %+v",items)}
 qty:=0.0
 for _,item:=range items{if item.Name=="Oats"{qty=item.Quantity}}
 if qty!=120{t.Fatalf("expected 120 g of oats, got %v",qty)}
 list,err:=s.SaveGrocery(ctx,GroceryList{Title:"Groceries",Items:items},0)
 if err!=nil{t.Fatal(err)}
 list.Items[0].Checked=true
 revised,err:=s.SaveGrocery(ctx,list,1)
 if err!=nil||revised.Revision!=2{t.Fatalf("list edit: %+v %v",revised,err)}
 if _,err=s.SaveGrocery(ctx,list,1);!errors.Is(err,ErrConflict){t.Fatalf("stale grocery edit accepted: %v",err)}
 result,err:=s.GetGrocery(ctx,list.ID)
 if err!=nil||!result.Items[0].Checked{t.Fatalf("grocery edit not persisted: %+v %v",result,err)}
}
func TestGroceriesRefuseInvalidQuantityAndNonmealPlans(t *testing.T){
 s:=testStore(t)
 ctx:=context.Background()
 _,err:=s.SaveGrocery(ctx,GroceryList{Title:"Invalid",Items:[]GroceryItem{{
  ID:"x",Name:"Eggs",Quantity:-2,Unit:"each",
 }}},0)
 if err==nil{t.Fatal("negative grocery quantity accepted")}
 p,err:=s.SavePlan(ctx,Plan{Kind:"workout",Title:"Training",Sessions:[]Session{}},0)
 if err!=nil{t.Fatal(err)}
 if _,err=s.GroceryFromPlan(ctx,p.ID);err==nil{t.Fatal("workout plan converted to groceries")}
}
