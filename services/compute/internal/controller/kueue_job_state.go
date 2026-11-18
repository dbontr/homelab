package controller

import (
	"errors"
	"math"
	"regexp"
	"strings"
)

const (
	batchNamespace = "pwm-batch"
	batchQueue = "research-cpu"
	batchJobLabel = "homelab.dbontr.me/job-id"
)

var batchRequestID = regexp.MustCompile("^[A-Za-z0-9][A-Za-z0-9._-]{7,63}$")
var batchJobID = regexp.MustCompile("^[0-9a-f]{32}$")

func validateBatchJob(command, image, requestID string, cpu float64, memory int) error {
	pieces := strings.Split(image, "@sha256:")
	if strings.TrimSpace(command) == "" || len(command) > 16000 ||
		len(pieces) != 2 || pieces[0] == "" || !validImage.MatchString(image) ||
		!shaPattern.MatchString(pieces[len(pieces)-1]) {
		return errors.New("batch execution requires a command and immutable SHA-256 OCI image")
	}
	if !batchRequestID.MatchString(requestID) {
		return errors.New("request_id must contain 8-64 safe characters")
	}
	if math.IsNaN(cpu) || math.IsInf(cpu, 0) || cpu < 0.1 || cpu > 2 || memory < 64 || memory > 4096 {
		return errors.New("CPU must be 0.1-2 and memory_mb must be 64-4096")
	}
	return nil
}

// A durable Workspace record precedes remote Kubernetes execution. A repeated
// request ID must represent the same work, even after a controller restart.
func (db *Database) QueueKueueJob(project, requestID, command, image string, cpu float64, memory int) (map[string]any, error) {
	if err := validateBatchJob(command, image, requestID, cpu, memory); err != nil {
		return nil, err
	}
	var out map[string]any
	err := db.transaction(func() error {
		if err := requireProject(db, project); err != nil { return err }
		existing, err := db.lockedOne("SELECT j.id,j.status,j.command,j.image,j.cpu,j.memory_mb FROM kueue_jobs k JOIN jobs j ON j.id=k.job WHERE k.project=? AND k.request_id=?", project, requestID)
		if err != nil { return err }
		if existing != nil {
			if asString(existing["command"]) != command || asString(existing["image"]) != image ||
				asFloat(existing["cpu"]) != cpu || asInt(existing["memory_mb"]) != memory {
				return errors.New("request_id already identifies a different batch job")
			}
			status:=asString(existing["status"])
			if status=="kueue_queued"{status="queued"}
			out=map[string]any{"id":existing["id"],"status":status,"backend":"kueue","request_id":requestID}
			return nil
		}
		id, now := randomHex(16), stamp()
		if err:=db.execLocked("INSERT INTO jobs(id,project,command,image,cpu,memory_mb,status,created,updated) VALUES(?,?,?,?,?,?,'kueue_queued',?,?)",
			id,project,command,image,cpu,memory,now,now);err!=nil{return err}
		if err:=db.execLocked("INSERT INTO kueue_jobs(job,project,request_id,remote_name,create_attempted,created,updated) VALUES(?,?,?,?,0,?,?)",
			id,project,requestID,"pwm-"+id,now,now);err!=nil{return err}
		out=map[string]any{"id":id,"status":"queued","backend":"kueue","request_id":requestID}
		return nil
	})
	return out,err
}

func (db *Database) KueueJob(project,id string) (map[string]any,error) {
	if !validID.MatchString(project)||!batchJobID.MatchString(id){return nil,errors.New("invalid project or job ID")}
	result,err:=db.queryOne("SELECT j.id,j.project,j.status,j.node,j.image,j.cpu,j.memory_mb,j.created,j.updated,j.result,k.request_id,k.remote_name,k.remote_uid,k.last_error FROM jobs j JOIN kueue_jobs k ON j.id=k.job WHERE j.project=? AND j.id=?",project,id)
	if err!=nil{return nil,err}
	if result==nil{return nil,errors.New("batch job not found")}
	if result["status"]=="kueue_queued"{result["status"]="queued"}
	result["backend"]="kueue"
	return result,nil
}

// KueueJobByRequest resolves the controller's durable job ID from the exact
// project-scoped idempotency identity. An executor retry must not guess a job ID.
func (db *Database) KueueJobByRequest(project, requestID string) (map[string]any, error) {
	if !validID.MatchString(project) || !batchRequestID.MatchString(requestID) {
		return nil, errors.New("invalid project or batch request identity")
	}
	row, err := db.queryOne("SELECT job FROM kueue_jobs WHERE project=? AND request_id=?", project, requestID)
	if err != nil {
		return nil, err
	}
	if row == nil {
		return nil, errors.New("batch request not found")
	}
	return db.KueueJob(project, asString(row["job"]))
}

func (db *Database) CancelKueueJob(project,id string)(map[string]any,error){
	if !validID.MatchString(project)||!batchJobID.MatchString(id){return nil,errors.New("invalid project or job ID")}
	var out map[string]any
	err:=db.transaction(func()error{
		row,err:=db.lockedOne("SELECT j.status FROM jobs j JOIN kueue_jobs k ON j.id=k.job WHERE j.project=? AND j.id=?",project,id)
		if err!=nil{return err}
		if row==nil{return errors.New("batch job not found")}
		status:=asString(row["status"])
		if status=="kueue_queued"||status=="admitted"||status=="running"||status=="recovery_required"{
			status="cancel_requested"
			if err:=db.execLocked("UPDATE jobs SET status=?,updated=? WHERE id=? AND project=?",status,stamp(),id,project);err!=nil{return err}
		}
		out=map[string]any{"id":id,"status":status,"backend":"kueue"}
		return nil
	})
	return out,err
}

func (db *Database) pendingKueueJobs()([]map[string]any,error){
	return db.query("SELECT j.id,j.project,j.status,j.command,j.image,j.cpu,j.memory_mb,k.request_id,k.remote_name,k.remote_uid,k.create_attempted FROM jobs j JOIN kueue_jobs k ON j.id=k.job WHERE j.status IN ('kueue_queued','admitted','running','cancel_requested','recovery_required') ORDER BY j.created,j.id LIMIT 100")
}

// beginKueueSubmission commits the outbound-create fence before any remote
// HTTP request. A second reconciler must not send a competing create, even
// when the first process loses its response or exits before the request.
func (db *Database) beginKueueSubmission(id string) (bool, error) {
	var permitted bool
	err := db.transaction(func() error {
		row, err := db.lockedOne("SELECT j.status,k.create_attempted FROM kueue_jobs k JOIN jobs j ON j.id=k.job WHERE k.job=?", id)
		if err != nil { return err }
		if row == nil { return errors.New("batch job not found") }
		if asString(row["status"]) != "kueue_queued" || asInt(row["create_attempted"]) != 0 { return nil }
		if err := db.execLocked("UPDATE kueue_jobs SET create_attempted=1,updated=? WHERE job=? AND create_attempted=0", stamp(), id); err != nil { return err }
		permitted = true
		return nil
	})
	return permitted, err
}

// A missing remote Job after a recorded create attempt is ambiguous: the
// cluster may already have run and garbage-collected it. Preserve the job and
// surface the uncertainty instead of silently submitting the work again.
func (db *Database) requireKueueRecovery(id, reason string) error {
	if len(reason) > 500 { reason = reason[:500] }
	return db.transaction(func() error {
		row, err := db.lockedOne("SELECT j.status,k.last_error FROM kueue_jobs k JOIN jobs j ON j.id=k.job WHERE k.job=?", id)
		if err != nil { return err }
		if row == nil { return errors.New("batch job not found") }
		prior := asString(row["status"])
		if prior == "cancel_requested" || prior == "succeeded" || prior == "failed" || prior == "cancelled" { return nil }
		if prior == "recovery_required" && asString(row["last_error"]) == reason { return nil }
		if err := db.execLocked("UPDATE jobs SET status='recovery_required',updated=? WHERE id=?", stamp(), id); err != nil { return err }
		return db.execLocked("UPDATE kueue_jobs SET last_error=?,updated=? WHERE job=?", reason, stamp(), id)
	})
}

func (db *Database) updateKueueJob(id,uid,status,result string)error{
	if len(result)>32768{result=result[:32768]}
	return db.transaction(func()error{
		row,err:=db.lockedOne("SELECT k.remote_uid,j.status FROM kueue_jobs k JOIN jobs j ON j.id=k.job WHERE k.job=?",id)
		if err!=nil{return err}
		if row==nil{return errors.New("batch job not found")}
		oldUID:=asString(row["remote_uid"])
		if oldUID!=""&&uid!=""&&oldUID!=uid{return errors.New("Kubernetes Job UID changed")}
		if uid==""{uid=oldUID}
		if err:=db.execLocked("UPDATE kueue_jobs SET remote_uid=?,last_error='',updated=? WHERE job=?",uid,stamp(),id);err!=nil{return err}
		prior:=asString(row["status"])
		if prior=="succeeded"||prior=="failed"||prior=="cancelled"{return nil}
		if prior=="cancel_requested"&&status!="cancelled"{return nil}
		if status=="cancelled"&&prior!="cancel_requested"{return errors.New("cancelled job without request")}
		if status=="queued"{status="kueue_queued"}
		return db.execLocked("UPDATE jobs SET status=?,result=CASE WHEN ?='' THEN result ELSE ? END,updated=? WHERE id=?",status,result,result,stamp(),id)
	})
}

func (db *Database) kueueError(id string,err error){
	message:=err.Error()
	if len(message)>500{message=message[:500]}
	_ = db.exec("UPDATE kueue_jobs SET last_error=?,updated=? WHERE job=?",message,stamp(),id)
}
