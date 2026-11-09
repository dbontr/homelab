package store

import "testing"

func TestReviewedSourceCurriculumRequiresFullAuditAndExplicitImport(t *testing.T){
 s:=newTestStore(t)
 source,err:=s.IngestText("Electrostatics notes","notes","text/plain",
  "An electric field is derived from an electric potential. "+
  "Electric flux through a closed surface measures enclosed charge.")
 if err!=nil{t.Fatal(err)}
 id:=source["id"].(string)
 early,err:=s.PreviewSourceCurriculum(id,10)
 if err!=nil{t.Fatal(err)}
 if early["curriculum"].(SourceCurriculum).Ready{
  t.Fatal("unextracted source was considered a complete curriculum")
 }
 span,err:=s.NextExtraction(id)
 if err!=nil{t.Fatal(err)}
 batch,err:=s.ProposeExtraction(span["span_id"].(string),ExtractionProposal{
  Claims:[]ExtractionCandidate{
   {Statement:"Electric fields derive from electric potential",Quote:"An electric field is derived from an electric potential",Context:"electrostatics",Domain:"physics"},
   {Statement:"Gauss flux is related to enclosed charge",Quote:"Electric flux through a closed surface measures enclosed charge",Context:"electrostatics",Domain:"physics"},
  },
 })
 if err!=nil{t.Fatal(err)}
 if _,err:=s.AuditExtraction(batch["batch_id"].(string),
  []ExtractionDecision{{Index:0,Action:"accept"},{Index:1,Action:"accept"}},nil);err!=nil{t.Fatal(err)}
 unreviewed,err:=s.PreviewSourceCurriculum(id,10)
 if err!=nil{t.Fatal(err)}
 if unreviewed["curriculum"].(SourceCurriculum).Ready{
  t.Fatal("model-created propositions were promoted without source review")
 }
 graph,err:=s.ReasoningGraph(10)
 if err!=nil{t.Fatal(err)}
 claims:=graph["propositions"].([]Value)
 if len(claims)!=2{t.Fatalf("missing claims: %+v",graph)}
 for _,claim:=range claims {
  if _,err:=s.ReviewProposition(claim["id"].(string),"reviewed",
   "Reviewed against exact quoted source evidence");err!=nil{t.Fatal(err)}
 }
 before,err:=s.PreviewSourceCurriculum(id,10)
 if err!=nil{t.Fatal(err)}
 plan:=before["curriculum"].(SourceCurriculum)
 if !plan.Ready||len(plan.Steps)!=2||len(plan.SHA256)!=64||plan.RemainingSpans!=0{
  t.Fatalf("reviewed source curriculum not ready: %+v",plan)
 }
 if _,err:=s.ImportSourceCurriculum(id,10,plan.SHA256,false);err==nil{
  t.Fatal("unconfirmed source curriculum import accepted")
 }
 if _,err:=s.ImportSourceCurriculum(id,10,"bad-digest",true);err==nil{
  t.Fatal("invalid curriculum digest accepted")
 }
 result,err:=s.ImportSourceCurriculum(id,10,plan.SHA256,true)
 if err!=nil{t.Fatal(err)}
 if result["mastery_awarded"]!=false||result["concepts_created"]!=2{
  t.Fatalf("curriculum import altered mastery or omitted concepts: %+v",result)
 }
 for _,step:=range plan.Steps{
  concept,err:=s.Concept(step.ConceptID)
  if err!=nil{t.Fatal(err)}
  if concept["mastery"].(Value)["level"]!=0{
   t.Fatal("curriculum source was interpreted as learner mastery")
  }
  if _,err:=s.StartLearningSession("Reconstruct source concepts",step.ConceptID,"learn",nil);err!=nil{
   t.Fatal(err)
  }
 }
 repeated,err:=s.ImportSourceCurriculum(id,10,plan.SHA256,true)
 if err!=nil||repeated["concepts_created"]!=0{
  t.Fatalf("curriculum reimport was not idempotent: %+v %v",repeated,err)
 }
}

func TestSourceCurriculumRejectsTruncatedSelectionAndInvalidSource(t *testing.T){
 s:=newTestStore(t)
 if _,err:=s.PreviewSourceCurriculum("missing",10);err==nil{t.Fatal("missing source accepted")}
 if _,err:=s.PreviewSourceCurriculum("missing",0);err==nil{t.Fatal("invalid limit accepted")}
 source,err:=s.IngestText("Foundations","notes","text/plain",
  "A prime number has exactly two distinct positive divisors. "+
  "An even number is divisible by two.")
 if err!=nil{t.Fatal(err)}
 id:=source["id"].(string)
 span,err:=s.NextExtraction(id)
 if err!=nil{t.Fatal(err)}
 batch,err:=s.ProposeExtraction(span["span_id"].(string),ExtractionProposal{
  Claims:[]ExtractionCandidate{
   {Statement:"Prime integers have two positive divisors",Quote:"A prime number has exactly two distinct positive divisors",Domain:"mathematics"},
   {Statement:"Even numbers are divisible by two",Quote:"An even number is divisible by two",Domain:"mathematics"},
  },
 })
 if err!=nil{t.Fatal(err)}
 if _,err:=s.AuditExtraction(batch["batch_id"].(string),
  []ExtractionDecision{{Index:0,Action:"accept"},{Index:1,Action:"accept"}},nil);err!=nil{t.Fatal(err)}
 graph,err:=s.ReasoningGraph(10)
 if err!=nil{t.Fatal(err)}
 for _,claim:=range graph["propositions"].([]Value){
  if _,err:=s.ReviewProposition(claim["id"].(string),"reviewed","Checked against source");err!=nil{t.Fatal(err)}
 }
 preview,err:=s.PreviewSourceCurriculum(id,1)
 if err!=nil{t.Fatal(err)}
 plan:=preview["curriculum"].(SourceCurriculum)
 if plan.Ready||len(plan.Blockers)==0{
  t.Fatal("truncated review was presented as complete")
 }
 if _,err:=s.ImportSourceCurriculum(id,1,plan.SHA256,true);err==nil{
  t.Fatal("truncated source material was imported")
 }
}
