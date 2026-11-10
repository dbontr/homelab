package agentflow

import "testing"

func TestNativeVerificationMapsOnlyApprovedChecks(t *testing.T) {
	allowed := map[string]string{
		"go test -count=1 ./...": "go-test",
		"go test ./...": "go-test",
		"go vet ./...": "go-vet",
		"cargo test --locked --offline --all-targets": "cargo-test",
		"cargo check --locked --offline": "cargo-check",
		"cargo clippy --locked --offline --all-targets -- -D warnings": "cargo-clippy",
	}
	for command, expected := range allowed {
		action, ok := nativeVerificationAction(command)
		if !ok || action != expected {
			t.Errorf("%q: got %q, %t; expected %q", command, action, ok, expected)
		}
	}
	for _, command := range []string{
		"sh -c 'go test ./...'", "go test ./...; echo unsafe",
		"git push origin main", "cargo test", "cargo test --online",
		"cd repo && go test ./...", "make deploy", "", " go test ./...",
		"rm -rf /", "go test ./... && true",
	} {
		if _, ok := nativeVerificationAction(command); ok {
			t.Errorf("unapproved native verification command %q admitted", command)
		}
	}
}

func TestNativeVerificationRejectsUnpinnedWorkflowWithoutCompute(t *testing.T) {
	engine := &Engine{NativeVerification: true}
	_, err := engine.verificationCommand(t.Context(), Workflow{}, "dbontr/homelab", "go test ./...")
	if err == nil {
		t.Fatal("native verification accepted a run without a source identity")
	}
}
