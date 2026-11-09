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
