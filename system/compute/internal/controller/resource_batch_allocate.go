package controller

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
)

// BatchAllocationRequest names an idempotent, explicitly authorized group of
// workspaces. Replaying the same ID cannot create additional workspaces.
type BatchAllocationRequest struct {
	BatchID string `json:"batch_id"`
	ProjectID string `json:"project_id"`
	Workloads []BatchWorkload `json:"workloads"`
}

type BatchWorkspace struct {
	WorkloadID string `json:"workload_id"`
	WorkspaceID string `json:"workspace_id"`
	Node string `json:"node"`
	Status string `json:"status"`
}

type BatchAllocationResult struct {
	BatchID string `json:"batch_id"`
	ProjectID string `json:"project_id"`
	Workspaces []BatchWorkspace `json:"workspaces"`
}

// confirmedBatchLocked retrieves a committed receipt under the SQLite writer.
// The receipt is protected by its exact request fingerprint and project.
func (db *Database) confirmedBatchLocked(batchID, project, fingerprint string) (BatchAllocationResult, bool, error) {
	var result BatchAllocationResult
	record, err := db.lockedOne("SELECT project,fingerprint,result FROM workspace_batch_requests WHERE id=?", batchID)
	if err != nil { return result, false, err }
	if record == nil { return result, false, nil }
	if asString(record["project"])!=project || asString(record["fingerprint"])!=fingerprint {
		return result, false, errors.New("workspace batch idempotency conflict")
	}
	if err:=json.Unmarshal([]byte(asString(record["result"])),&result);err!=nil {
		return result,false,errors.New("invalid persisted workspace batch receipt")
	}
	if result.BatchID!=batchID || result.ProjectID!=project || len(result.Workspaces)==0 {
		return result,false,errors.New("workspace batch receipt scope mismatch")
	}
	return result,true,nil
}

func (db *Database) confirmedBatch(batchID, project, fingerprint string) (BatchAllocationResult, bool, error) {
	var result BatchAllocationResult
	var found bool
	err:=db.transaction(func()error{
		var err error
		result,found,err=db.confirmedBatchLocked(batchID,project,fingerprint)
		return err
	})
	return result,found,err
}

// AllocateWorkspaceBatch converts an advisory batch into a single atomic
// allocation. Every worker and device is rechecked within the controller's
// canonical workspace transaction. Partial placements are never committed.
func (db *Database) AllocateWorkspaceBatch(req BatchAllocationRequest) (BatchAllocationResult, error) {
	var result BatchAllocationResult
	if !validID.MatchString(req.BatchID) || !validID.MatchString(req.ProjectID) ||
		len(req.Workloads)<1 || len(req.Workloads)>12 {
		return result,errors.New("invalid workspace batch identity or size")
	}
	raw,err:=json.Marshal(req)
	if err!=nil{return result,err}
	digest:=sha256.Sum256(raw)
	fingerprint:=hex.EncodeToString(digest[:])

	if previous,found,err:=db.confirmedBatch(req.BatchID,req.ProjectID,fingerprint);err!=nil {
		return result,err
	}else if found {
		return previous,nil
	}

	// Planning is intentionally advisory. A complete plan is necessary but not
	// sufficient: the write transaction repeats all admission and lease checks.
	plan,err:=db.PlanWorkspaceBatch(BatchPlanRequest{
		ProjectID:req.ProjectID,Workloads:req.Workloads,
	})
	if err!=nil{return result,err}
	placements,ok:=plan["placements"].([]BatchPlacement)
	if !ok || len(placements)!=len(req.Workloads) || asInt(plan["unplaced"])!=0 {
		return result,errors.New("batch has workloads without eligible placements")
	}
	approved:=make(map[string]WorkspaceRequest,len(req.Workloads))
	for _,job:=range req.Workloads {
		if !validID.MatchString(job.ID) {
			return result,errors.New("invalid batch workload identity")
		}
		approved[job.ID]=job.Request
	}

	err=db.transaction(func()error{
		if previous,found,err:=db.confirmedBatchLocked(req.BatchID,req.ProjectID,fingerprint);err!=nil {
			return err
		}else if found {
			result=previous
			return nil
		}
		result=BatchAllocationResult{
			BatchID:req.BatchID,ProjectID:req.ProjectID,
			Workspaces:make([]BatchWorkspace,0,len(placements)),
		}
		for _,placement:=range placements {
			if placement.Status!="candidate" || !validID.MatchString(placement.Node) {
				return errors.New("workspace batch contains an unapproved placement")
			}
			request,exists:=approved[placement.ID]
			if !exists { return errors.New("batch placement identity mismatch") }
			request.ProjectID=req.ProjectID
			if request.NodeID!="" && request.NodeID!=placement.Node {
				return errors.New("batch placement differs from requested worker")
			}
			request.NodeID=placement.Node
			workspace,err:=db.createWorkspaceLocked(request)
			if err!=nil {
				return fmt.Errorf("batch workspace %s: %w",placement.ID,err)
			}
			result.Workspaces=append(result.Workspaces,BatchWorkspace{
				WorkloadID:placement.ID,
				WorkspaceID:asString(workspace["id"]),
				Node:asString(workspace["node"]),
				Status:asString(workspace["status"]),
			})
		}
		persisted,err:=json.Marshal(result)
		if err!=nil{return err}
		if len(persisted)>16<<10{return errors.New("workspace batch receipt too large")}
		return db.execLocked("INSERT INTO workspace_batch_requests(id,project,fingerprint,result,created) VALUES(?,?,?,?,?)",
			req.BatchID,req.ProjectID,fingerprint,string(persisted),stamp())
	})
	return result,err
}
