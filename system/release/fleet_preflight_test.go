package release

import (
 "context"
 "encoding/json"
 "os"
 "path/filepath"
 "strings"
 "testing"
)

func TestFleetActivationDoesNotStartWithoutHostConfirmations(t *testing.T){
 s,agent:=testFleetHTTP(t)
 auth:=s.Authorizer.(*fleetHTTPAuthorizer)
 auth.allow=true
 revision:=strings.Repeat("b",40)
 stage:=fleetHTTPRequest(s,"POST","/v1/stage",`{"revision":"`+revision+`"}`,"signed-owner")
 if stage.Code!=200{t.Fatal(stage.Body.String())}
 var out struct{Result FleetStagePlan}
 if err:=json.Unmarshal(stage.Body.Bytes(),&out);err!=nil{t.Fatal(err)}
 agent.needConfirm=true
 args:=`{"plan_id":"`+out.Result.ID+`"}`
 prepare:=fleetHTTPRequest(s,"POST","/v1/prepare",args,"signed-owner")
 if prepare.Code!=200||!strings.Contains(prepare.Body.String(),"confirmation-1"){
  t.Fatal("missing host confirmation absent from preview",prepare.Code,prepare.Body.String())
 }
 denied:=fleetHTTPRequest(s,"POST","/v1/activate",args,"signed-owner")
 if denied.Code!=200||!strings.Contains(denied.Body.String(),`"allowed":false`)||len(agent.actions)!=0{
  t.Fatal("fleet started without host confirmations",denied.Code,denied.Body.String(),agent.actions)
 }
 if _,err:=os.Stat(s.Controller.Fleet.StateFile);!os.IsNotExist(err){
  t.Fatal("unapproved rollout persisted a transaction journal",err)
 }
 for _,operation:=range auth.operations{
  if operation=="infrastructure__fleet_activate"{t.Fatal("fleet approval was consumed before host preflight")}
 }
 agent.needConfirm=false
 successful:=fleetHTTPRequest(s,"POST","/v1/activate",args,"signed-owner")
 if successful.Code!=200||len(agent.actions)!=1{
  t.Fatal("confirmed fleet rollout did not execute",successful.Code,successful.Body.String())
 }
}

func TestFleetPreflightUsesStoredPlanIdentity(t *testing.T){
 dir:=t.TempDir()
 if err:=os.Chmod(dir,0700);err!=nil{t.Fatal(err)}
 agent:=&orchestratorFake{stageFake:stageFake{host:"jupiter"}}
 o:=&FleetOrchestrator{
  Fleet:&DurableFleet{StateFile:filepath.Join(dir,"fleet.json"),Agents:map[string]HostReleaseAgent{"jupiter":agent}},
  StageTargets:[]FleetStageTarget{{Host:"jupiter",Role:"compute"}},
  StageAgents:map[string]FleetStageAgent{"jupiter":agent},
  PlanFile:filepath.Join(dir,"plan.json"),
 }
 staged,err:=o.Stage(context.Background(),strings.Repeat("a",40))
 if err!=nil{t.Fatal(err)}
 if _,err:=o.Preflight(context.Background(),strings.Repeat("f",48));err==nil{
  t.Fatal("foreign plan preflight accepted")
 }
 items,err:=o.Preflight(context.Background(),staged.ID)
 if err!=nil||len(items)!=1||!items[0].Allowed||items[0].ReleaseID!=staged.Targets[0].ReleaseID{
  t.Fatal("wrong stored scope",items,err)
 }
}
