package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestSourceIdentities(t *testing.T) {
	if !lowerHex(strings.Repeat("a", 32), 32) ||
		!lowerHex(strings.Repeat("b", 40), 40, 64) {
		t.Fatal("valid identities rejected")
	}
	for _, bad := range []string{"../../etc", strings.Repeat("G", 40), strings.Repeat("a", 39)} {
		if lowerHex(bad, 32, 40, 64) {
			t.Fatalf("invalid identity accepted: %s", bad)
		}
	}
	for _, bad := range []string{"", ".", "..", "bad/", "../repo", ".git", "repo.", strings.Repeat("x", 101)} {
		if validRepoPart(bad) {
			t.Fatalf("invalid repository component accepted: %s", bad)
		}
	}
	if !validRepoPart("dbontr") || !validRepoPart("homelab-go") {
		t.Fatal("valid repository name rejected")
	}
}

func TestApprovedTools(t *testing.T) {
	for _, action := range []string{"go-test", "go-vet", "cargo-test", "cargo-check", "cargo-clippy"} {
		if _, err := containedTool(action); err != nil {
			t.Fatalf("approved action rejected: %s %v", action, err)
		}
	}
	for _, action := range []string{"sh", "git-push", "nix-activate", "kubectl", "", "../../bad"} {
		if _, err := containedTool(action); err == nil {
			t.Fatalf("unapproved action accepted: %q", action)
		}
	}
}

func TestWorkspaceRejectsInvalidIdentity(t *testing.T) {
	if _, err := workspace("../etc"); err == nil {
		t.Fatal("unsafe workspace identity accepted")
	}
	if _, err := containedCheckout("owner/../repo"); err == nil {
		t.Fatal("unsafe checkout accepted")
	}
}

func TestReceiptBindsSourceAndRevision(t *testing.T) {
	dir := t.TempDir()
	marker := filepath.Join(dir, ".pwm-source-revision")
	good := "owner/repository\n" + strings.Repeat("a", 40) + "\n"
	if err := os.WriteFile(marker, []byte(good), 0600); err != nil {
		t.Fatal(err)
	}
	if err := importReceipt(dir, "owner/repository", strings.Repeat("a", 40)); err != nil {
		t.Fatal(err)
	}
	if err := importReceipt(dir, "owner/other", strings.Repeat("a", 40)); err == nil {
		t.Fatal("mismatched repository receipt accepted")
	}
	if err := os.Remove(marker); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("other", marker); err != nil {
		t.Fatal(err)
	}
	if err := importReceipt(dir, "owner/repository", strings.Repeat("a", 40)); err == nil {
		t.Fatal("symlinked receipt accepted")
	}
}

func TestTimeoutBound(t *testing.T) {
	t.Setenv("HOMELAB_SANDBOX_TIMEOUT_SECONDS", "0")
	if timeoutFromEnv() != 300*time.Second {
		t.Fatal("unbounded timeout accepted")
	}
	t.Setenv("HOMELAB_SANDBOX_TIMEOUT_SECONDS", "1800")
	if timeoutFromEnv() != 1800*time.Second {
		t.Fatal("configured timeout rejected")
	}
	t.Setenv("HOMELAB_SANDBOX_TIMEOUT_SECONDS", "1801")
	if timeoutFromEnv() != 300*time.Second {
		t.Fatal("excessive timeout accepted")
	}
}
