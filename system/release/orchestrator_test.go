package release

import (
 "context"
 "os"
 "path/filepath"
 "strings"
 "testing"
)

type orchestratorFake struct{stageFake;actions []string}
func(f *orchestratorFake)Preview(_ context.Context,id,action string)(ReleasePreparation,error){
 return ReleasePreparation{Host:f.host,ReleaseID:id,Operation:"infrastructure__release_activate",Allowed:true},nil
}
func(f *orchestratorFake)Activate(_ context.Context,id string)error{f.actions=append(f.actions,"activate:"+id);return nil}
func(f *orchestratorFake)Revert(_ context.Context,id string)error{f.actions=append(f.actions,"revert:"+id);return nil}

func TestOrchestratorPersistsExactHostPlan(t *testing.T){
 dir:=t.TempDir()
 if err:=os.Chmod(dir,0700);err!=nil{t.Fatal(err)}
 state:=filepath.Join(dir,"fleet.json")
 planFile:=filepath.Join(dir,"plan.json")
 agent:=&orchestratorFake{stageFake:stageFake{host:"jupiter"}}
 agents:=map[string]HostReleaseAgent{"jupiter":agent}
 o:=&FleetOrchestrator{
  Fleet:&DurableFleet{StateFile:state,Agents:agents},
  StageTargets:[]FleetStageTarget{{Host:"jupiter",Role:"compute"}},
  StageAgents:map[string]FleetStageAgent{"jupiter":agent},
  PlanFile:planFile,
 }
 revision:=strings.Repeat("b",40)
 staged,err:=o.Stage(context.Background(),revision)
 if err!=nil||staged.ID==""||len(staged.Targets)!=1{t.Fatal(staged,err)}
 info,err:=os.Stat(planFile)
 if err!=nil||info.Mode().Perm()!=0600{t.Fatal("unsafe plan permissions",err)}
 recovered,err:=o.Plan()
 if err!=nil||recovered.ID!=staged.ID{t.Fatal(recovered,err)}
 if _,err:=o.Stage(context.Background(),revision);err==nil{t.Fatal("overwrote staged plan")}
 if _,err:=o.Activate(context.Background(),strings.Repeat("c",48));err==nil{t.Fatal("wrong plan activated")}
 tx,err:=o.Activate(context.Background(),staged.ID)
 if err!=nil||tx.Phase!="complete"||len(agent.actions)!=1{t.Fatal(tx,err)}
 if err:=o.Clear(staged.ID);err!=nil{t.Fatal(err)}
 if _,err:=o.Plan();!os.IsNotExist(err){t.Fatal("resolved plan not removed",err)}
}

func TestOrchestratorRefusesTamperedPersistedPlan(t *testing.T){
 dir:=t.TempDir()
 if err:=os.Chmod(dir,0700);err!=nil{t.Fatal(err)}
 agent:=&orchestratorFake{stageFake:stageFake{host:"jupiter"}}
 o:=&FleetOrchestrator{
  Fleet:&DurableFleet{StateFile:filepath.Join(dir,"fleet.json"),Agents:map[string]HostReleaseAgent{"jupiter":agent}},
  StageTargets:[]FleetStageTarget{{Host:"jupiter",Role:"compute"}},
  StageAgents:map[string]FleetStageAgent{"jupiter":agent},
  PlanFile:filepath.Join(dir,"plan.json"),
 }
 staged,err:=o.Stage(context.Background(),strings.Repeat("b",40))
 if err!=nil{t.Fatal(err)}
 data,err:=os.ReadFile(o.PlanFile);if err!=nil{t.Fatal(err)}
 modified:=strings.Replace(string(data),`"revision":"`+strings.Repeat("b",40)+`"`,`"revision":"`+strings.Repeat("c",40)+`"`,1)
 if err:=os.WriteFile(o.PlanFile,[]byte(modified),0600);err!=nil{t.Fatal(err)}
 if _,err:=o.Activate(context.Background(),staged.ID);err==nil{t.Fatal("tampered plan accepted")}
}
