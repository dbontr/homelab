package recipes

import (
 "strings"
 "testing"
)

func TestSchemaRecipePreviewRequiresQuantityReview(t *testing.T){
 text:=`<html><head><script type="application/ld+json">{"@context":"https://schema.org","@graph":[{"@type":"Recipe","name":"Roasted squash","recipeYield":"4 servings","prepTime":"PT15M","cookTime":"PT1H","recipeIngredient":["1 1/2 cups squash","salt to taste"],"recipeInstructions":[{"@type":"HowToStep","text":"Roast the squash."}]}]}</script></head></html>`
 draft,err:=ParseSchemaOrg(text)
 if err!=nil{t.Fatal(err)}
 if draft.Title!="Roasted squash"||draft.Servings!=4||draft.CookMinutes!=60||draft.PrepMinutes!=15{t.Fatalf("parsed %#v",draft)}
 if len(draft.Ingredients)!=2||draft.Ingredients[0].Amount!=1.5||draft.Ingredients[1].Amount!=0{t.Fatalf("ingredients %#v",draft.Ingredients)}
 if err:=validateRecipe(draft);err==nil{t.Fatal("unreviewed quantity accepted")}
 draft.Ingredients[1].Amount=1
 if err:=validateRecipe(draft);err!=nil{t.Fatal(err)}
}

func TestSchemaRecipeRejectsScriptsAndMissingData(t *testing.T){
 if _,err:=ParseSchemaOrg(`<script>alert(1)</script>`);err==nil{t.Fatal("accepted script")}
 if _,err:=ParseSchemaOrg(strings.Repeat("X",513<<10));err==nil{t.Fatal("accepted oversized source")}
 if _,err:=ParseSchemaOrg(`{"@type":"Recipe","name":"Blank"}`);err==nil{t.Fatal("accepted incomplete recipe")}
}
