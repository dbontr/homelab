package privileged

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/dbontr/homelab-new/system/security"
)

func TestPrivilegedExecutionDistinguishesSubmissionFromVerifiedCompletion(t *testing.T) {
	authorization := &fakeAuth{decision: security.Authorization{Allowed: true}}
	releases := &fakeRelease{}
	journal := testExecutorAudit(t)
	var commands int
	executor, err := New(Config{
		Host: "saturn", Systemctl: "/usr/bin/systemctl",
		AllowedServices: []string{"homelab-platform.service"},
		AllowRelease: true, Auth: authorization, Releases: releases, Audit: journal,
		Run: func(context.Context, string, ...string) error {
			commands++
			return nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	releaseID := strings.Repeat("a", 48)
	for _, tc := range []struct {
		name, operation, arguments string
		state                      ExecutionState
		auditOutcome               string
	}{
		{"service command", "infrastructure__service_restart", `{"host":"saturn","service":"homelab-platform.service"}`, ExecutionAccepted, "accepted"},
		{"release activation", "infrastructure__release_activate", `{"host":"saturn","release_id":"` + releaseID + `"}`, ExecutionCompleted, "succeeded"},
		{"release reversion", "infrastructure__release_revert", `{"host":"saturn","release_id":"` + releaseID + `"}`, ExecutionCompleted, "succeeded"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			result, err := executor.Execute(context.Background(), tc.operation, "saturn", json.RawMessage(tc.arguments))
			if err != nil || !result.Allowed || result.State != tc.state {
				t.Fatalf("incorrect execution outcome: %+v, %v", result, err)
			}
			if len(result.AuditID) != 32 {
				t.Fatalf("missing audit correlation ID: %q", result.AuditID)
			}
			if _, err := hex.DecodeString(result.AuditID); err != nil {
				t.Fatalf("invalid audit identity: %v", err)
			}
			log, err := os.ReadFile(journal.path)
			if err != nil {
				t.Fatal(err)
			}
			lines := strings.Split(strings.TrimSpace(string(log)), "\n")
			var final auditEntry
			if err := json.Unmarshal([]byte(lines[len(lines)-1]), &final); err != nil {
				t.Fatal(err)
			}
			if final.ID != result.AuditID || final.Outcome != tc.auditOutcome {
				t.Fatalf("journal and execution disagree: %+v / %+v", final, result)
			}
		})
	}
	if commands != 1 || releases.calls != 2 {
		t.Fatal("the executor did not run only the configured handlers")
	}
}

type executionRoundTrip func(*http.Request) (*http.Response, error)

func (f executionRoundTrip) RoundTrip(r *http.Request) (*http.Response, error) {
	return f(r)
}

func TestExecutorClientRejectsAmbiguousSuccess(t *testing.T) {
	id := strings.Repeat("b", 32)
	for _, tc := range []struct {
		name, response string
		allowed        bool
		state          ExecutionState
		wantError      bool
	}{
		{"accepted", `{"allowed":true,"state":"accepted","audit_id":"` + id + `"}`, true, ExecutionAccepted, false},
		{"completed", `{"allowed":true,"state":"completed","audit_id":"` + id + `"}`, true, ExecutionCompleted, false},
		{"denied", `{"allowed":false,"reason":"confirmation required"}`, false, "", false},
		{"missing state", `{"allowed":true,"audit_id":"` + id + `"}`, false, "", true},
		{"missing audit ID", `{"allowed":true,"state":"accepted"}`, false, "", true},
		{"unknown state", `{"allowed":true,"state":"running","audit_id":"` + id + `"}`, false, "", true},
		{"bad audit ID", `{"allowed":true,"state":"accepted","audit_id":"wrong"}`, false, "", true},
        {"unrelated receipt", `{"allowed":true,"state":"accepted","audit_id":"` + strings.Repeat("c",32) + `"}`, false, "", true},
		{"contradictory denial", `{"allowed":false,"state":"completed","audit_id":"` + id + `"}`, false, "", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			client := &Client{http: &http.Client{
				Transport: executionRoundTrip(func(r *http.Request) (*http.Response, error) {
					if r.Method != http.MethodPost || r.URL.Path != "/v1/execute" {
						t.Fatal("invalid executor client request")
					}
					return &http.Response{
						StatusCode: http.StatusOK,
						Body: io.NopCloser(strings.NewReader(tc.response)),
						Header: make(http.Header),
					}, nil
				}),
			}}
			result, err := client.ExecuteWithID(context.Background(), id, "infrastructure__service_restart", "saturn", json.RawMessage(`{}`))
			if (err != nil) != tc.wantError {
				t.Fatalf("expected error=%t, got %+v, %v", tc.wantError, result, err)
			}
			if !tc.wantError && (result.Allowed != tc.allowed || result.State != tc.state) {
				t.Fatalf("unexpected client decision: %+v", result)
			}
		})
	}
}


func TestWorkspaceAndKubernetesSubmissionsDoNotClaimCompletion(t *testing.T) {
	var workspaceCalls, kubernetesCalls atomic.Int32
	workspace, done := testWorkspacePolicy(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		workspaceCalls.Add(1)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"jsonrpc":"2.0","id":1,"result":{"content":[]}}`))
	}))
	defer done()
	executor, err := New(Config{
		Host: "saturn", Systemctl: "/usr/bin/systemctl",
		Kubernetes: &KubernetesPolicy{
			Binary: "/usr/bin/kubectl",
			Kubeconfig: "/run/credentials/homelab-executord.service/kubeconfig",
			Namespaces: map[string][]string{"apps": {"worker"}},
			MaxReplicas: 3,
		},
		Workspace: workspace, Auth: &fakeAuth{decision: security.Authorization{Allowed: true}},
		Releases: &fakeRelease{}, Audit: testExecutorAudit(t),
		Run: func(_ context.Context, command string, args ...string) error {
			if command != "/usr/bin/kubectl" { t.Fatalf("unexpected executable %q", command) }
			kubernetesCalls.Add(1)
			return nil
		},
	})
	if err != nil { t.Fatal(err) }
	for _, tc := range []struct { operation, arguments string }{
		{"infrastructure__workspace_stop", `{"host":"saturn","project_id":"research","workspace_id":"ws1"}`},
		{"cluster__deployment_scale", `{"host":"saturn","namespace":"apps","deployment":"worker","replicas":2}`},
	} {
		result, err := executor.Execute(context.Background(), tc.operation, "saturn", json.RawMessage(tc.arguments))
		if err != nil || !result.Allowed || result.State != ExecutionAccepted || result.AuditID == "" {
			t.Fatalf("submitted %s reported a final state: %+v, %v", tc.operation, result, err)
		}
	}
	if workspaceCalls.Load() != 1 || kubernetesCalls.Load() != 1 {
		t.Fatalf("unexpected submitted operations: workspace %d, Kubernetes %d", workspaceCalls.Load(), kubernetesCalls.Load())
	}
}
