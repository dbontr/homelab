package release

import (
 "context"
 "errors"
 "strings"
 "testing"
)

type fleetFake struct {
 host string
 actions *[]string
 failActivate bool
 failRevert bool
}
func(f *fleetFake) Activate(_ context.Context,id string)error{
 *f.actions=append(*f.actions,"activate:"+f.host+":"+id)
 if f.failActivate{return errors.New("activation failed")}
 return nil
}
func(f *fleetFake) Revert(_ context.Context,id string)error{
 *f.actions=append(*f.actions,"revert:"+f.host+":"+id)
 if f.failRevert{return errors.New("rollback failed")}
 return nil
}

func TestFleetOrderedActivationAndReverseRecovery(t *testing.T){
 var actions []string
 ids:=[]string{strings.Repeat("a",48),strings.Repeat("b",48),strings.Repeat("c",48)}
 agents:=map[string]HostReleaseAgent{
  "jupiter":&fleetFake{host:"jupiter",actions:&actions},
  "venus":&fleetFake{host:"venus",actions:&actions},
  "saturn":&fleetFake{host:"saturn",actions:&actions,failActivate:true},
 }
 f:=&FleetCoordinator{Agents:agents}
 result,err:=f.Activate(context.Background(),[]FleetTarget{
  {Host:"jupiter",Role:"compute",ReleaseID:ids[0]},
  {Host:"venus",Role:"compute",ReleaseID:ids[1]},
  {Host:"saturn",Role:"control",ReleaseID:ids[2]},
 })
 if err==nil||result.Complete||!result.RolledBack||!result.AttentionRequired{t.Fatalf("incorrect failure report: %+v %v",result,err)}
 if len(actions)!=5{t.Fatalf("expected 3 activations and 2 rollbacks: %v",actions)}
 if !strings.HasPrefix(actions[3],"revert:venus:")||!strings.HasPrefix(actions[4],"revert:jupiter:"){
  t.Fatalf("rollback was not reverse ordered: %v",actions)
 }
 if result.Steps[2].State!="unknown"{t.Fatal("failed host should be marked unknown")}
}

func TestFleetRejectsDuplicateHostAndEarlyControlPlane(t *testing.T){
 id:=strings.Repeat("a",48)
 f:=&FleetCoordinator{Agents:map[string]HostReleaseAgent{
  "saturn":&fleetFake{}, "jupiter":&fleetFake{},
 }}
 for _,targets:=range [][]FleetTarget{
  {{Host:"saturn",Role:"control",ReleaseID:id},{Host:"jupiter",Role:"compute",ReleaseID:id}},
  {{Host:"jupiter",Role:"compute",ReleaseID:id},{Host:"jupiter",Role:"compute",ReleaseID:id}},
  {{Host:"jupiter",Role:"compute",ReleaseID:"main"}},
  {{Host:"k8s/../../root",Role:"compute",ReleaseID:id}},
 }{
  if _,err:=f.Activate(context.Background(),targets);err==nil{t.Fatalf("invalid plan accepted: %+v",targets)}
 }
}

func TestFleetRollbackFailureReported(t *testing.T){
 id:=strings.Repeat("a",48)
 actions:=[]string{}
 f:=&FleetCoordinator{Agents:map[string]HostReleaseAgent{
  "venus":&fleetFake{host:"venus",actions:&actions,failRevert:true},
  "jupiter":&fleetFake{host:"jupiter",actions:&actions,failActivate:true},
 }}
 report,err:=f.Activate(context.Background(),[]FleetTarget{
  {Host:"venus",Role:"compute",ReleaseID:id},
  {Host:"jupiter",Role:"compute",ReleaseID:id},
 })
 if err==nil||report.RolledBack||!report.AttentionRequired||report.Steps[0].State!="revert-failed"{
  t.Fatalf("rollback problem hidden: %+v %v",report,err)
 }
}
