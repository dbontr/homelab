package worker

import (
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
