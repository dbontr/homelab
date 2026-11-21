package motivation

import (
 "errors"
 "path/filepath"
 "testing"
)

func TestCuriosityQuestionsKeepSourceAndSurviveRestart(t *testing.T){
 path:=filepath.Join(t.TempDir(),"motivation.json")
 e,err:=Open(path);if err!=nil{t.Fatal(err)}
 input:=CuriosityChange{
  Enabled:true,SourceKind:"task",SourceID:"study_42",SourceLabel:"Prove a bound",
  Question:"Does this proposed inequality hold for the boundary case?",
  WorthwhileOutcome:"A correct proof or counterexample",
  FirstAction:"Evaluate the boundary case symbolically",
  ExpectedEvidence:"Written computation establishing whether the bound fails",
 }
 first,err:=e.SaveCuriosity("boundary",input);if err!=nil{t.Fatal(err)}
 if len(first.CuriosityPrompts)!=1||first.CuriosityPrompts[0].Revision!=1||
  first.CuriosityPrompts[0].Provenance!="owner_declared"{t.Fatalf("incorrect provenance: %+v",first.CuriosityPrompts)}
 if _,err=e.Curiosity("boundary");err==nil{t.Fatal("disabled global motivation permitted curiosity")}
 if _,err=e.SaveCuriosity("boundary",input);err==nil{t.Fatal("stale revision accepted")}
 if err=e.Close();err!=nil{t.Fatal(err)}
 reopened,err:=Open(path);if err!=nil{t.Fatal(err)}
 defer reopened.Close()
 saved:=reopened.Snapshot().CuriosityPrompts[0]
 if saved.SourceID!=input.SourceID||saved.ExpectedEvidence!=input.ExpectedEvidence{
  t.Fatal("provenance or evidence requirement lost on restart")
 }
 input.ExpectedRevision=1;input.Paused=true
 next,err:=reopened.SaveCuriosity("boundary",input);if err!=nil{t.Fatal(err)}
 if next.CuriosityPrompts[0].Revision!=2||!next.CuriosityPrompts[0].Paused{
  t.Fatal("curiosity pause did not persist")
 }
 input.ExpectedRevision=2;input.Paused=false;input.Enabled=false
 if _,err=reopened.SaveCuriosity("boundary",input);err!=nil{t.Fatal(err)}
}

func TestCuriosityInputValidationAndNoFabricatedResult(t *testing.T){
 e,err:=Open(filepath.Join(t.TempDir(),"motivation.json"));if err!=nil{t.Fatal(err)}
 defer e.Close()
 good:=CuriosityChange{
  Enabled:true,SourceKind:"research",SourceID:"source_1",
  Question:"What evidence tests the hypothesis?",
  WorthwhileOutcome:"Evaluate the premise",
  FirstAction:"Write the simplest falsifying case",
  ExpectedEvidence:"A reproducible computation",
 }
 for _,bad:=range []CuriosityChange{
  {Enabled:true,SourceKind:"task",SourceID:"task_1",Question:"Question"},
  {Enabled:true,SourceKind:"research",SourceID:"../private",Question:good.Question,
   WorthwhileOutcome:good.WorthwhileOutcome,FirstAction:good.FirstAction,ExpectedEvidence:good.ExpectedEvidence},
  {Enabled:true,SourceKind:"research",SourceID:"source_1",Question:"Fabricated\x00 evidence",
   WorthwhileOutcome:good.WorthwhileOutcome,FirstAction:good.FirstAction,ExpectedEvidence:good.ExpectedEvidence},
 }{
  _,err:=e.SaveCuriosity("bad",bad)
  var invalid InvalidError
  if !errors.As(err,&invalid){t.Fatalf("invalid curiosity accepted: %+v, %v",bad,err)}
 }
 state,err:=e.SaveCuriosity("real",good);if err!=nil{t.Fatal(err)}
 if len(state.Interventions)!=0||len(state.Checkins)!=0{
  t.Fatal("saving a question fabricated an intervention or completed output")
 }
}
