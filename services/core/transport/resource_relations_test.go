package transport

import (
 "context"
 "path/filepath"
 "strings"
 "testing"

 "github.com/dbontr/homelab-new/system/recipes"
 "github.com/dbontr/homelab-new/system/store"
)

func hasResourceLink(links []ResourceLink,relation,reference string)bool{
 for _,link:=range links{
  if link.Relation==relation&&link.Reference==reference{return true}
 }
 return false
}

func TestOwnerResourcesExposeReversibleRelationsWithoutCopyingRecords(t *testing.T){
 personal,err:=store.Open(filepath.Join(t.TempDir(),"personal.json"),"UTC")
 if err!=nil{t.Fatal(err)}
 goal,err:=personal.AddGoal("Complete senior design","")
 if err!=nil{t.Fatal(err)}
 root,err:=personal.AddTask(store.TaskInput{
  Title:"Design system",GoalID:goal.ID,
 })
 if err!=nil{t.Fatal(err)}
 child,err:=personal.AddTask(store.TaskInput{
  Title:"Verify system",GoalID:goal.ID,ParentID:root.ID,
  DependsOn:[]string{root.ID},
 })
 if err!=nil{t.Fatal(err)}
 other,err:=personal.AddTask(store.TaskInput{Title:"Unrelated work"})
 if err!=nil{t.Fatal(err)}
 gateway,err:=New(personal,strings.Repeat("t",40),nil)
 if err!=nil{t.Fatal(err)}
 goalRef,err:=gateway.resource(context.Background(),"goal",goal.ID)
 if err!=nil{t.Fatal(err)}
 if len(goalRef.Links)!=2||
  !hasResourceLink(goalRef.Links,"task",resourceRef("task",root.ID))||
  !hasResourceLink(goalRef.Links,"task",resourceRef("task",child.ID))||
  hasResourceLink(goalRef.Links,"task",resourceRef("task",other.ID)){
  t.Fatalf("goal resource relationships are incomplete or overbroad: %+v",goalRef.Links)
 }
 rootRef,err:=gateway.resource(context.Background(),"task",root.ID)
 if err!=nil{t.Fatal(err)}
 if !hasResourceLink(rootRef.Links,"child",resourceRef("task",child.ID))||
  !hasResourceLink(rootRef.Links,"dependent",resourceRef("task",child.ID))||
  !hasResourceLink(rootRef.Links,"goal",resourceRef("goal",goal.ID)){
  t.Fatalf("task inverse relations are absent: %+v",rootRef.Links)
 }
 childRef,err:=gateway.resource(context.Background(),"task",child.ID)
 if err!=nil{t.Fatal(err)}
 if !hasResourceLink(childRef.Links,"parent",resourceRef("task",root.ID))||
  !hasResourceLink(childRef.Links,"prerequisite",resourceRef("task",root.ID)){
  t.Fatalf("task parent and prerequisite links did not survive: %+v",childRef.Links)
 }
 recipeStore,err:=recipes.Open(filepath.Join(t.TempDir(),"recipes","recipes.json"))
 if err!=nil{t.Fatal(err)}
 recipe,err:=recipeStore.Save(recipes.Recipe{
  Title:"Beans",Servings:2,
  Ingredients:[]recipes.Ingredient{{Name:"Beans",Amount:200,Unit:"g"}},
  Steps:[]recipes.Step{{Text:"Boil beans."}},
 })
 if err!=nil{t.Fatal(err)}
 meal,err:=recipeStore.AddMeal(recipes.Meal{
  RecipeID:recipe.ID,Date:"2027-01-04",Servings:1,
 })
 if err!=nil{t.Fatal(err)}
 gateway.SetRecipes(recipeStore)
 recipeRef,err:=gateway.resource(context.Background(),"recipe",recipe.ID)
 if err!=nil{t.Fatal(err)}
 if len(recipeRef.Links)!=1||
  !hasResourceLink(recipeRef.Links,"meal",resourceRef("meal",meal.ID)){
  t.Fatalf("recipe did not expose its scheduled meal: %+v",recipeRef.Links)
 }
 mealRef,err:=gateway.resource(context.Background(),"meal",meal.ID)
 if err!=nil{t.Fatal(err)}
 if !hasResourceLink(mealRef.Links,"recipe",resourceRef("recipe",recipe.ID)){
  t.Fatalf("meal lost canonical recipe identity: %+v",mealRef.Links)
 }
 if len(personal.Tasks(""))!=3||len(recipeStore.Meals())!=1{
  t.Fatal("resource resolution changed owner records")
 }
}

func TestResourceIdentifiersSupportFullLearningSessionIdentity(t *testing.T){
 sessionID:=strings.Repeat("a",128)
 kind,id,err:=parseResourceReference(resourceRef("learning_session",sessionID))
 if err!=nil||kind!="learning_session"||id!=sessionID{
  t.Fatalf("supported learning session ID cannot round-trip: %v",err)
 }
 for _,invalid:=range []string{
  resourceRef("learning_session",strings.Repeat("a",129)),
  "homelab:learning_session:../../etc/passwd",
  "homelab:unknown:known",
  "homelab:learning_session:with:colon",
 }{
  if _,_,err:=parseResourceReference(invalid);err==nil{
   t.Fatalf("invalid reference was accepted: %q",invalid)
  }
 }
}
