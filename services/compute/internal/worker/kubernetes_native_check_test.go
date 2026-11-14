package worker

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestNativeVerificationAdmission(t *testing.T) {
	revision := "0123456789abcdef0123456789abcdef01234567"
	for _, action := range []string{"go-test", "go-vet", "cargo-test", "cargo-check", "cargo-clippy"} {
		if !validVerification("dbontr/homelab", revision, action) {
			t.Fatalf("valid tool rejected: %s", action)
		}
	}
	for _, test := range []struct{ repository, revision, action string }{
		{"", revision, "go-test"},
		{"dbontr/../homelab", revision, "go-test"},
		{"dbontr/.hidden", revision, "go-test"},
		{"dbontr/homelab/other", revision, "go-test"},
		{"dbontr/homelab", "ABCDEF0123456789abcdef0123456789abcdef01", "go-test"},
		{"dbontr/homelab", revision[:39], "go-test"},
		{"dbontr/homelab", revision, "sh -c whoami"},
		{"dbontr/homelab", revision, "git-push"},
		{"dbontr/homelab", revision, "nix-switch"},
	} {
		if validVerification(test.repository, test.revision, test.action) {
			t.Fatalf("unsafe native verification request admitted: %+v", test)
		}
	}
}

func TestNativeVerifierRequiresDenyAllNamespacePolicy(t *testing.T) {
	policy := map[string]any{"spec": map[string]any{
		"podSelector": map[string]any{},
		"policyTypes": []any{"Ingress", "Egress"},
		"ingress": []any{},
		"egress": []any{},
	}}
	if !deniesNetwork(policy) {
		t.Fatal("explicit deny-all policy rejected")
	}
	for _, variant := range []map[string]any{
		{"spec": map[string]any{"podSelector": map[string]any{}, "policyTypes": []any{"Ingress"}}},
		{"spec": map[string]any{"podSelector": map[string]any{}, "policyTypes": []any{"Ingress", "Egress"}, "egress": []any{map[string]any{}}}},
		{"spec": map[string]any{"podSelector": map[string]any{"app": "limited"}, "policyTypes": []any{"Ingress", "Egress"}}},
		{"spec": map[string]any{"podSelector": map[string]any{}, "policyTypes": []any{"Ingress", "Egress"}, "ingress": "unknown"}},
		{},
	} {
		if deniesNetwork(variant) {
			t.Fatalf("unsafe network policy admitted: %+v", variant)
		}
	}
}

func TestNativeVerifierJobSeparatesCredentialsAndMutableSource(t *testing.T) {
	k := &Kubernetes{Project: "engineering", Worker: "jupiter",
		VerificationImage: "registry.invalid/homelab/verifier@sha256:" +
			"0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"}
	id := "0123456789abcdef0123456789abcdef"
	op := Operation{ID: id, Workspace: id, Action: "command"}
	manifest := k.verificationManifest(op, "dbontr/homelab", "0123456789abcdef0123456789abcdef01234567", "go-test", "jupiter")
	pod := kubeObject(kubeObject(kubeObject(manifest, "spec"), "template"), "spec")
	if pod["automountServiceAccountToken"] != false || pod["nodeName"] != "jupiter" {
		t.Fatal("verification Job has unsafe service-account or node selection")
	}
	if pod["serviceAccountName"] != "pwm-sandbox" {
		t.Fatal("verification Job has an unexpected service account")
	}
	containers, _ := pod["containers"].([]any)
	if len(containers) != 1 {
		t.Fatal("verification Job has unexpected containers")
	}
	container, _ := containers[0].(map[string]any)
	if container["image"] != k.VerificationImage || container["workingDir"] != "/workspace" {
		t.Fatal("verification Job has unexpected image or checkout")
	}
	security := kubeObject(container, "securityContext")
	if security["allowPrivilegeEscalation"] != false || security["readOnlyRootFilesystem"] != true {
		t.Fatal("verification Job must be non-privileged and read-only")
	}
	volumes, _ := pod["volumes"].([]any)
	if len(volumes) != 2 {
		t.Fatal("verification Job mounts unexpected volumes")
	}
	source, _ := volumes[0].(map[string]any)
	pvc := kubeObject(source, "persistentVolumeClaim")
	if pvc["readOnly"] != true || pvc["claimName"] != kubeClaimName(id) {
		t.Fatal("verification Job source volume is writable or unrelated")
	}
}

func TestNativeVerifierCleanupMustCompleteBeforeSuccess(t *testing.T) {
	// A local executable emulates Kubernetes replies, not a cluster API.
	binary := filepath.Join(t.TempDir(), "kubectl")
	mock := "#!/bin/sh\ncase \" $* \" in\n  *\" get networkpolicy pwm-isolate-workloads -o json \"*)\n    printf '%s\\n' '{\"spec\":{\"podSelector\":{},\"policyTypes\":[\"Ingress\",\"Egress\"],\"ingress\":[],\"egress\":[]}}' ;;\n  *\" get pod pwm-0123456789abcdef0123456789abcdef -o json \"*)\n    printf '%s\\n' '{\"metadata\":{\"labels\":{\"app.kubernetes.io/managed-by\":\"project-workspace\",\"pwm.dev/project\":\"engineering\",\"pwm.dev/worker\":\"jupiter\",\"pwm.dev/workspace\":\"0123456789abcdef0123456789abcdef\",\"pwm.dev/kind\":\"workspace\"}},\"spec\":{\"nodeName\":\"jupiter\"}}' ;;\n  *\" get jobs -l \"*)\n    printf '%s\\n' '{\"items\":[]}' ;;\n  *\" create -f - \"*)\n    cat >/dev/null ;;\n  *\" get job pwm-verify-0123456789abcdef0123456789abcdef -o json \"*)\n    printf '%s\\n' '{\"status\":{\"succeeded\":1}}' ;;\n  *\" logs job/pwm-verify-0123456789abcdef0123456789abcdef --all-containers=true \"*)\n    printf 'completed\\n' ;;\n  *\" delete job pwm-verify-0123456789abcdef0123456789abcdef \"*)\n    [ \"$FAIL_JOB_DELETE\" = \"0\" ] ;;\n  *)\n    printf 'unexpected mock kubectl invocation: %s\\n' \"$*\" >&2\n    exit 1 ;;\nesac\n"
	if err := os.WriteFile(binary, []byte(mock), 0700); err != nil {
		t.Fatal(err)
	}
	k := &Kubernetes{
		Namespace: "research", Project: "engineering", Worker: "jupiter",
		Binary: binary,
		VerificationImage: "registry.invalid/verifier@sha256:" + strings.Repeat("a", 64),
	}
	op := Operation{ID: "0123456789abcdef0123456789abcdef", Workspace: "0123456789abcdef0123456789abcdef", Action: "command"}
	payload := map[string]any{
		"sandbox_check": true, "repository": "dbontr/homelab",
		"revision": "0123456789abcdef0123456789abcdef01234567",
		"tool": "go-test",
	}
	t.Setenv("FAIL_JOB_DELETE", "0")
	success, err := k.VerifyNativeCheck(context.Background(), op, payload)
	if err != nil || success["stdout"] != "completed" {
		t.Fatalf("successful verification and deletion rejected: %v, %v", success, err)
	}

	t.Setenv("FAIL_JOB_DELETE", "1")
	unknown, err := k.VerifyNativeCheck(context.Background(), op, payload)
	if err == nil || unknown != nil || !strings.Contains(err.Error(), "cleanup is unconfirmed") {
		t.Fatalf("unconfirmed Job cleanup was reported as success: %v, %v", unknown, err)
	}
	if k.verificationReady() {
		t.Fatal("cleanup failure did not quarantine the worker")
	}
	_, err = k.Handle(context.Background(), nil, Operation{
		Workspace: op.Workspace, Action: "start",
	}, nil)
	if err == nil || !strings.Contains(err.Error(), "quarantined") {
		t.Fatalf("quarantined worker accepted a workspace operation: %v", err)
	}
	state, reason := k.ExecuteJob(context.Background(), nil, Job{})
	if state != "failed" || !strings.Contains(reason, "quarantined") {
		t.Fatalf("quarantined worker accepted an ordinary Job: %s %s", state, reason)
	}
	_, err = k.VerifyNativeCheck(context.Background(), op, payload)
	if err == nil || !strings.Contains(err.Error(), "quarantined") {
		t.Fatalf("quarantined worker accepted another verifier: %v", err)
	}
	t.Setenv("FAIL_JOB_DELETE", "0")
	if err := k.ReconcileStaleJobs(context.Background()); err != nil {
		t.Fatalf("verified Job cleanup could not clear quarantine: %v", err)
	}
	if !k.verificationReady() {
		t.Fatal("successful reconciliation left worker quarantined")
	}
	verified, err := k.VerifyNativeCheck(context.Background(), op, payload)
	if err != nil || verified["stdout"] != "completed" {
		t.Fatalf("reconciled worker cannot resume verification: %v, %v", verified, err)
	}
}
