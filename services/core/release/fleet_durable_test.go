package release

import (
 "context"
 "encoding/json"
 "errors"
 "os"
 "path/filepath"
 "strings"
 "testing"
)

type durableFake struct {
 actions *[]string
 name string
 file string
 failActivate bool
 failRevert bool
 observation ReleaseObservation
 observeErr error
}

func(f *durableFake) Activate(_ context.Context,id string)error{
 data,err:=os.ReadFile(f.file);if err!=nil{return err}
 var tx FleetTransaction
 if err:=json.Unmarshal(data,&tx);err!=nil{return err}
 if tx.Steps[0].State=="pending"&&f.name=="jupiter"{return errors.New("operation was not journaled before execution")}
 *f.actions=append(*f.actions,"activate:"+f.name)
 if f.failActivate{return errors.New("disconnected during activation")}
 return nil
}
func(f *durableFake) Revert(_ context.Context,id string)error{
 *f.actions=append(*f.actions,"revert:"+f.name)
 if f.failRevert{return errors.New("disconnected during revert")}
 return nil
}
func(f *durableFake) Observe(context.Context)(ReleaseObservation,error){return f.observation,f.observeErr}
func(f *durableFake) ActivateWithID(ctx context.Context,requestID,releaseID string)error{
 if !validRemoteExecutionID(requestID){return errors.New("invalid activation execution ID")}
 return f.Activate(ctx,releaseID)
}
func(f *durableFake) RevertWithID(ctx context.Context,requestID,releaseID string)error{
 if !validRemoteExecutionID(requestID){return errors.New("invalid rollback execution ID")}
 return f.Revert(ctx,releaseID)
}



func TestDurableFleetPersistsIntentAndRequiresResolution(t *testing.T){
 file:=filepath.Join(t.TempDir(),"private","journal.json")
 id:=strings.Repeat("a",48)
 actions:=[]string{}
 agent:=&durableFake{name:"jupiter",actions:&actions,file:file}
 d:=&DurableFleet{StateFile:file,Agents:map[string]HostReleaseAgent{"jupiter":agent}}
 tx,err:=d.Start(context.Background(),[]FleetTarget{{Host:"jupiter",Role:"compute",ReleaseID:id}})
 if err!=nil||tx.Phase!="complete"||tx.Steps[0].State!="active"||len(actions)!=1{t.Fatalf("activation: %+v %v",tx,err)}
 restored,err:=(&DurableFleet{StateFile:file,Agents:d.Agents}).Status()
 if err!=nil||restored.ID!=tx.ID||restored.Phase!="complete"{t.Fatal(restored,err)}
 if _,err:=d.Start(context.Background(),tx.Targets);err==nil{t.Fatal("replayed transaction")}
 if err:=d.Clear(strings.Repeat("b",48));err==nil{t.Fatal("cleared wrong transaction")}
 if err:=d.Clear(tx.ID);err!=nil{t.Fatal(err)}
 if _,err:=d.Start(context.Background(),tx.Targets);err!=nil{t.Fatal("new transaction after explicit clearance:",err)}
}

func TestDurableFleetReverseRollbackAndReconciliation(t *testing.T){
 dir:=t.TempDir();if err:=os.Chmod(dir,0700);err!=nil{t.Fatal(err)}
 file:=filepath.Join(dir,"journal.json")
 actions:=[]string{}
 id:=strings.Repeat("a",48)
 first:=&durableFake{name:"jupiter",actions:&actions,file:file}
 second:=&durableFake{name:"venus",actions:&actions,file:file,failActivate:true}
 d:=&DurableFleet{StateFile:file,Agents:map[string]HostReleaseAgent{"jupiter":first,"venus":second}}
 tx,err:=d.Start(context.Background(),[]FleetTarget{{Host:"jupiter",Role:"compute",ReleaseID:id},{Host:"venus",Role:"compute",ReleaseID:id}})
 if err==nil||tx.Phase!="attention"||tx.Steps[0].State!="reverted"||tx.Steps[1].State!="unknown"{t.Fatalf("unsafe outcome: %+v %v",tx,err)}
 expected:=[]string{"activate:jupiter","activate:venus","revert:jupiter"}
 if strings.Join(actions,",")!=strings.Join(expected,","){t.Fatal(actions)}
 if err:=d.Clear(tx.ID);err==nil{t.Fatal("cleared uncertain transaction")}
 second.observation=ReleaseObservation{ID:id,Phase:"rolled-back"}
 recovered,err:=(&DurableFleet{StateFile:file,Agents:d.Agents}).Recover(context.Background(),tx.ID)
 if err!=nil||recovered.Phase!="reverted"||recovered.Steps[1].State!="reverted"{t.Fatalf("recovery: %+v %v",recovered,err)}
}

func TestDurableFleetDoesNotReplayAmbiguousActivation(t *testing.T){
 dir:=t.TempDir();if err:=os.Chmod(dir,0700);err!=nil{t.Fatal(err)}
 file:=filepath.Join(dir,"journal.json")
 id:=strings.Repeat("c",48)
 actions:=[]string{}
 agent:=&durableFake{name:"saturn",file:file,actions:&actions}
 d:=&DurableFleet{StateFile:file,Agents:map[string]HostReleaseAgent{"saturn":agent}}
 tx:=FleetTransaction{ID:strings.Repeat("e",48),Phase:"activating",Targets:[]FleetTarget{{Host:"saturn",Role:"control",ReleaseID:id}},Steps:[]FleetStep{{Host:"saturn",Role:"control",ReleaseID:id,State:"activating"}}}
 if err:=d.locked(func()error{return d.write(&tx)});err!=nil{t.Fatal(err)}
 resolved,err:=d.Recover(context.Background(),tx.ID)
 if err!=nil||resolved.Phase!="attention"||len(actions)!=0{t.Fatal("unobserved action was replayed",resolved,err,actions)}
 agent.observation=ReleaseObservation{ID:id,Phase:"active"}
 resolved,err=d.Recover(context.Background(),tx.ID)
 if err!=nil||resolved.Phase!="reverted"||len(actions)!=1||actions[0]!="revert:saturn"{t.Fatal("observed active release not reverted",resolved,err,actions)}
}

func TestDurableFleetRejectsUnsafeJournalPermissions(t *testing.T){
 dir:=t.TempDir()
 file:=filepath.Join(dir,"journal.json")
 id:=strings.Repeat("a",48)
 actions:=[]string{}
 agent:=&durableFake{name:"jupiter",file:file,actions:&actions}
 d:=&DurableFleet{StateFile:file,Agents:map[string]HostReleaseAgent{"jupiter":agent}}
 if err:=os.Chmod(dir,0755);err!=nil{t.Fatal(err)}
 if _,err:=d.Start(context.Background(),[]FleetTarget{{Host:"jupiter",Role:"compute",ReleaseID:id}});err==nil{t.Fatal("accepted world-readable fleet state directory")}
}
