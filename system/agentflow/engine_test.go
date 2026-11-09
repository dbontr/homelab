package agentflow

import (
 "context"
 "crypto/sha256"
 "encoding/hex"
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
 files map[string]string
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
  case "read_file":
   filename:=str(obj(args["payload"])["path"])
   if body,ok:=f.files[filename];ok{result["content"]=body}else{result["content"]="package sample"}
  case "write_file":
   payload:=obj(args["payload"])
   if f.files==nil{f.files=map[string]string{}}
   filename,body:=str(payload["path"]),str(payload["content"])
   f.files[filename]=body
   digest:=sha256.Sum256([]byte(body))
   result["sha256"]=hex.EncodeToString(digest[:])
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
  if id=="checkout"{return map[string]any{"status":"succeeded","result":map[string]any{"repository":"example/repo","revision":strings.Repeat("a",40)}},nil}
  result,ok:=f.operations[id];if !ok{return nil,errors.New("unknown operation")}
  return map[string]any{"status":"succeeded","result":result},nil
 case "github_publish_artifacts":
  f.drafts++
  changes,ok:=args["changes"].(map[string]string)
  if str(args["base_branch"])!="main"||!ok||len(changes)==0{return nil,errors.New("invalid publication")}
  return map[string]any{"draft":true,"number":12,"base_sha":strings.Repeat("a",40),"url":"https://github.com/example/repo/pull/12"},nil
 default:return nil,fmt.Errorf("unexpected compute tool %s",name)
 }
}

func sampleProgram()Program{
 return Program{
  Project:"research",Repository:"example/repo",
  Revision:strings.Repeat("a",40),BaseBranch:"main",ExpectedHead:strings.Repeat("a",40),
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

func TestCancelSignalsActiveEngineOperation(t *testing.T){
 e,_:=testEngine(t)
 run,err:=e.Submit(sampleProgram());if err!=nil{t.Fatal(err)}
 ctx,cancel:=context.WithCancel(context.Background())
 defer cancel()
 e.activeMu.Lock()
 e.active=map[string]context.CancelFunc{run.ID:cancel}
 e.activeMu.Unlock()
 if err:=e.Cancel(run.ID,run.Revision);err!=nil{t.Fatal(err)}
 select{
 case <-ctx.Done():
 default:t.Fatal("active execution was not interrupted")
 }
 current,err:=e.Get(run.ID);if err!=nil{t.Fatal(err)}
 if !current.Cancelled||current.Status!=Cancelled{t.Fatalf("unexpected cancellation status: %+v",current)}
}

type repairAuthor struct{
 compute *fakeCompute
 calls int
}
func (a *repairAuthor)Propose(_ context.Context,_ string,_ map[string]string,allowed []string)(map[string]string,error){
 a.calls++
 if len(allowed)!=1||allowed[0]!="README.md"{return nil,errors.New("unexpected editable scope")}
 a.compute.failCheck=false
 return map[string]string{"README.md":"Corrected after verification"},nil
}
func TestAuthorizedRepairRerunsVerificationBeforePublication(t *testing.T){
 e,compute:=testEngine(t)
 compute.failCheck=true
 author:=&repairAuthor{compute:compute}
 e.Author=author
 p:=sampleProgram()
 p.AllowedPaths=[]string{"README.md"}
 run,err:=e.Submit(p);if err!=nil{t.Fatal(err)}
 for i:=0;i<8;i++{
  progressed,err:=(&Runner{Store:e.Store,Worker:e,Lease:time.Minute}).RunOne(context.Background(),run.ID)
  if err!=nil||!progressed{t.Fatalf("step %d: %v",i,err)}
 }
 final,err:=e.Get(run.ID);if err!=nil{t.Fatal(err)}
 if final.Status!=Succeeded||compute.drafts!=1||author.calls!=1{
  t.Fatalf("failed to verify bounded repair: status=%s drafts=%d calls=%d",final.Status,compute.drafts,author.calls)
 }
}
func TestUnauthorizedRepairCannotReachDraftPublication(t *testing.T){
 e,compute:=testEngine(t)
 compute.failCheck=true
 e.Author=untrustedAuthor{}
 p:=sampleProgram()
 p.AllowedPaths=[]string{"README.md"}
 run,err:=e.Submit(p);if err!=nil{t.Fatal(err)}
 for i:=0;i<8;i++{
  progressed,err:=(&Runner{Store:e.Store,Worker:e,Lease:time.Minute}).RunOne(context.Background(),run.ID)
  if err!=nil{t.Fatal(err)}
  if !progressed{break}
 }
 final,err:=e.Get(run.ID);if err!=nil{t.Fatal(err)}
 if final.Status!=Failed||compute.drafts!=0{
  t.Fatalf("unauthorized repair reached publication: status=%s drafts=%d",final.Status,compute.drafts)
 }
}
