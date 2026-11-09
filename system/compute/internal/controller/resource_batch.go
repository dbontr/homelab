package controller

import (
 "errors"
 "math"
 "sort"
)

type BatchPlanRequest struct {
 ProjectID string `json:"project_id"`
 Workloads []BatchWorkload `json:"workloads"`
}
type BatchWorkload struct {
 ID string `json:"id"`
 Priority int `json:"priority"`
 Request WorkspaceRequest `json:"request"`
}
type BatchPlacement struct {
 ID string `json:"id"`
 Priority int `json:"priority"`
 Node string `json:"node,omitempty"`
 Status string `json:"status"`
 Reason string `json:"reason,omitempty"`
}
type batchBudget struct {
 CPU float64
 Memory float64
 Devices map[string]int
}

// PlanWorkspaceBatch accounts for previously proposed work in this batch. It
// reuses single-workspace admission and never creates workspaces or device leases.
func (db *Database) PlanWorkspaceBatch(batch BatchPlanRequest)(map[string]any,error){
 if !validID.MatchString(batch.ProjectID)||len(batch.Workloads)<1||len(batch.Workloads)>12{
  return nil,errors.New("invalid bounded batch plan")
 }
 type item struct{
  ID string
  Priority int
  CPU float64
  Memory int
  Hardware map[string]int
  USB string
  Options []map[string]any
  Demand int
 }
 items:=make([]item,0,len(batch.Workloads))
 seen:=map[string]bool{}
 for _,job:=range batch.Workloads {
  if !validID.MatchString(job.ID)||seen[job.ID]||job.Priority<0||job.Priority>5{
   return nil,errors.New("invalid workload identity or priority")
  }
  seen[job.ID]=true
  if job.Request.ProjectID!=""&&job.Request.ProjectID!=batch.ProjectID{
   return nil,errors.New("cross-project workload is forbidden")
  }
  job.Request.ProjectID=batch.ProjectID
  job.Request.defaults()
  hardware,err:=job.Request.validatedHardware()
  if err!=nil{return nil,err}
  plan,err:=db.PlanWorkspace(job.Request)
  if err!=nil{return nil,err}
  candidates,ok:=plan["candidates"].([]map[string]any)
  if !ok{return nil,errors.New("invalid resource candidate list")}
  demand:=0
  for _,quantity:=range hardware{demand+=quantity}
  usb:=""
  if job.Request.USBPassthrough{usb=job.Request.USBNode+":"+job.Request.USBDeviceID}
  items=append(items,item{ID:job.ID,Priority:job.Priority,CPU:job.Request.CPU,
   Memory:job.Request.MemoryMB,Hardware:hardware,USB:usb,Options:candidates,Demand:demand})
 }
 sort.Slice(items,func(i,j int)bool{
  a,b:=items[i],items[j]
  if a.Priority!=b.Priority{return a.Priority>b.Priority}
  if a.Demand!=b.Demand{return a.Demand>b.Demand}
  if a.Memory!=b.Memory{return a.Memory>b.Memory}
  if a.CPU!=b.CPU{return a.CPU>b.CPU}
  return a.ID<b.ID
 })
 placements:=make([]BatchPlacement,0,len(items))
 used:=map[string]*batchBudget{}
 busyUSB:=map[string]bool{}
 placed:=0
 for _,job:=range items{
  selected:=""
  for _,candidate:=range job.Options{
   if candidate["eligible"]!=true{continue}
   node:=asString(candidate["node"])
   if node==""{continue}
   freeCPU:=asFloat(candidate["cpu_unreserved"])
   freeMemory:=asFloat(candidate["memory_mb_unreserved"])
   if math.IsNaN(freeCPU)||math.IsNaN(freeMemory)||freeCPU<0||freeMemory<0{continue}
   reservation:=used[node]
   if reservation==nil{reservation=&batchBudget{Devices:map[string]int{}}}
   if reservation.CPU+job.CPU>freeCPU+1e-7 ||
      reservation.Memory+float64(job.Memory)>freeMemory{continue}
   if job.USB!=""&&busyUSB[job.USB]{continue}
   free,ok:=candidate["hardware_unreserved"].(map[string]int)
   if !ok{continue}
   available:=true
   for kind,required:=range job.Hardware{
    if required<=0{continue}
    // GPU VRAM and exact device identities are not reserved in a plan.
    // Reserve each accelerator kind for one workload per node, conservatively.
    if reservation.Devices[kind]>0||free[kind]<required{available=false;break}
   }
   if !available{continue}
   selected=node
   reservation.CPU+=job.CPU
   reservation.Memory+=float64(job.Memory)
   for kind,required:=range job.Hardware{if required>0{reservation.Devices[kind]+=required}}
   used[node]=reservation
   if job.USB!=""{busyUSB[job.USB]=true}
   break
  }
  if selected==""{
   placements=append(placements,BatchPlacement{ID:job.ID,Priority:job.Priority,
    Status:"unplaced",Reason:"no eligible worker has sufficient unreserved batch capacity and permitted hardware"})
  }else{
   placed++
   placements=append(placements,BatchPlacement{ID:job.ID,Priority:job.Priority,Node:selected,Status:"candidate"})
  }
 }
 return map[string]any{"project_id":batch.ProjectID,"placed":placed,
  "unplaced":len(placements)-placed,"placements":placements,
  "advisory":"Not a reservation. Worker identity, image policy, device suitability and capacity are checked again at allocation."},nil
}
