package transport

import "testing"

func TestCapabilityReadsKeepPersonalConsentSeparateFromResearch(t *testing.T){
 for _,action:=range []string{"project_tasks","learning_summary","learning_sessions","learning_reviews","health_goals","health_records","health_training_sets","health_food_proposals","recipe_search","recipe_meals","media_requests"}{
  stage:=CapabilityStage{ID:"personal",Action:action}
  if action=="recipe_search"{stage.Query="pasta"}
  if err:=validCapabilityStage(stage,false);err==nil{t.Errorf("%s bypassed personal consent",action)}
  if err:=validCapabilityStage(stage,true);err!=nil{t.Errorf("%s rejected with consent: %v",action,err)}
 }
 for _,action:=range []string{"project_context","engineering_runs","research_search"}{
  stage:=CapabilityStage{ID:"project",Action:action}
  if action=="research_search"{stage.Query="hypothesis"}
  if err:=validCapabilityStage(stage,false);err!=nil{t.Errorf("%s wrongly needs personal access: %v",action,err)}
 }
}

func TestWorkflowRecipeAndStudyPackValidation(t *testing.T){
 for _,action:=range []string{"task_review","recipe_groceries","recipe_nutrition"}{
  stage:=CapabilityStage{ID:"personal",Action:action}
  if action=="recipe_nutrition"{stage.Query="valid-id"}
  if err:=validCapabilityStage(stage,false);err==nil{t.Fatalf("%s bypassed personal consent",action)}
  if err:=validCapabilityStage(stage,true);err!=nil{t.Fatalf("%s rejected authorized read: %v",action,err)}
 }
 for _,stage:=range []CapabilityStage{
  {ID:"invalid",Action:"recipe_groceries",Query:"tomorrow"},
  {ID:"invalid",Action:"recipe_groceries",Limit:20},
  {ID:"invalid",Action:"study_pack_preview",Query:"unverified passage"},
 } {
  if err:=validCapabilityStage(stage,true);err==nil{t.Fatalf("invalid stage accepted: %+v",stage)}
 }
 if err:=validCapabilityStage(CapabilityStage{ID:"cited",Action:"study_pack_preview"},false);err!=nil{t.Fatal(err)}
}
