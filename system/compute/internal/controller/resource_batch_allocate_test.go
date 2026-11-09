package controller

import (
	"encoding/json"
	"testing"
)

func batchWorkload(id string, priority int, cpu float64) BatchWorkload {
	return BatchWorkload{ID:id,Priority:priority,Request:WorkspaceRequest{
		Image:"debian:bookworm-slim",CPU:cpu,MemoryMB:1024,
	}}
}

func TestWorkspaceBatchCommitsExactlyOnceAndSurvivesRestart(t *testing.T) {
	db,_,server:=fixture(t)
	project(t,server)
	if _,err:=db.RegisterNode("worker-one",plannerNode(4,8192,nil,"debian:bookworm-slim"));err!=nil{t.Fatal(err)}
	req:=BatchAllocationRequest{BatchID:"batch-alpha",ProjectID:"demo",Workloads:[]BatchWorkload{
		batchWorkload("primary",5,1),batchWorkload("secondary",3,1),
	}}
	first,err:=db.AllocateWorkspaceBatch(req)
	if err!=nil{t.Fatal(err)}
	if len(first.Workspaces)!=2 || first.Workspaces[0].WorkloadID!="primary" {
		t.Fatalf("unexpected batch allocation: %+v",first)
	}
	second,err:=db.AllocateWorkspaceBatch(req)
	if err!=nil{t.Fatal(err)}
	encodedA,_:=json.Marshal(first)
	encodedB,_:=json.Marshal(second)
	if string(encodedA)!=string(encodedB){t.Fatal("replay allocated a different batch")}
	all,err:=db.Workspaces("demo")
	if err!=nil || len(all)!=2{t.Fatalf("idempotency created duplicate workspaces: %v %v",all,err)}
	req.Workloads[0].Request.CPU=2
	if _,err:=db.AllocateWorkspaceBatch(req);err==nil {
		t.Fatal("mismatched retry reused existing batch ID")
	}
}

func TestWorkspaceBatchHasNoPartialCommitOnInsufficientCapacity(t *testing.T) {
	db,_,server:=fixture(t)
	project(t,server)
	if _,err:=db.RegisterNode("worker-one",plannerNode(2,2048,nil,"debian:bookworm-slim"));err!=nil{t.Fatal(err)}
	req:=BatchAllocationRequest{BatchID:"batch-overload",ProjectID:"demo",Workloads:[]BatchWorkload{
		batchWorkload("primary",5,1.5),batchWorkload("secondary",4,1.5),
	}}
	if _,err:=db.AllocateWorkspaceBatch(req);err==nil {
		t.Fatal("overcommitted batch succeeded")
	}
	rows,err:=db.Workspaces("demo")
	if err!=nil || len(rows)!=0{t.Fatalf("partial workspace allocation persisted: %v %v",rows,err)}
}

func TestWorkspaceBatchRejectsCrossProjectHardwareAndDuplicateIDs(t *testing.T) {
	db,_,server:=fixture(t)
	project(t,server)
	if _,err:=db.RegisterNode("worker-one",plannerNode(4,8192,nil,"debian:bookworm-slim"));err!=nil{t.Fatal(err)}
	req:=BatchAllocationRequest{BatchID:"batch-secure",ProjectID:"demo",Workloads:[]BatchWorkload{
		batchWorkload("one",5,1),batchWorkload("one",4,1),
	}}
	if _,err:=db.AllocateWorkspaceBatch(req);err==nil{t.Fatal("duplicate workload ID accepted")}
	req.Workloads[1].ID="two"
	req.Workloads[1].Request.ProjectID="another-project"
	if _,err:=db.AllocateWorkspaceBatch(req);err==nil{t.Fatal("cross-project request accepted")}
	req.Workloads[1].Request.ProjectID=""
	req.Workloads[0].Request.Hardware=map[string]any{"gpu":1}
	if _,err:=db.AllocateWorkspaceBatch(req);err==nil{t.Fatal("nonexistent accelerator reserved")}
	rows,err:=db.Workspaces("demo")
	if err!=nil || len(rows)!=0{t.Fatalf("failed batch affected workspace records: %v %v",rows,err)}
}
