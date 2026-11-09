package controller

import (
	"encoding/json"
	"testing"
	"time"
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

func TestWorkspaceBatchRESTHonorsProjectRolesAndUSBPolicy(t *testing.T) {
	db,server,httpServer:=fixture(t)
	project(t,httpServer)
	if _,err:=db.RegisterNode("worker-one",plannerNode(4,8192,nil,"debian:bookworm-slim"));err!=nil{t.Fatal(err)}
	key,issuer:=testOIDCIssuer(t,server)
	token:=signedTestToken(t,key,issuer,"project-workspace","builder",time.Now().Add(time.Hour).Unix())
	principal:="oidc:"+issuer+"#builder"
	req:=BatchAllocationRequest{
		BatchID:"batch-rest",ProjectID:"demo",
		Workloads:[]BatchWorkload{batchWorkload("worker-task",5,1)},
	}
	path:="/api/projects/demo/workspaces/batch"
	code,_:=call(t,httpServer,"POST",path,token,req)
	if code!=403{t.Fatalf("ungranted caller allocated workspaces: %d",code)}
	if _,err:=db.SetProjectGrant("demo",principal,"viewer","");err!=nil{t.Fatal(err)}
	code,_=call(t,httpServer,"POST",path,token,req)
	if code!=403{t.Fatalf("viewer allocated workspaces: %d",code)}
	if _,err:=db.SetProjectGrant("demo",principal,"editor","viewer");err!=nil{t.Fatal(err)}
	code,result:=call(t,httpServer,"POST",path,token,req)
	if code!=200{t.Fatalf("editor allocation denied: %d %v",code,result)}
	code,replayed:=call(t,httpServer,"POST",path,token,req)
	if code!=200{t.Fatalf("replay denied: %d %v",code,replayed)}
	encoded,_:=json.Marshal(result)
	encodedReplay,_:=json.Marshal(replayed)
	if string(encoded)!=string(encodedReplay){t.Fatal("REST replay created a different batch")}
	usb:=BatchAllocationRequest{
		BatchID:"batch-usb",ProjectID:"demo",
		Workloads:[]BatchWorkload{{
			ID:"usb-task",Priority:3,
			Request:WorkspaceRequest{
				USBPassthrough:true,USBNode:"worker-one",USBDeviceID:"usb-one",
				Hardware:map[string]any{"usb":1},
			},
		}},
	}
	code,_=call(t,httpServer,"POST",path,token,usb)
	if code!=403{t.Fatalf("editor used owner-only USB passthrough: %d",code)}
	if server.authorizeTool(caller{Principal:principal},"workspace_batch_create",map[string]any{
		"project_id":"demo",
		"workloads":[]any{map[string]any{"id":"usb-task","request":map[string]any{
			"usb_passthrough":true,"hardware":map[string]any{"usb":float64(1)},
		}}},
	}) {t.Fatal("MCP editor bypassed owner-only USB policy")}
}
