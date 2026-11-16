package controller

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

const testBatchImage = "docker.io/library/alpine:3.20@sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"

type batchAPIFixture struct{
	mu sync.Mutex
	job map[string]any
	created int
	active bool
	complete bool
	admissionError bool
}

func (f *batchAPIFixture) ServeHTTP(w http.ResponseWriter,r *http.Request){
	f.mu.Lock()
	defer f.mu.Unlock()
	w.Header().Set("Content-Type","application/json")
	switch{
	case r.URL.Path=="/apis/batch/v1/namespaces/pwm-batch/jobs" && r.Method=="POST":
		if f.job!=nil{http.Error(w,"conflict",409);return}
		if err:=json.NewDecoder(r.Body).Decode(&f.job);err!=nil{http.Error(w,"bad JSON",400);return}
		metadata:=decodeAnyObject(f.job["metadata"])
		metadata["uid"]="batch-uid"
		f.created++
		_ = json.NewEncoder(w).Encode(f.job)
	case strings.HasPrefix(r.URL.Path,"/apis/batch/v1/namespaces/pwm-batch/jobs/"):
		if f.job==nil{http.Error(w,"missing",404);return}
		name:=strings.TrimPrefix(r.URL.Path,"/apis/batch/v1/namespaces/pwm-batch/jobs/")
		if name!=asString(decodeAnyObject(f.job["metadata"])["name"]){http.Error(w,"missing",404);return}
		if r.Method=="DELETE"{
			var opts map[string]any
			if err:=json.NewDecoder(r.Body).Decode(&opts);err!=nil{http.Error(w,"invalid delete",400);return}
			if asString(decodeAnyObject(opts["preconditions"])["uid"])!="batch-uid"{http.Error(w,"bad uid",409);return}
			f.job=nil
			_,_ = w.Write([]byte("{}"))
			return
		}
		if r.Method!="GET"{http.Error(w,"method",405);return}
		if f.complete{
			f.job["status"]=map[string]any{"conditions":[]any{map[string]any{"type":"Complete","status":"True"}},"succeeded":1}
		}else if f.active{
			f.job["status"]=map[string]any{"active":1}
		}
		_ = json.NewEncoder(w).Encode(f.job)
	case r.URL.Path=="/apis/kueue.x-k8s.io/v1beta2/namespaces/pwm-batch/workloads":
		if f.admissionError {http.Error(w,"temporary failure",503);return}
		_ = json.NewEncoder(w).Encode(map[string]any{"items":[]any{map[string]any{
			"metadata":map[string]any{"ownerReferences":[]any{map[string]any{"uid":"batch-uid"}}},
			"status":map[string]any{"conditions":[]any{map[string]any{"type":"Admitted","status":"True"}}},
		}}})
	case r.URL.Path=="/api/v1/namespaces/pwm-batch/pods":
		_ = json.NewEncoder(w).Encode(map[string]any{"items":[]any{map[string]any{
			"metadata":map[string]any{"name":"execution-0","ownerReferences":[]any{map[string]any{"uid":"batch-uid"}}},
		}}})
	case strings.HasPrefix(r.URL.Path,"/api/v1/namespaces/pwm-batch/pods/execution-0/log"):
		_,_ = w.Write([]byte("calculation-complete\n"))
	default:
		http.Error(w,"unexpected endpoint",404)
	}
}

func batchFixture(t *testing.T)(*Database,*Server,*httptest.Server,*batchAPIFixture){
	t.Helper()
	db,srv,ts:=fixture(t)
	if _,err:=db.CreateProject("research","Research");err!=nil{t.Fatal(err)}
	mock:=&batchAPIFixture{}
	kube:=httptest.NewTLSServer(mock)
	t.Cleanup(kube.Close)
	tokenFile:=filepath.Join(t.TempDir(),"token")
	if err:=os.WriteFile(tokenFile,[]byte("test-project-controller-token"),0600);err!=nil{t.Fatal(err)}
	srv.Kueue=&KueueBackend{endpoint:kube.URL,tokenFile:tokenFile,client:kube.Client()}
	return db,srv,ts,mock
}

func TestBatchSubmissionIdempotenceAndWorkerIsolation(t *testing.T){
	db,srv,_,_:=batchFixture(t)
	const key="repeatable-001"
	first,err:=db.QueueKueueJob("research",key,"echo ok",testBatchImage,0.25,128)
	if err!=nil{t.Fatal(err)}
	// Earlier controllers claim status='queued' without recognizing Kueue.
	// A Kueue job must remain outside that claim path during rollback.
	stored,err:=db.queryOne("SELECT status FROM jobs WHERE id=?",first["id"])
	if err!=nil||stored["status"]!="kueue_queued"{
		t.Fatalf("rollback-unsafe job status: %v %v",stored,err)
	}
	again,err:=db.QueueKueueJob("research",key,"echo ok",testBatchImage,0.25,128)
	if err!=nil||first["id"]!=again["id"]{t.Fatalf("idempotent submission: %v %v",again,err)}
	if _,err:=db.QueueKueueJob("research",key,"echo changed",testBatchImage,0.25,128);err==nil{
		t.Fatal("idempotency key accepted changed command")
	}
	if _,err:=db.QueueKueueJob("research","invalid-001","echo ok","debian:bookworm-slim",1,512);err==nil{
		t.Fatal("mutable image accepted")
	}
	if _,err:=db.QueueKueueJob("research","invalid-002","echo ok",testBatchImage,2.5,512);err==nil{
		t.Fatal("over-quota CPU accepted")
	}
	if _,err:=db.RegisterNode("test-worker",caps());err!=nil{t.Fatal(err)}
	claimed,err:=db.ClaimJob("test-worker")
	if err!=nil{t.Fatal(err)}
	if claimed!=nil{t.Fatalf("Kueue job was claimed by native worker: %v",claimed)}
	if srv.Kueue==nil{t.Fatal("batch backend not configured")}
}

func TestKueueRequestIdentityResolvesExactOwnedJob(t *testing.T) {
	db, _, endpoint, _ := batchFixture(t)
	const key = "reconcile-batch-001"
	submitted, err := db.QueueKueueJob("research", key, "printf receipt", testBatchImage, 0.25, 128)
	if err != nil {
		t.Fatal(err)
	}
	receipt, err := db.KueueJobByRequest("research", key)
	if err != nil || receipt["id"] != submitted["id"] || receipt["request_id"] != key ||
		receipt["status"] != "queued" || receipt["backend"] != "kueue" {
		t.Fatalf("request identity did not resolve its durable job: %v %v", receipt, err)
	}
	if _, err := db.KueueJobByRequest("other", key); err == nil {
		t.Fatal("cross-project request resolved a job")
	}
	for _, invalid := range []string{"missing-request-001", "../../request", "short", ""} {
		if _, err := db.KueueJobByRequest("research", invalid); err == nil {
			t.Fatalf("unrecognized request accepted: %q", invalid)
		}
	}
	code, response := call(t, endpoint, "POST", "/mcp", testOwner, map[string]any{
		"jsonrpc": "2.0", "id": 1, "method": "tools/call",
		"params": map[string]any{
			"name": "compute_job_by_request",
			"arguments": map[string]any{"project_id": "research", "request_id": key},
		},
	})
	wantOK(t, code, response)
	result := gitObj(gitObj(response["result"])["structuredContent"])
	if result["id"] != submitted["id"] || result["request_id"] != key {
		t.Fatalf("MCP request lookup did not return exact job: %v", response)
	}
	code, _ = call(t, endpoint, "POST", "/mcp", "", map[string]any{
		"jsonrpc": "2.0", "id": 2, "method": "tools/call",
		"params": map[string]any{
			"name": "compute_job_by_request",
			"arguments": map[string]any{"project_id": "research", "request_id": key},
		},
	})
	if code != http.StatusUnauthorized {
		t.Fatalf("anonymous job-receipt lookup accepted: HTTP %d", code)
	}
}

func TestKueueLifecycleMCPAndCancellation(t *testing.T){
	db,srv,httpServer,fake:=batchFixture(t)
	args:=map[string]any{
		"project_id":"research","backend":"kueue","request_id":"attempt-001",
		"command":"echo calculation-complete","image":testBatchImage,"cpu":0.25,"memory_mb":128,
	}
	code,response:=call(t,httpServer,"POST","/mcp",testOwner,map[string]any{
		"jsonrpc":"2.0","id":1,"method":"tools/call",
		"params":map[string]any{"name":"submit_compute_job","arguments":args},
	})
	wantOK(t,code,response)
	result:=gitObj(gitObj(response["result"])["structuredContent"])
	id:=asString(result["id"])
	if id==""{t.Fatalf("missing job ID: %v",response)}
	pending,err:=db.pendingKueueJobs()
	if err!=nil||len(pending)!=1{t.Fatalf("pending job: %v %v",pending,err)}
	if err:=srv.Kueue.reconcile(context.Background(),db,pending[0]);err!=nil{t.Fatal(err)}
	job,err:=db.KueueJob("research",id)
	if err!=nil||job["status"]!="admitted"{t.Fatalf("not admitted: %v %v",job,err)}
	fake.mu.Lock();fake.active=true;fake.mu.Unlock()
	pending,_=db.pendingKueueJobs()
	if err:=srv.Kueue.reconcile(context.Background(),db,pending[0]);err!=nil{t.Fatal(err)}
	job,_=db.KueueJob("research",id)
	if job["status"]!="running"{t.Fatalf("not running: %v",job)}
	fake.mu.Lock();fake.complete=true;fake.mu.Unlock()
	pending,_=db.pendingKueueJobs()
	if err:=srv.Kueue.reconcile(context.Background(),db,pending[0]);err!=nil{t.Fatal(err)}
	job,_=db.KueueJob("research",id)
	if job["status"]!="succeeded"||!strings.Contains(asString(job["result"]),"calculation-complete"){
		t.Fatalf("missing durable result: %v",job)
	}
	fake.mu.Lock();created:=fake.created;fake.mu.Unlock()
	if created!=1{t.Fatalf("non-idempotent Kubernetes create: %d",created)}
	code,response=call(t,httpServer,"GET","/api/projects/research/jobs/"+id,testOwner,nil)
	wantOK(t,code,response)
	if response["status"]!="succeeded"{t.Fatalf("REST status: %v",response)}
	code,_=call(t,httpServer,"GET","/api/projects/research/jobs/"+id,"",nil)
	if code!=401{t.Fatalf("anonymous job status accepted: %d",code)}
	code,response=call(t,httpServer,"POST","/api/projects/research/jobs/"+id+"/cancel",testOwner,nil)
	wantOK(t,code,response)
	if response["status"]!="succeeded"{t.Fatalf("completed job cancelled: %v",response)}
	_,err=db.KueueJob("other",id)
	if err==nil{t.Fatal("cross-project job read allowed")}
}

func TestKueueCancelAndRestartRecovery(t *testing.T){
	db,srv,_,fake:=batchFixture(t)
	first,err:=db.QueueKueueJob("research","cancel-001","echo no",testBatchImage,0.1,64)
	if err!=nil{t.Fatal(err)}
	id:=asString(first["id"])
	if _,err:=db.CancelKueueJob("research",id);err!=nil{t.Fatal(err)}
	pending,err:=db.pendingKueueJobs()
	if err!=nil{t.Fatal(err)}
	if err:=srv.Kueue.reconcile(context.Background(),db,pending[0]);err!=nil{t.Fatal(err)}
	status,err:=db.KueueJob("research",id)
	if err!=nil||status["status"]!="cancelled"{t.Fatalf("cancellation not durable: %v %v",status,err)}
	fake.mu.Lock();created:=fake.created;fake.mu.Unlock()
	if created!=0{t.Fatal("cancelled job was created remotely")}
	second,err:=db.QueueKueueJob("research","recovery-001","echo resuming",testBatchImage,0.1,64)
	if err!=nil{t.Fatal(err)}
	pending,_=db.pendingKueueJobs()
	if err:=srv.Kueue.reconcile(context.Background(),db,pending[0]);err!=nil{t.Fatal(err)}
	restarted:=&KueueBackend{endpoint:srv.Kueue.endpoint,tokenFile:srv.Kueue.tokenFile,client:srv.Kueue.client}
	pending,_=db.pendingKueueJobs()
	if err:=restarted.reconcile(context.Background(),db,pending[0]);err!=nil{t.Fatal(err)}
	status,err=db.KueueJob("research",asString(second["id"]))
	if err!=nil||status["status"]!="admitted"{t.Fatalf("resume failed: %v %v",status,err)}
	fake.mu.Lock();created=fake.created;fake.mu.Unlock()
	if created!=1{t.Fatalf("restart submitted duplicate Job: %d",created)}
	if _,err:=db.CancelKueueJob("research",asString(second["id"]));err!=nil{t.Fatal(err)}
	pending,_=db.pendingKueueJobs()
	if err:=restarted.reconcile(context.Background(),db,pending[0]);err!=nil{t.Fatal(err)}
	pending,_=db.pendingKueueJobs()
	if err:=restarted.reconcile(context.Background(),db,pending[0]);err!=nil{t.Fatal(err)}
	status,_=db.KueueJob("research",asString(second["id"]))
	if status["status"]!="cancelled"{t.Fatalf("remote cleanup not confirmed: %v",status)}
}

func TestKueueRejectsMutatedJob(t *testing.T){
	row:=map[string]any{
		"id":"123456789abcdef0123456789abcdef0","project":"research",
		"remote_name":"pwm-123456789abcdef0123456789abcdef0",
		"request_id":"request-001","command":"echo checked",
		"image":testBatchImage,"cpu":0.25,"memory_mb":128,
	}
	create:=func()map[string]any{
		obj:=batchKubernetesJob(row)
		decodeAnyObject(obj["metadata"])["uid"]="batch-uid"
		raw,err:=json.Marshal(obj)
		if err!=nil{t.Fatal(err)}
		var decoded map[string]any
		if err:=json.Unmarshal(raw,&decoded);err!=nil{t.Fatal(err)}
		return decoded
	}
	if _,err:=matchBatchJob(create(),row);err!=nil{t.Fatalf("valid Job rejected: %v",err)}
	for _,tamper:=range []struct{name string;change func(map[string]any)}{
		{"project",func(o map[string]any){decodeAnyObject(decodeAnyObject(o["metadata"])["annotations"])["homelab.dbontr.me/project"]="other"}},
		{"queue",func(o map[string]any){decodeAnyObject(decodeAnyObject(o["metadata"])["labels"])["kueue.x-k8s.io/queue-name"]="foreign"}},
		{"cpu",func(o map[string]any){
			pod:=decodeAnyObject(decodeAnyObject(decodeAnyObject(o["spec"])["template"])["spec"])
			container:=decodeAnyObject(asAnySlice(pod["containers"])[0])
			decodeAnyObject(decodeAnyObject(container["resources"])["limits"])["cpu"]="8000m"
		}},
		{"service-account",func(o map[string]any){
			pod:=decodeAnyObject(decodeAnyObject(decodeAnyObject(o["spec"])["template"])["spec"])
			pod["automountServiceAccountToken"]=true
		}},
		{"init-container",func(o map[string]any){
			pod:=decodeAnyObject(decodeAnyObject(decodeAnyObject(o["spec"])["template"])["spec"])
			pod["initContainers"]=[]any{map[string]any{"name":"unexpected"}}
		}},
	}{
		t.Run(tamper.name,func(t *testing.T){
			obj:=create()
			tamper.change(obj)
			if _,err:=matchBatchJob(obj,row);err==nil{t.Fatal("accepted tampered Kubernetes Job")}
		})
	}
}

func TestKueuePinsUIDBeforeAdmissionError(t *testing.T){
	db,srv,_,fake:=batchFixture(t)
	created,err:=db.QueueKueueJob("research","outage-001","echo resuming",testBatchImage,0.1,64)
	if err!=nil{t.Fatal(err)}
	fake.mu.Lock()
	fake.admissionError=true
	fake.mu.Unlock()
	pending,err:=db.pendingKueueJobs()
	if err!=nil{t.Fatal(err)}
	if err:=srv.Kueue.reconcile(context.Background(),db,pending[0]);err==nil{t.Fatal("admission failure ignored")}
	persisted,err:=db.KueueJob("research",asString(created["id"]))
	if err!=nil||asString(persisted["remote_uid"])!="batch-uid"{
		t.Fatalf("remote UID not persisted: %v %v",persisted,err)
	}
	fake.mu.Lock()
	fake.job=nil
	fake.admissionError=false
	fake.mu.Unlock()
	pending,_=db.pendingKueueJobs()
	if err:=srv.Kueue.reconcile(context.Background(),db,pending[0]);err!=nil{t.Fatal(err)}
	persisted,_=db.KueueJob("research",asString(created["id"]))
	if persisted["status"]!="failed"{t.Fatalf("missing Job was recreated: %v",persisted)}
	fake.mu.Lock()
	count:=fake.created
	fake.mu.Unlock()
	if count!=1{t.Fatalf("duplicate execution: %d",count)}
}
