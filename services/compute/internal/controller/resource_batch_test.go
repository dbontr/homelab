package controller

import "testing"

func TestBatchResourcePlannerHonorsCapacityAndPriorities(t *testing.T){
 db,_,httpServer:=fixture(t)
 project(t,httpServer)
 if _,err:=db.RegisterNode("node-one",plannerNode(4,4096,nil,"debian:bookworm-slim"));err!=nil{t.Fatal(err)}
 if _,err:=db.RegisterNode("node-two",plannerNode(3,4096,nil,"debian:bookworm-slim"));err!=nil{t.Fatal(err)}
 batch:=BatchPlanRequest{ProjectID:"demo",Workloads:[]BatchWorkload{
  {ID:"first",Priority:5,Request:WorkspaceRequest{CPU:3,MemoryMB:2048}},
  {ID:"second",Priority:3,Request:WorkspaceRequest{CPU:2,MemoryMB:2048}},
  {ID:"third",Priority:1,Request:WorkspaceRequest{CPU:3,MemoryMB:2048}},
 }}
 plan,err:=db.PlanWorkspaceBatch(batch)
 if err!=nil{t.Fatal(err)}
 if asInt(plan["placed"])!=2||asInt(plan["unplaced"])!=1{t.Fatalf("overcommitted placement: %v",plan)}
 result:=plan["placements"].([]BatchPlacement)
 if result[0].ID!="first"||result[0].Status!="candidate"{t.Fatalf("priority not honored: %v",result)}
 existing,err:=db.Workspaces("demo")
 if err!=nil||len(existing)!=0{t.Fatalf("advisory plan allocated actual resources: %v %v",existing,err)}
 batch.Workloads[0].Request.ProjectID="unauthorized"
 if _,err:=db.PlanWorkspaceBatch(batch);err==nil{t.Fatal("cross-project placement accepted")}
}

func TestBatchPlanDoesNotDoublePromiseAccelerators(t *testing.T){
 db,_,server:=fixture(t)
 project(t,server)
 node:=plannerNode(12,32768,[]any{
  map[string]any{"id":"gpu-zero","kind":"gpu","memory_mb":16384},
  map[string]any{"id":"gpu-one","kind":"gpu","memory_mb":8192},
 },"debian:bookworm-slim")
 if _,err:=db.RegisterNode("gpu-node",node);err!=nil{t.Fatal(err)}
 plan,err:=db.PlanWorkspaceBatch(BatchPlanRequest{ProjectID:"demo",Workloads:[]BatchWorkload{
  {ID:"advanced",Priority:5,Request:WorkspaceRequest{Hardware:map[string]any{"gpu":1},MinGPUMemoryMB:12000}},
  {ID:"secondary",Priority:4,Request:WorkspaceRequest{Hardware:map[string]any{"gpu":1},MinGPUMemoryMB:8000}},
 }})
 if err!=nil{t.Fatal(err)}
 if asInt(plan["placed"])!=1||asInt(plan["unplaced"])!=1{
  t.Fatalf("uncertain accelerator identities were double promised: %v",plan)
 }
}
