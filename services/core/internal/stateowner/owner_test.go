package stateowner

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestLeaseRejectsCompetingWritersAndReleasesOnClose(t *testing.T) {
	path := filepath.Join(t.TempDir(), "tasks.json")
	first, err := Acquire(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Acquire(path); err == nil {
		t.Fatal("another writer acquired the same state")
	}
	if err := first.Close(); err != nil {
		t.Fatal(err)
	}
	second, err := Acquire(path)
	if err != nil {
		t.Fatalf("state remained locked after owner released it: %v", err)
	}
	if err := second.Close(); err != nil {
		t.Fatal(err)
	}
	if err := second.Close(); err != nil {
		t.Fatal("repeated Close must be safe", err)
	}
}

func TestFailedAcquisitionReleasesPreviouslyHeldResources(t *testing.T) {
	root := t.TempDir()
	owned, err := Acquire(filepath.Join(root, "z.json"))
	if err != nil {
		t.Fatal(err)
	}
	defer owned.Close()
	if _, err := Acquire(filepath.Join(root, "a.json"), filepath.Join(root, "z.json")); err == nil {
		t.Fatal("multiple resource acquisition ignored another writer")
	}
	other, err := Acquire(filepath.Join(root, "a.json"))
	if err != nil {
		t.Fatalf("partially acquired resource remained locked: %v", err)
	}
	defer other.Close()
}

func TestLockPermissionsAndSymlinkRejection(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "state.json")
	lease, err := Acquire(path)
	if err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path + ".lock")
	if err != nil || info.Mode().Perm() != 0600 {
		t.Fatalf("lock is not private: %v %v", info, err)
	}
	lease.Close()
	if err := os.Chmod(path+".lock", 0644); err != nil {
		t.Fatal(err)
	}
	if _, err := Acquire(path); err == nil {
		t.Fatal("insecure existing lock was accepted")
	}
	alias := filepath.Join(root, "alias.json")
	if err := os.Symlink(path, alias); err != nil {
		t.Fatal(err)
	}
	if _, err := Acquire(alias); err == nil {
		t.Fatal("symbolic-link state alias was accepted")
	}
}

func TestParentDirectoryAliasSharesOwnership(t *testing.T) {
	root := t.TempDir()
	realDir := filepath.Join(root, "real")
	if err := os.Mkdir(realDir, 0700); err != nil {
		t.Fatal(err)
	}
	alias := filepath.Join(root, "alias")
	if err := os.Symlink(realDir, alias); err != nil {
		t.Fatal(err)
	}
	first, err := Acquire(filepath.Join(realDir, "tasks.json"))
	if err != nil {
		t.Fatal(err)
	}
	defer first.Close()
	if _, err := Acquire(filepath.Join(alias, "tasks.json")); err == nil {
		t.Fatal("aliased directory bypassed the owner lock")
	}
}

func TestKernelReleasesLeaseAfterProcessExit(t *testing.T) {
	if os.Getenv("STATEOWNER_HELPER") == "lock" {
		lease, err := Acquire(os.Getenv("STATEOWNER_PATH"))
		if err != nil {
			os.Exit(2)
		}
		_ = lease
		os.Exit(0)
	}
	path := filepath.Join(t.TempDir(), "state.json")
	cmd := exec.Command(os.Args[0], "-test.run=^TestKernelReleasesLeaseAfterProcessExit$")
	cmd.Env = append(os.Environ(), "STATEOWNER_HELPER=lock", "STATEOWNER_PATH="+path)
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("child did not acquire lease: %v %s", err, output)
	}
	lease, err := Acquire(path)
	if err != nil {
		t.Fatalf("kernel retained dead process lease: %v", err)
	}
	defer lease.Close()
}
