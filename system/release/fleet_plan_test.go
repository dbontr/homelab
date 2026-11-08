package release

import (
 "context"
 "errors"
 "strings"
 "testing"
)

type stageFake struct {host string; calls int; fail bool; bad bool}
func(f *stageFake) Stage(_ context.Context,rev string)(Manifest,error){
 f.calls++
 if f.fail{return Manifest{},errors.New("host offline")}
 host:=f.host
 if f.bad{host="other"}
 return Manifest{Host:host,Revision:rev,ID:strings.Repeat("a",48),System:"/nix/store/abc-system"},nil
}
type planAgent struct{}
func(*planAgent)Activate(context.Context,string)error{return nil}
func(*planAgent)Revert(context.Context,string)error{return nil}

func TestStageFleetExactRevisionAndOrder(t *testing.T){
 rev:=strings.Repeat("b",40)
 a,b:=&stageFake{host:"jupiter"},&stageFake{host:"saturn"}
 agents:=map[string]FleetStageAgent{"jupiter":a,"saturn":b}
 plan,err:=StageFleet(context.Background(),rev,[]FleetStageTarget{{Host:"jupiter",Role:"compute"},{Host:"saturn",Role:"control"}},agents)
 if err!=nil||len(plan.Targets)!=2||a.calls!=1||b.calls!=1{t.Fatal(plan,err)}
 all:=map[string]HostReleaseAgent{"jupiter":&planAgent{},"saturn":&planAgent{}}
 if err:=plan.Validate(all);err!=nil{t.Fatal(err)}
 plan.Manifests[1].Revision=strings.Repeat("c",40)
 if err:=plan.Validate(all);err==nil{t.Fatal("changed revision accepted")}
}
func TestStageFleetFailsBeforeSideEffects(t *testing.T){
 rev:=strings.Repeat("b",40)
 a:=&stageFake{host:"jupiter"}
 agents:=map[string]FleetStageAgent{"jupiter":a,"saturn":&stageFake{host:"saturn"}}
 bad:=[][]FleetStageTarget{
  {{Host:"saturn",Role:"control"},{Host:"jupiter",Role:"compute"}},
  {{Host:"jupiter",Role:"compute"},{Host:"jupiter",Role:"compute"}},
  {{Host:"jupiter",Role:"compute"},{Host:"unconfigured",Role:"compute"}},
 }
 for _,targets:=range bad{
  if _,err:=StageFleet(context.Background(),rev,targets,agents);err==nil{t.Fatal("accepted bad targets",targets)}
 }
 if a.calls!=0{t.Fatal("staged before validating the complete plan")}
}
func TestStageFleetRejectsMismatchedManifest(t *testing.T){
 rev:=strings.Repeat("b",40)
 a:=&stageFake{host:"jupiter",bad:true}
 _,err:=StageFleet(context.Background(),rev,[]FleetStageTarget{{Host:"jupiter",Role:"compute"}},map[string]FleetStageAgent{"jupiter":a})
 if err==nil{t.Fatal("accepted wrong host generation")}
}
