package agentflow

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strings"
	"testing"
	"time"
)

type fakeCompute struct {
	calls            []string
	operations       map[string]map[string]any
	files            map[string]string
	count            int
	failCheck        bool
	mismatchWrite    bool
	mismatchRevision bool
	mismatchDraft    bool
	flatExport       bool
	mismatchArtifact bool
	lastProvision map[string]any
	mutateOnCheck    bool
	exported         map[string]string
	drafts           int
	writes           int
}

func (f *fakeCompute) Call(_ context.Context, name string, args map[string]any) (map[string]any, error) {
	f.calls = append(f.calls, name)
	switch name {
	case "workspace_create":
		f.lastProvision=args
		return map[string]any{"id": "testworkspace", "status": "starting"}, nil
	case "workspace_list":
		return map[string]any{"result": []any{map[string]any{"id": "testworkspace", "status": "ready"}}}, nil
	case "github_checkout_repository":
		return map[string]any{"operation_id": "checkout"}, nil
	case "workspace_call":
		if f.operations == nil {
			f.operations = map[string]map[string]any{}
		}
		f.count++
		id := fmt.Sprintf("op%d", f.count)
		action := str(args["action"])
		result := map[string]any{}
		switch action {
		case "read_file":
			filename := str(obj(args["payload"])["path"])
			if body, ok := f.files[filename]; ok {
				result["content"] = body
			} else {
				return nil, errors.New("source file not found")
			}
		case "write_file":
			payload := obj(args["payload"])
			if f.files == nil {
				f.files = map[string]string{}
			}
			filename, body := str(payload["path"]), str(payload["content"])
			if expected, ok := payload["expected_sha256"].(string); ok {
				actual := ""
				if prior, exists := f.files[filename]; exists {
					actual = shaHex(prior)
				}
				if expected != actual {
					return nil, errors.New("source revision conflict")
				}
			}
			f.writes++
			f.files[filename] = body
			digest := sha256.Sum256([]byte(body))
			result["sha256"] = hex.EncodeToString(digest[:])
			if f.mismatchWrite {
				result["sha256"] = strings.Repeat("0", 64)
			}
		case "command":
			code := float64(0)
			if f.failCheck {
				code = 1
			}
			if f.mutateOnCheck && code == 0 {
				f.files["repos/example__repo/README.md"] = "unexpected source mutation"
			}
			result["exit_code"] = code
			result["stdout"] = "source:README.md:1:package sample"
		case "list_directory":
			result["stdout"] = "README.md\n"
		case "export_file":
			filename := str(obj(args["payload"])["path"])
			body, ok := f.files[filename]
			if !ok {
				return nil, errors.New("exported source file does not exist")
			}
			if f.exported == nil {
				f.exported = map[string]string{}
			}
			f.exported["artifact1"] = body
			if f.flatExport {
				result["artifact_id"] = "artifact1"
			} else {
				result["artifact"] = map[string]any{"id": "artifact1", "sha256": shaHex(body), "size": len(body)}
			}
		default:
			return nil, errors.New("unexpected workspace action")
		}
		f.operations[id] = result
		return map[string]any{"operation_id": id}, nil
	case "workspace_operation":
		id := str(args["operation_id"])
		if id == "checkout" {
			revision := strings.Repeat("a", 40)
			if f.mismatchRevision {
				revision = strings.Repeat("b", 40)
			}
			return map[string]any{"status": "succeeded", "result": map[string]any{"repository": "example/repo", "revision": revision}}, nil
		}
		result, ok := f.operations[id]
		if !ok {
			return nil, errors.New("unknown operation")
		}
		return map[string]any{"status": "succeeded", "result": result}, nil
	case "artifact_info":
		id := str(args["artifact_id"])
		body, ok := f.exported[id]
		if !ok {
			return nil, errors.New("artifact unavailable")
		}
		digest := shaHex(body)
		if f.mismatchArtifact {
			digest = strings.Repeat("0", 64)
		}
		return map[string]any{"id": id, "project": str(args["project_id"]), "sha256": digest, "size": len(body)}, nil
	case "github_publish_artifacts":
		f.drafts++
		changes, err := stringMap(args["changes"])
		if str(args["base_branch"]) != "main" || err != nil || len(changes) == 0 {
			return nil, errors.New("invalid publication")
		}
		base := strings.Repeat("a", 40)
		if f.mismatchDraft {
			base = strings.Repeat("c", 40)
		}
		return map[string]any{"draft": true, "number": 12, "base_sha": base, "url": "https://github.com/example/repo/pull/12"}, nil
	default:
		return nil, fmt.Errorf("unexpected compute tool %s", name)
	}
}
func sampleProgram() Program {
	return Program{
		Project: "research", Repository: "example/repo",
		Revision: strings.Repeat("a", 40), BaseBranch: "main", ExpectedHead: strings.Repeat("a", 40),
		Objective: "Correct the documentation", Changes: map[string]string{"README.md": "Correct details"},
		Checks: []string{"go test ./..."}, ImagePoolID: "debian-bookworm-slim",
		CPU: 1, MemoryMB: 2048, DurationMinutes: 30, Title: "Repair documentation",
	}
}
func testEngine(t *testing.T) (*Engine, *fakeCompute) {
	t.Helper()
	store := testStore(t)
	compute := &fakeCompute{files: map[string]string{"repos/example__repo/README.md": "package sample"}}
	e := &Engine{Store: store, Compute: compute, AllowedProjects: map[string]bool{"research": true}}
	return e, compute
}
func TestEngineeringRunPreparesOnlyDraftAfterVerifiedChecks(t *testing.T) {
	e, compute := testEngine(t)
	created, err := e.Submit(sampleProgram())
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 8; i++ {
		run, err := e.Get(created.ID)
		if err != nil {
			t.Fatal(err)
		}
		if run.Status == Succeeded {
			break
		}
		progress, err := (&Runner{Store: e.Store, Worker: e, Lease: time.Minute}).RunOne(context.Background(), created.ID)
		if err != nil || !progress {
			t.Fatalf("step %d: progress=%v err=%v", i, progress, err)
		}
	}
	run, err := e.Get(created.ID)
	if err != nil {
		t.Fatal(err)
	}
	if run.Status != Succeeded || compute.drafts != 1 {
		t.Fatalf("expected verified draft: status=%s drafts=%d steps=%+v", run.Status, compute.drafts, run.Steps)
	}
	if run.Steps[len(run.Steps)-1].Kind != "publish" {
		t.Fatal("missing publication result")
	}
}
func TestFailedVerificationCannotPublish(t *testing.T) {
	e, compute := testEngine(t)
	compute.failCheck = true
	created, err := e.Submit(sampleProgram())
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 8; i++ {
		progress, err := (&Runner{Store: e.Store, Worker: e, Lease: time.Minute}).RunOne(context.Background(), created.ID)
		if err != nil {
			t.Fatal(err)
		}
		if !progress {
			break
		}
	}
	run, err := e.Get(created.ID)
	if err != nil {
		t.Fatal(err)
	}
	if run.Status != Failed || compute.drafts != 0 {
		t.Fatalf("verification bypassed: status=%s drafts=%d", run.Status, compute.drafts)
	}
}

type untrustedAuthor struct{}

func (untrustedAuthor) Propose(context.Context, string, map[string]string, []string) (map[string]string, error) {
	return map[string]string{"unapproved-file": "malicious"}, nil
}
func TestGeneratedEditsAreRestrictedToOwnerAllowedPaths(t *testing.T) {
	e, compute := testEngine(t)
	e.Author = untrustedAuthor{}
	p := sampleProgram()
	p.Changes = nil
	p.AllowedPaths = []string{"README.md"}
	created, err := e.Submit(p)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 4; i++ {
		progressed, err := (&Runner{Store: e.Store, Worker: e, Lease: time.Minute}).RunOne(context.Background(), created.ID)
		if err != nil || !progressed {
			t.Fatal(err)
		}
	}
	run, err := e.Get(created.ID)
	if err != nil {
		t.Fatal(err)
	}
	if run.Status != Failed || compute.drafts != 0 {
		t.Fatalf("unauthorized edit escaped: %+v", run)
	}
}
func TestUnauthorizedProjectAndTraversalAreRejected(t *testing.T) {
	e, _ := testEngine(t)
	p := sampleProgram()
	p.Project = "other"
	if _, err := e.Submit(p); err == nil {
		t.Fatal("accepted unauthorized project")
	}
	p = sampleProgram()
	p.Changes = map[string]string{"../outside": "bad"}
	if _, err := e.Submit(p); err == nil {
		t.Fatal("accepted traversal")
	}
}
func TestEngineDoesNotReturnClaimSecrets(t *testing.T) {
	e, _ := testEngine(t)
	run, err := e.Submit(sampleProgram())
	if err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(run)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "lease_digest") || strings.Contains(string(raw), "token") {
		t.Fatal("leaked execution claim details")
	}
}
func TestCancelSignalsActiveEngineOperation(t *testing.T) {
	e, _ := testEngine(t)
	run, err := e.Submit(sampleProgram())
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	e.activeMu.Lock()
	e.active = map[string]context.CancelFunc{run.ID: cancel}
	e.activeMu.Unlock()
	if err := e.Cancel(run.ID, run.Revision); err != nil {
		t.Fatal(err)
	}
	select {
	case <-ctx.Done():
	default:
		t.Fatal("active execution was not interrupted")
	}
	current, err := e.Get(run.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !current.Cancelled || current.Status != Cancelled {
		t.Fatalf("unexpected cancellation status: %+v", current)
	}
}

type repairAuthor struct {
	compute *fakeCompute
	calls   int
}

func (a *repairAuthor) Propose(_ context.Context, _ string, _ map[string]string, allowed []string) (map[string]string, error) {
	a.calls++
	if len(allowed) != 1 || allowed[0] != "README.md" {
		return nil, errors.New("unexpected editable scope")
	}
	a.compute.failCheck = false
	return map[string]string{"README.md": "Corrected after verification"}, nil
}
func TestAuthorizedRepairRerunsVerificationBeforePublication(t *testing.T) {
	e, compute := testEngine(t)
	compute.failCheck = true
	author := &repairAuthor{compute: compute}
	e.Author = author
	p := sampleProgram()
	p.AllowedPaths = []string{"README.md"}
	run, err := e.Submit(p)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 8; i++ {
		progressed, err := (&Runner{Store: e.Store, Worker: e, Lease: time.Minute}).RunOne(context.Background(), run.ID)
		if err != nil || !progressed {
			t.Fatalf("step %d: %v", i, err)
		}
	}
	final, err := e.Get(run.ID)
	if err != nil {
		t.Fatal(err)
	}
	if final.Status != Succeeded || compute.drafts != 1 || author.calls != 1 || compute.exported["artifact1"] != "Corrected after verification" {
		t.Fatalf("failed to export verified repair: status=%s drafts=%d calls=%d artifact=%q steps=%+v", final.Status, compute.drafts, author.calls, compute.exported["artifact1"], final.Steps)
	}
}
func TestUnauthorizedRepairCannotReachDraftPublication(t *testing.T) {
	e, compute := testEngine(t)
	compute.failCheck = true
	e.Author = untrustedAuthor{}
	p := sampleProgram()
	p.AllowedPaths = []string{"README.md"}
	run, err := e.Submit(p)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 8; i++ {
		progressed, err := (&Runner{Store: e.Store, Worker: e, Lease: time.Minute}).RunOne(context.Background(), run.ID)
		if err != nil {
			t.Fatal(err)
		}
		if !progressed {
			break
		}
	}
	final, err := e.Get(run.ID)
	if err != nil {
		t.Fatal(err)
	}
	if final.Status != Failed || compute.drafts != 0 {
		t.Fatalf("unauthorized repair reached publication: status=%s drafts=%d", final.Status, compute.drafts)
	}
}

func TestUnconfirmedSourceWriteCannotReachVerification(t *testing.T) {
	e, compute := testEngine(t)
	compute.mismatchWrite = true
	run, err := e.Submit(sampleProgram())
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 8; i++ {
		progressed, err := (&Runner{Store: e.Store, Worker: e, Lease: time.Minute}).RunOne(context.Background(), run.ID)
		if err != nil {
			t.Fatal(err)
		}
		if !progressed {
			break
		}
	}
	final, err := e.Get(run.ID)
	if err != nil {
		t.Fatal(err)
	}
	if final.Status != Failed || compute.drafts != 0 {
		t.Fatal("source digest mismatch bypassed verification boundary")
	}
}
func TestUnconfirmedCheckoutRevisionPreventsPublication(t *testing.T) {
	e, compute := testEngine(t)
	compute.mismatchRevision = true
	run, err := e.Submit(sampleProgram())
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 8; i++ {
		progressed, err := (&Runner{Store: e.Store, Worker: e, Lease: time.Minute}).RunOne(context.Background(), run.ID)
		if err != nil {
			t.Fatal(err)
		}
		if !progressed {
			break
		}
	}
	final, err := e.Get(run.ID)
	if err != nil {
		t.Fatal(err)
	}
	if final.Status != Failed || compute.drafts != 0 {
		t.Fatal("source revision mismatch bypassed verification boundary")
	}
}
func TestDraftPublicationRequiresExpectedRevisionProof(t *testing.T) {
	e, compute := testEngine(t)
	compute.mismatchDraft = true
	run, err := e.Submit(sampleProgram())
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 8; i++ {
		progressed, err := (&Runner{Store: e.Store, Worker: e, Lease: time.Minute}).RunOne(context.Background(), run.ID)
		if err != nil {
			t.Fatal(err)
		}
		if !progressed {
			break
		}
	}
	final, err := e.Get(run.ID)
	if err != nil {
		t.Fatal(err)
	}
	if final.Status != Failed || compute.drafts != 1 {
		t.Fatal("wrong revision publication was reported as successful")
	}
}

func TestFlatWorkerArtifactResponsePublishesVerifiedDraft(t *testing.T) {
	e, compute := testEngine(t)
	compute.flatExport = true
	created, err := e.Submit(sampleProgram())
	if err != nil {
		t.Fatal(err)
	}
	runner := Runner{Store: e.Store, Worker: e, Lease: time.Minute}
	for i := 0; i < 8; i++ {
		progress, err := runner.RunOne(context.Background(), created.ID)
		if err != nil || !progress {
			t.Fatalf("engineering step %d: progressed=%v err=%v", i, progress, err)
		}
	}
	complete, err := e.Get(created.ID)
	if err != nil || complete.Status != Succeeded || compute.drafts != 1 {
		t.Fatalf("verified draft was not published: run=%+v error=%v", complete, err)
	}
}

func TestStoredArtifactDigestMismatchBlocksPublication(t *testing.T) {
	e, compute := testEngine(t)
	compute.mismatchArtifact = true
	created, err := e.Submit(sampleProgram())
	if err != nil {
		t.Fatal(err)
	}
	runner := Runner{Store: e.Store, Worker: e, Lease: time.Minute}
	for i := 0; i < 8; i++ {
		advanced, err := runner.RunOne(context.Background(), created.ID)
		if err != nil {
			t.Fatal(err)
		}
		if !advanced {
			break
		}
	}
	failed, err := e.Get(created.ID)
	if err != nil || failed.Status != Failed || compute.drafts != 0 {
		t.Fatalf("unverified artifact could reach publication: run=%+v error=%v", failed, err)
	}
}

func TestVerificationRefusesUnexpectedSourceMutation(t *testing.T) {
	e, compute := testEngine(t)
	compute.mutateOnCheck = true
	created, err := e.Submit(sampleProgram())
	if err != nil { t.Fatal(err) }
	runner := Runner{Store:e.Store,Worker:e,Lease:time.Minute}
	for i:=0;i<8;i++ {
		advanced,err:=runner.RunOne(context.Background(),created.ID)
		if err!=nil {t.Fatal(err)}
		if !advanced {break}
	}
	run,err:=e.Get(created.ID)
	if err!=nil||run.Status!=Failed||compute.drafts!=0 {
		t.Fatalf("verification accepted modified source: run=%+v err=%v",run,err)
	}
}


func TestEngineeringAcceleratorAdmissionAndProvenance(t *testing.T) {
 p:=sampleProgram()
 p.Hardware=map[string]int{"gpu":1,"npu":1}
 p.MinGPUMemoryMB=8000
 p.Architecture="amd64"
 p.NodeID="gpu-worker"
 p.LinkedRepositories=nil
 compiled,err:=Compile(p)
 if err!=nil{t.Fatal(err)}
 if compiled.Inputs==nil||compiled.Inputs.Primary.Revision!=p.Revision||
  len(compiled.Inputs.ProgramSHA256)!=64 || compiled.Inputs.Hardware["npu"]!=1{
  t.Fatalf("missing program identity: %+v",compiled.Inputs)
 }
 expected:=shaHex(p.Changes["README.md"])
 if compiled.Inputs.ChangeSHA256["README.md"]!=expected{t.Fatal("source contents absent from provenance fingerprint")}
 var provision map[string]any
 if err:=json.Unmarshal(compiled.Steps[0].Input,&provision);err!=nil{t.Fatal(err)}
 if obj(provision["hardware"])["gpu"]!=float64(1) ||
  provision["min_gpu_memory_mb"]!=float64(8000) ||provision["node_id"]!="gpu-worker"{
  t.Fatalf("accelerator lease lost in compiled program: %+v",provision)
 }
 engine,compute:=testEngine(t)
 created,err:=engine.Submit(p)
 if err!=nil{t.Fatal(err)}
 runner:=&Runner{Store:engine.Store,Worker:engine,Lease:time.Minute}
 if _,err:=runner.RunOne(context.Background(),created.ID);err!=nil{t.Fatal(err)}
 if obj(compute.lastProvision["hardware"])["npu"]!=float64(1) ||
  compute.lastProvision["min_gpu_memory_mb"]!=float64(8000) {
  t.Fatalf("compute controller did not receive hardware admission: %+v",compute.lastProvision)
 }
 summary,err:=engine.Get(created.ID)
 if err!=nil||summary.Inputs==nil||summary.Inputs.ProgramSHA256==""{
  t.Fatalf("run did not retain source identity: %+v %v",summary,err)
 }
}
func TestEngineeringHardwareLimitsRejectNonFiniteAndUnapprovedDevices(t *testing.T) {
 for _,mutate:=range []func(*Program){
  func(p *Program){p.CPU=math.NaN()},
  func(p *Program){p.CPU=math.Inf(1)},
  func(p *Program){p.CPU=math.Inf(-1)},
  func(p *Program){p.Hardware=map[string]int{"fpga":1}},
  func(p *Program){p.Hardware=map[string]int{"gpu":3}},
  func(p *Program){p.Hardware=map[string]int{"npu":-1}},
  func(p *Program){p.Hardware=map[string]int{"npu":1};p.MinGPUMemoryMB=2048},
  func(p *Program){p.Architecture="unsupported"},
  func(p *Program){p.NodeID="../../other"},
 } {
  p:=sampleProgram();mutate(&p)
  if _,err:=Compile(p);err==nil {t.Fatalf("unsafe engineering request accepted: %+v",p)}
 }
}
