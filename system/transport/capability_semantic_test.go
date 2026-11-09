package transport

import "testing"

func TestWorkflowSemanticQueryRequiresValidSourceQuery(t *testing.T){
 stage:=CapabilityStage{ID:"semantic",Action:"research_semantic_query",Query:"wave interference",Limit:8}
 if err:=validCapabilityStage(stage,false);err!=nil{t.Fatal(err)}
 stage.Query=""
 if err:=validCapabilityStage(stage,false);err==nil{t.Fatal("empty semantic query accepted")}
 stage.Query="wave interference"
 stage.Limit=26
 if err:=validCapabilityStage(stage,false);err==nil{t.Fatal("unbounded semantic query accepted")}
}
