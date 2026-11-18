package agentflow

import (
 "context"
 "encoding/json"
 "strings"
 "testing"
)

type mediaComputeMock struct{
 sha string
 id string
 mode string
 mismatch bool
}
func(m *mediaComputeMock)Call(_ context.Context,tool string,args map[string]any)(map[string]any,error){
 switch tool {
 case "artifact_info":
  if args["project_id"]!="demo"||args["artifact_id"]!=m.id {return nil,context.Canceled}
  digest:=m.sha
  if m.mismatch{digest=strings.Repeat("0",64)}
  return map[string]any{"id":m.id,"sha256":digest,"name":"photo.CR3","size":float64(1024)},nil
 case "workspace_import_artifact":
  return map[string]any{"operation_id":strings.Repeat("b",32)},nil
 case "workspace_call":
  if args["action"]=="command"{return map[string]any{"operation_id":strings.Repeat("c",32)},nil}
  if args["action"]=="export_file"{return map[string]any{"operation_id":strings.Repeat("d",32)},nil}
 case "workspace_operation":
  switch args["operation_id"] {
  case strings.Repeat("b",32):
   return map[string]any{"status":"succeeded","result":map[string]any{"size":float64(1024)}},nil
  case strings.Repeat("c",32):
   return map[string]any{"status":"succeeded","result":map[string]any{"stdout":m.sha+"  photo.CR3\n","exit_code":float64(0)}},nil
  case strings.Repeat("d",32):
   return map[string]any{"status":"succeeded","result":map[string]any{"artifact_id":m.id}},nil
  }
 }
 return nil,context.Canceled
}
func mediaWorkflow() Workflow {
 return Workflow{Project:"demo",Steps:[]Step{
  {ID:"provision",Status:Succeeded,Result:json.RawMessage(`{"id":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}`)},
  {ID:"checkout",Status:Succeeded,Result:json.RawMessage(`{"repository":"acme/research","revision":"bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"}`)},
 }}
}
func TestProjectArtifactImportAndDeliverableExport(t *testing.T){
 fake:=&mediaComputeMock{id:strings.Repeat("a",32),sha:strings.Repeat("b",64)}
 engine:=&Engine{Compute:fake}
 w:=mediaWorkflow()
 imports,err:=engine.importInputs(context.Background(),w,map[string]any{"artifacts":[]any{
  map[string]any{"artifact_id":fake.id,"path":"photos/photo.CR3"},
 }})
 if err!=nil || obj(imports["inputs"])==nil {t.Fatalf("import: %v %v",imports,err)}
 outputs,err:=engine.deliverOutputs(context.Background(),w,map[string]any{"paths":[]any{"photos/photo.CR3"}})
 if err!=nil || obj(outputs["outputs"])==nil{t.Fatalf("export: %v %v",outputs,err)}
 fake.mismatch=true
 if _,err:=engine.importInputs(context.Background(),w,map[string]any{"artifacts":[]any{
  map[string]any{"artifact_id":fake.id,"path":"photos/photo.CR3"},
 }});err==nil{t.Fatal("import with conflicting stored digest accepted")}
 if _,err:=engine.deliverOutputs(context.Background(),w,map[string]any{"paths":[]any{"photos/photo.CR3"}});err==nil{
  t.Fatal("export with conflicting stored digest accepted")
 }
}

func TestImportedPhotoSourceIsBoundToWorkflow(t *testing.T){
 p:=Program{
  Project:"demo",Repository:"acme/research",Revision:strings.Repeat("b",40),
  ExpectedHead:strings.Repeat("b",40),BaseBranch:"main",Objective:"Edit photo",
  AllowedPaths:[]string{"scripts/grade.sh"},Checks:[]string{"./scripts/grade.sh"},
  Autonomous:true,VisualFeedback:true,VisualPaths:[]string{"photos/review.png"},
  ImagePoolID:"visual",CPU:1,MemoryMB:2048,DurationMinutes:40,Title:"RAW photo",
  InputArtifacts:[]ArtifactInput{{ArtifactID:strings.Repeat("a",32),Path:"photos/photo.CR3"}},
  OutputPaths:[]string{"photos/photo.CR3","photos/photo.CR3.xmp","photos/final.jpg"},
 }
 compiled,err:=Compile(p)
 if err!=nil{t.Fatal(err)}
 inputIndex,agentIndex:=-1,-1
 for i,st:=range compiled.Steps{
  if st.Kind=="inputs"{inputIndex=i}
  if st.Kind=="agent"{agentIndex=i}
 }
 if inputIndex<0||agentIndex<0{t.Fatal("missing import or agent")}
 if len(compiled.Steps[agentIndex].DependsOn)!=1||compiled.Steps[agentIndex].DependsOn[0]!="inputs"{
  t.Fatalf("agent can start before image import: %+v",compiled.Steps[agentIndex].DependsOn)
 }
 if len(compiled.Inputs.InputArtifacts)!=1{t.Fatal("input artifact provenance missing")}
 bad:=p
 bad.InputArtifacts=[]ArtifactInput{{ArtifactID:strings.Repeat("a",32),Path:".env"}}
 if err:=bad.validate();err==nil{t.Fatal("credential import path accepted")}
}

func TestImportedOriginalRemainsImmutableAfterVerification(t *testing.T){
 initial:=strings.Repeat("b",64)
 fake:=&mediaComputeMock{id:strings.Repeat("a",32),sha:initial}
 engine:=&Engine{Compute:fake}
 w:=mediaWorkflow()
 receipt,_:=json.Marshal(map[string]any{"inputs":map[string]any{
  "photos/photo.CR3":map[string]any{"artifact_id":fake.id,"sha256":initial,"size":1024},
 }})
 w.Steps=append(w.Steps,Step{ID:"inputs",Kind:"inputs",Status:Succeeded,Result:receipt})
 if err:=engine.verifyImmutableInputs(context.Background(),w);err!=nil{t.Fatal(err)}
 fake.sha=strings.Repeat("c",64)
 if err:=engine.verifyImmutableInputs(context.Background(),w);err==nil{
  t.Fatal("verification accepted a modified RAW original")
 }
}
