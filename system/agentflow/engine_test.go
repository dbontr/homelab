package agentflow

import (
 "context"
 "encoding/json"
 "errors"
 "fmt"
 "strings"
 "testing"
 "time"
)

type fakeCompute struct{
 calls []string
 operations map[string]map[string]any
 count int
 failCheck bool
 drafts int
}
func(f *fakeCompute)Call(_ context.Context,name string,args map[string]any)(map[string]any,error){
 f.calls=append(f.calls,name)
 switch name{
 case "workspace_create":
  return map[string]any{"id":"testworkspace","status":"starting"},nil
 case "workspace_list":
  return map[string]any{"result":[]any{map[string]any{"id":"testworkspace","status":"ready"}}},nil
 case "github_checkout_repository":
  return map[string]any{"operation_id":"checkout"},nil
 case "workspace_call":
  if f.operations==nil{f.operations=map[string]map[string]any{}}
  f.count++
  id:=fmt.Sprintf("op%d",f.count)
  action:=str(args["action"])
  result:=map[string]any{}
  switch action{
  case "read_file":result["content"]="package sample"
  case "write_file":result["bytes"]=4
  case "command":
   code:=float64(0);if f.failCheck{code=1}
   result["exit_code"]=code
  case "export_file":result["artifact_id"]="artifact1"
  default:return nil,errors.New("unexpected workspace action")
  }
  f.operations[id]=result
  return map[string]any{"operation_id":id},nil
 case "workspace_operation":
  id:=str(args["operation_id"])
  if id=="checkout"{return map[string]any{"status":"succeeded","result":map[string]any{"repository":"example/repo"}},nil}
  result,ok:=f.operations[id];if !ok{return nil,errors.New("unknown operation")}
  return map[string]any{"status":"succeeded","result":result},nil
 case "github_publish_artifacts":
  f.drafts++
  if str(args["base_branch"])!="main"||len(obj(args["changes"]))==0{return nil,errors.New("invalid publication")}
  return map[string]any{"draft":true,"number":12,"url":"https://github.com/example/repo/pull/12"},nil
 default:return nil,fmt.Errorf("unexpected compute tool %s",name)
 }
}

func sampleProgram()Program{
 return Program{
  Project:"research",Repository:"example/repo",
  Revision:strings.Repeat("a",40),BaseBranch:"main",ExpectedHead:strings.Repeat("b",40),
  Objective:"Correct the documentation",Changes:map[string]string{"README.md":"Correct details"},
  Checks:[]string{"go test ./..."},ImagePoolID:"debian-bookworm-slim",
  CPU:1,MemoryMB:2048,DurationMinutes:30,Title:"Repair documentation",
 }
}
func testEngine(t *testing.T)(*Engine,*fakeCompute){
 t.Helper()
 store:=testStore(t);compute:=&fakeCompute{}
 e:=&Engine{Store:store,Compute:compute,AllowedProjects:map[string]bool{"research":true}}
 return e,compute
}
func TestEngineeringRunPreparesOnlyDraftAfterVerifiedChecks(t *testing.T){
 e,compute:=testEngine(t)
 created,err:=e.Submit(sampleProgram());if err!=nil{t.Fatal(err)}
 for i:=0;i<8;i++{
  run,err:=e.Get(created.ID);if err!=nil{t.Fatal(err)}
  if run.Status==Succeeded{break}
  progress,err:=(&Runner{Store:e.Store,Worker:e,Lease:time.Minute}).RunOne(context.Background(),created.ID)
  if err!=nil||!progress{t.Fatalf("step %d: progress=%v err=%v",i,progress,err)}
 }
 run,err:=e.Get(created.ID);if err!=nil{t.Fatal(err)}
 if run.Status!=Succeeded||compute.drafts!=1{t.Fatalf("expected verified draft: status=%s drafts=%d",run.Status,compute.drafts)}
 if run.Steps[len(run.Steps)-1].Kind!="publish"{t.Fatal("missing publication result")}
}
func TestFailedVerificationCannotPublish(t *testing.T){
 e,compute:=testEngine(t);compute.failCheck=true
 created,err:=e.Submit(sampleProgram());if err!=nil{t.Fatal(err)}
 for i:=0;i<8;i++{
  progress,err:=(&Runner{Store:e.Store,Worker:e,Lease:time.Minute}).RunOne(context.Background(),created.ID)
  if err!=nil{t.Fatal(err)}
  if !progress{break}
 }
 run,err:=e.Get(created.ID);if err!=nil{t.Fatal(err)}
 if run.Status!=Failed||compute.drafts!=0{t.Fatalf("verification bypassed: status=%s drafts=%d",run.Status,compute.drafts)}
}
type untrustedAuthor struct{}
func(untrustedAuthor)Propose(context.Context,string,map[string]string,[]string)(map[string]string,error){
 return map[string]string{"unapproved-file":"malicious"},nil
}
func TestGeneratedEditsAreRestrictedToOwnerAllowedPaths(t *testing.T){
 e,compute:=testEngine(t);e.Author=untrustedAuthor{}
 p:=sampleProgram();p.Changes=nil;p.AllowedPaths=[]string{"README.md"}
 created,err:=e.Submit(p);if err!=nil{t.Fatal(err)}
 for i:=0;i<4;i++{
  progressed,err:=(&Runner{Store:e.Store,Worker:e,Lease:time.Minute}).RunOne(context.Background(),created.ID)
  if err!=nil||!progressed{t.Fatal(err)}
 }
 run,err:=e.Get(created.ID);if err!=nil{t.Fatal(err)}
 if run.Status!=Failed||compute.drafts!=0{t.Fatalf("unauthorized edit escaped: %+v",run)}
}
func TestUnauthorizedProjectAndTraversalAreRejected(t *testing.T){
 e,_:=testEngine(t)
 p:=sampleProgram();p.Project="other"
 if _,err:=e.Submit(p);err==nil{t.Fatal("accepted unauthorized project")}
 p=sampleProgram();p.Changes=map[string]string{"../outside":"bad"}
 if _,err:=e.Submit(p);err==nil{t.Fatal("accepted traversal")}
}
func TestEngineDoesNotReturnClaimSecrets(t *testing.T){
 e,_:=testEngine(t)
 run,err:=e.Submit(sampleProgram());if err!=nil{t.Fatal(err)}
 raw,err:=json.Marshal(run);if err!=nil{t.Fatal(err)}
 if strings.Contains(string(raw),"lease_digest")||strings.Contains(string(raw),"token"){t.Fatal("leaked execution claim details")}
}
