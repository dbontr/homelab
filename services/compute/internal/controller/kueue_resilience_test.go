package controller

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"sync"
	"testing"
)

// A remote POST can commit even when its response is lost. This transport
// makes the failure deterministic without creating a Kubernetes resource.
type lostBatchCreateResponse struct {
	base http.RoundTripper
	mu sync.Mutex
	lost bool
}

func (t *lostBatchCreateResponse) RoundTrip(request *http.Request) (*http.Response, error) {
	response, err := t.base.RoundTrip(request)
	if err != nil { return response, err }
	if request.Method != http.MethodPost || request.URL.Path != "/apis/batch/v1/namespaces/pwm-batch/jobs" {
		return response, nil
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.lost { return response, nil }
	t.lost = true
	_ = response.Body.Close()
	return nil, errors.New("simulated lost Kubernetes create response")
}

func TestKueueLostCreateResponseReconcilesOneExecution(t *testing.T) {
	db, srv, _, fake := batchFixture(t)
	queued, err := db.QueueKueueJob("research", "lost-response-001", "echo persistent", testBatchImage, 0.2, 128)
	if err != nil { t.Fatal(err) }
	id := asString(queued["id"])
	transport := &lostBatchCreateResponse{base:srv.Kueue.client.Transport}
	srv.Kueue.client = &http.Client{Transport:transport}
	pending, err := db.pendingKueueJobs()
	if err != nil || len(pending) != 1 { t.Fatalf("pending: %v %v", pending, err) }
	if err := srv.Kueue.reconcile(context.Background(), db, pending[0]); err == nil || !strings.Contains(err.Error(), "lost Kubernetes") {
		t.Fatalf("expected the injected lost response, got %v", err)
	}
	pending, err = db.pendingKueueJobs()
	if err != nil || len(pending) != 1 || asInt(pending[0]["create_attempted"]) != 1 {
		t.Fatalf("outbound create was not fenced before network failure: %v %v", pending, err)
	}
	// A separate DB handle simulates a process restart against durable state.
	reopened, err := OpenDatabase(db.filename)
	if err != nil { t.Fatal(err) }
	defer reopened.Close()
	pending, err = reopened.pendingKueueJobs()
	if err != nil { t.Fatal(err) }
	if err := srv.Kueue.reconcile(context.Background(), reopened, pending[0]); err != nil { t.Fatal(err) }
	fake.mu.Lock()
	count := fake.created
	fake.complete = true
	fake.mu.Unlock()
	if count != 1 { t.Fatalf("Kubernetes created %d Jobs after response loss", count) }
	pending, err = reopened.pendingKueueJobs()
	if err != nil { t.Fatal(err) }
	if err := srv.Kueue.reconcile(context.Background(), reopened, pending[0]); err != nil { t.Fatal(err) }
	persisted, err := reopened.KueueJob("research", id)
	if err != nil || persisted["status"] != "succeeded" ||
		!strings.Contains(asString(persisted["result"]), "calculation-complete") {
		t.Fatalf("result missing across restart: %v %v", persisted, err)
	}
	if _, err := reopened.QueueKueueJob("research", "lost-response-001", "echo changed", testBatchImage, 0.2, 128); err == nil {
		t.Fatal("replay changed execution after a durable submission")
	}
}

func TestKueueInterruptedBeforeRemoteCreateRequiresRecovery(t *testing.T) {
	db, srv, _, fake := batchFixture(t)
	queued, err := db.QueueKueueJob("research", "outbound-crash-001", "echo never-duplicated", testBatchImage, 0.1, 64)
	if err != nil { t.Fatal(err) }
	id := asString(queued["id"])
	permitted, err := db.beginKueueSubmission(id)
	if err != nil || !permitted { t.Fatalf("could not record outbound attempt: %v %v", permitted, err) }
	permitted, err = db.beginKueueSubmission(id)
	if err != nil || permitted { t.Fatalf("outbound fence is not exclusive: %v %v", permitted, err) }
	reopened, err := OpenDatabase(db.filename)
	if err != nil { t.Fatal(err) }
	defer reopened.Close()
	for i := 0; i < 3; i++ {
		pending, err := reopened.pendingKueueJobs()
		if err != nil || len(pending) != 1 { t.Fatalf("missing recovery candidate: %v %v", pending, err) }
		if err := srv.Kueue.reconcile(context.Background(), reopened, pending[0]); err != nil { t.Fatal(err) }
	}
	record, err := reopened.KueueJob("research", id)
	if err != nil || record["status"] != "recovery_required" ||
		!strings.Contains(asString(record["last_error"]), "verify the execution outcome") {
		t.Fatalf("uncertain execution not visible: %v %v", record, err)
	}
	again, err := reopened.QueueKueueJob("research", "outbound-crash-001", "echo never-duplicated", testBatchImage, 0.1, 64)
	if err != nil || again["id"] != id || again["status"] != "recovery_required" {
		t.Fatalf("replay did not reuse the interrupted identity: %v %v", again, err)
	}
	fake.mu.Lock(); count := fake.created; fake.mu.Unlock()
	if count != 0 { t.Fatalf("uncertain execution was resubmitted: %d", count) }
	if _, err := reopened.CancelKueueJob("research", id); err != nil { t.Fatal(err) }
	pending, err := reopened.pendingKueueJobs()
	if err != nil { t.Fatal(err) }
	if err := srv.Kueue.reconcile(context.Background(), reopened, pending[0]); err != nil { t.Fatal(err) }
	final, err := reopened.KueueJob("research", id)
	if err != nil || final["status"] != "cancelled" ||
		!strings.Contains(asString(final["result"]), "unverified") {
		t.Fatalf("cancel hid uncertain execution history: %v %v", final, err)
	}
}

func TestKueueMissingObservedJobNeverCreatesReplacement(t *testing.T) {
	db, srv, _, fake := batchFixture(t)
	queued, err := db.QueueKueueJob("research", "missing-worker-001", "echo one", testBatchImage, 0.1, 64)
	if err != nil { t.Fatal(err) }
	pending, err := db.pendingKueueJobs()
	if err != nil { t.Fatal(err) }
	if err := srv.Kueue.reconcile(context.Background(), db, pending[0]); err != nil { t.Fatal(err) }
	fake.mu.Lock(); fake.job = nil; fake.mu.Unlock()
	pending, err = db.pendingKueueJobs()
	if err != nil { t.Fatal(err) }
	if err := srv.Kueue.reconcile(context.Background(), db, pending[0]); err != nil { t.Fatal(err) }
	for i := 0; i < 2; i++ {
		pending, err = db.pendingKueueJobs()
		if err != nil { t.Fatal(err) }
		if err := srv.Kueue.reconcile(context.Background(), db, pending[0]); err != nil { t.Fatal(err) }
	}
	record, err := db.KueueJob("research", asString(queued["id"]))
	if err != nil || record["status"] != "recovery_required" {
		t.Fatalf("missing execution not flagged: %v %v", record, err)
	}
	fake.mu.Lock(); count := fake.created; fake.mu.Unlock()
	if count != 1 { t.Fatalf("missing Job was silently replaced: %d", count) }
}

func TestKueueConcurrentReconcilersCreateAtMostOneJob(t *testing.T) {
	db, srv, _, fake := batchFixture(t)
	if _, err := db.QueueKueueJob("research", "parallel-requests-001", "echo once", testBatchImage, 0.25, 128); err != nil { t.Fatal(err) }
	pending, err := db.pendingKueueJobs()
	if err != nil || len(pending) != 1 { t.Fatalf("pending: %v %v", pending, err) }
	var group sync.WaitGroup
	for i := 0; i < 12; i++ {
		group.Add(1)
		go func() {
			defer group.Done()
			// An in-flight competing reconciliation may see a missing Job
			// before the owner has submitted it. Recovery remains safe.
			_ = srv.Kueue.reconcile(context.Background(), db, pending[0])
		}()
	}
	group.Wait()
	pending, err = db.pendingKueueJobs()
	if err != nil { t.Fatal(err) }
	if len(pending) == 1 {
		if err := srv.Kueue.reconcile(context.Background(), db, pending[0]); err != nil { t.Fatal(err) }
	}
	fake.mu.Lock(); count := fake.created; fake.mu.Unlock()
	if count != 1 { t.Fatalf("concurrent requests created %d remote Jobs", count) }
}

type suspendedKueueAdmission struct {
	base http.RoundTripper
	blocked bool
}

func (s *suspendedKueueAdmission) RoundTrip(r *http.Request) (*http.Response, error) {
	if s.blocked && strings.HasSuffix(r.URL.Path, "/workloads") {
		return &http.Response{
			StatusCode: http.StatusOK,
			Header: http.Header{"Content-Type": []string{"application/json"}},
			Body: io.NopCloser(strings.NewReader(`{"items":[]}`)),
			Request: r,
		}, nil
	}
	return s.base.RoundTrip(r)
}

func TestKueueQuotaBlockedAndWorkerRecovery(t *testing.T) {
	db, srv, _, fake := batchFixture(t)
	admission := &suspendedKueueAdmission{base: srv.Kueue.client.Transport, blocked: true}
	srv.Kueue.client = &http.Client{Transport: admission}
	queued, err := db.QueueKueueJob("research", "quota-blocked-001", "echo accepted", testBatchImage, 0.25, 128)
	if err != nil { t.Fatal(err) }
	id := asString(queued["id"])
	for i := 0; i < 3; i++ {
		pending, err := db.pendingKueueJobs()
		if err != nil || len(pending) != 1 { t.Fatalf("pending: %v %v", pending, err) }
		if err := srv.Kueue.reconcile(context.Background(), db, pending[0]); err != nil { t.Fatal(err) }
		job, err := db.KueueJob("research", id)
		if err != nil || job["status"] != "queued" { t.Fatalf("quota must block admission: %v %v", job, err) }
	}
	fake.mu.Lock(); count := fake.created; fake.mu.Unlock()
	if count != 1 { t.Fatalf("queue retries created %d Jobs", count) }
	// Simulate restored quota and an eligible worker, without using any
	// production Kubernetes controller or node.
	admission.blocked = false
	pending, err := db.pendingKueueJobs()
	if err != nil { t.Fatal(err) }
	if err := srv.Kueue.reconcile(context.Background(), db, pending[0]); err != nil { t.Fatal(err) }
	job, err := db.KueueJob("research", id)
	if err != nil || job["status"] != "admitted" { t.Fatalf("admission did not recover: %v %v", job, err) }
	fake.mu.Lock(); fake.active = true; fake.mu.Unlock()
	pending, _ = db.pendingKueueJobs()
	if err := srv.Kueue.reconcile(context.Background(), db, pending[0]); err != nil { t.Fatal(err) }
	job, _ = db.KueueJob("research", id)
	if job["status"] != "running" { t.Fatalf("worker did not start: %v", job) }
	fake.mu.Lock(); fake.complete = true; fake.mu.Unlock()
	pending, _ = db.pendingKueueJobs()
	if err := srv.Kueue.reconcile(context.Background(), db, pending[0]); err != nil { t.Fatal(err) }
	job, _ = db.KueueJob("research", id)
	if job["status"] != "succeeded" { t.Fatalf("worker recovery lost terminal result: %v", job) }
	fake.mu.Lock(); count = fake.created; fake.mu.Unlock()
	if count != 1 { t.Fatalf("worker recovery resubmitted Job %d times", count) }
}

func TestKueueRejectsCancellationWithoutRemoteIdentityMatch(t *testing.T) {
	db, srv, _, fake := batchFixture(t)
	queued, err := db.QueueKueueJob("research", "wrong-lease-001", "echo protected", testBatchImage, 0.1, 64)
	if err != nil { t.Fatal(err) }
	pending, err := db.pendingKueueJobs()
	if err != nil { t.Fatal(err) }
	if err := srv.Kueue.reconcile(context.Background(), db, pending[0]); err != nil { t.Fatal(err) }
	if _, err := db.CancelKueueJob("research", asString(queued["id"])); err != nil { t.Fatal(err) }
	fake.mu.Lock()
	decodeAnyObject(fake.job["metadata"])["uid"] = "different-execution-uid"
	fake.mu.Unlock()
	pending, err = db.pendingKueueJobs()
	if err != nil { t.Fatal(err) }
	if err := srv.Kueue.reconcile(context.Background(), db, pending[0]); err == nil ||
		!strings.Contains(err.Error(), "UID changed") {
		t.Fatalf("unowned Job deletion was not rejected: %v", err)
	}
	record, err := db.KueueJob("research", asString(queued["id"]))
	if err != nil || record["status"] != "cancel_requested" {
		t.Fatalf("bad lease caused premature cancellation: %v %v", record, err)
	}
	fake.mu.Lock(); stillPresent := fake.job != nil; fake.mu.Unlock()
	if !stillPresent { t.Fatal("deleted a Job with a foreign UID") }
}

type unavailableBatchLogs struct {
	base http.RoundTripper
	unavailable bool
}

func (t *unavailableBatchLogs) RoundTrip(r *http.Request) (*http.Response, error) {
	if t.unavailable && strings.HasSuffix(r.URL.Path, "/log") {
		return &http.Response{
			StatusCode: http.StatusServiceUnavailable,
			Header: http.Header{"Content-Type": []string{"application/json"}},
			Body: io.NopCloser(strings.NewReader("temporary Pod log outage")),
			Request: r,
		}, nil
	}
	return t.base.RoundTrip(r)
}

func TestKueueDoesNotSilentlyLoseTerminalOutput(t *testing.T) {
	db, srv, _, fake := batchFixture(t)
	request, err := db.QueueKueueJob("research", "missing-logs-001", "echo evidence", testBatchImage, 0.2, 128)
	if err != nil { t.Fatal(err) }
	id := asString(request["id"])
	pending, err := db.pendingKueueJobs()
	if err != nil { t.Fatal(err) }
	if err := srv.Kueue.reconcile(context.Background(), db, pending[0]); err != nil { t.Fatal(err) }
	fake.mu.Lock(); fake.complete = true; fake.mu.Unlock()
	logs := &unavailableBatchLogs{base: srv.Kueue.client.Transport, unavailable: true}
	srv.Kueue.client = &http.Client{Transport: logs}
	pending, err = db.pendingKueueJobs()
	if err != nil { t.Fatal(err) }
	if err := srv.Kueue.reconcile(context.Background(), db, pending[0]); err != nil { t.Fatal(err) }
	uncertain, err := db.KueueJob("research", id)
	if err != nil || uncertain["status"] != "recovery_required" ||
		!strings.Contains(asString(uncertain["last_error"]), "output is unavailable") {
		t.Fatalf("terminal status hid lost output: %v %v", uncertain, err)
	}
	reopened, err := OpenDatabase(db.filename)
	if err != nil { t.Fatal(err) }
	defer reopened.Close()
	logs.unavailable = false
	pending, err = reopened.pendingKueueJobs()
	if err != nil || len(pending) != 1 { t.Fatalf("unreconciled output disappeared: %v %v", pending, err) }
	if err := srv.Kueue.reconcile(context.Background(), reopened, pending[0]); err != nil { t.Fatal(err) }
	persisted, err := reopened.KueueJob("research", id)
	if err != nil || persisted["status"] != "succeeded" ||
		!strings.Contains(asString(persisted["result"]), "calculation-complete") {
		t.Fatalf("terminal stdout not recovered: %v %v", persisted, err)
	}
	fake.mu.Lock(); created := fake.created; fake.mu.Unlock()
	if created != 1 { t.Fatalf("log recovery started %d executions", created) }
}
