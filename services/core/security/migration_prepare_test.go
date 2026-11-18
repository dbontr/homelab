package security

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func migrationSnapshotDir(t *testing.T) string {
	t.Helper()
	parent := filepath.Join(t.TempDir(), "snapshots")
	if err := os.Mkdir(parent, 0700); err != nil {
		t.Fatal(err)
	}
	return filepath.Join(parent, "migration")
}

func TestPrepareMigrationPreservesLiveStateAndBindsGoReceipt(t *testing.T) {
	state := migrationTestState(t)
	auth, err := os.ReadFile(filepath.Join(state, "authenticator.json"))
	if err != nil {
		t.Fatal(err)
	}
	vault := []byte(`{"encrypted_fixture":"no-plaintext"}`)
	if err := os.WriteFile(filepath.Join(state, "credentials.json"), vault, 0600); err != nil {
		t.Fatal(err)
	}
	initial := migrationTestReceipt(t, state, "rust", nil)
	if err := AdmitMigration(state, "rust", initial); err != nil {
		t.Fatal(err)
	}
	_, parent, err := migrationMarkerRead(state)
	if err != nil || parent == nil {
		t.Fatal("missing parent authority selection", err)
	}
	destination := migrationSnapshotDir(t)
	preflightCalls := 0
	err = PrepareMigration(state, destination, "go", func(copyDir string) error {
		preflightCalls++
		gotAuth, err := os.ReadFile(filepath.Join(copyDir, "authenticator.json"))
		if err != nil || !bytes.Equal(gotAuth, auth) {
			t.Fatal("state snapshot did not preserve authenticator")
		}
		gotVault, err := os.ReadFile(filepath.Join(copyDir, "credentials.json"))
		if err != nil || !bytes.Equal(gotVault, vault) {
			t.Fatal("state snapshot did not preserve encrypted vault")
		}
		receipt := filepath.Join(copyDir, migrationSnapshotReceipt)
		if _, err := os.Lstat(receipt); !errors.Is(err, os.ErrNotExist) {
			t.Fatal("receipt existed before preflight", err)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if preflightCalls != 1 {
		t.Fatalf("expected one preflight, got %d", preflightCalls)
	}
	raw, err := os.ReadFile(filepath.Join(destination, migrationSnapshotReceipt))
	if err != nil {
		t.Fatal(err)
	}
	receipt, err := migrationReceiptCheck(raw, "go")
	if err != nil || receipt.ParentMarkerSHA256 == nil || *receipt.ParentMarkerSHA256 != *parent ||
		receipt.AuthenticatorSHA256 != migrationSHA(auth) ||
		receipt.VaultSHA256 == nil || *receipt.VaultSHA256 != migrationSHA(vault) {
		t.Fatalf("receipt does not bind exact live state: %+v, %v", receipt, err)
	}
	if err := AdmitMigration(state, "go", raw); err != nil {
		t.Fatal("Go authority rejected prepared receipt:", err)
	}
	unchangedAuth, err := os.ReadFile(filepath.Join(state, "authenticator.json"))
	if err != nil || !bytes.Equal(unchangedAuth, auth) {
		t.Fatal("migration modified live replay state")
	}
	unchangedVault, err := os.ReadFile(filepath.Join(state, "credentials.json"))
	if err != nil || !bytes.Equal(unchangedVault, vault) {
		t.Fatal("migration modified live vault")
	}
}

func TestPrepareMigrationFailedPreflightRemovesPrivateCopy(t *testing.T) {
	state := migrationTestState(t)
	destination := migrationSnapshotDir(t)
	sentinel := errors.New("incompatible offline policy")
	if err := PrepareMigration(state, destination, "go", func(string) error {
		return sentinel
	}); !errors.Is(err, sentinel) {
		t.Fatalf("expected preflight rejection, got %v", err)
	}
	if _, err := os.Lstat(destination); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("unverified copy retained: %v", err)
	}
	if _, err := os.Stat(filepath.Join(state, "authenticator.json")); err != nil {
		t.Fatal("live authentication record changed:", err)
	}
}

func TestPrepareMigrationRejectsInvalidReplayAndUnsafeFiles(t *testing.T) {
	state := migrationTestState(t)
	destination := migrationSnapshotDir(t)
	if err := os.WriteFile(filepath.Join(state, "authenticator.json"), []byte(`{"version":9}`), 0600); err != nil {
		t.Fatal(err)
	}
	if err := PrepareMigration(state, destination, "go", func(string) error {
		t.Fatal("invalid state reached preflight")
		return nil
	}); err == nil {
		t.Fatal("invalid replay state prepared")
	}
	if _, err := os.Lstat(destination); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("invalid state created a snapshot")
	}
	if err := os.WriteFile(filepath.Join(state, "authenticator.json"), []byte(`{"version":1,"last_totp_step":0,"failures":0,"locked_until":"0001-01-01T00:00:00Z"}`), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Link(filepath.Join(state, "authenticator.json"), filepath.Join(state, "other-link")); err != nil {
		t.Fatal(err)
	}
	if err := PrepareMigration(state, destination, "go", func(string) error { return nil }); err == nil {
		t.Fatal("hardlinked protected state was accepted")
	}
}

func TestPrepareMigrationRejectsSnapshotInsideStateOrUntrustedParent(t *testing.T) {
	state := migrationTestState(t)
	destination := filepath.Join(state, "nested")
	if err := PrepareMigration(state, destination, "go", func(string) error { return nil }); err == nil {
		t.Fatal("snapshot within live security state accepted")
	}
	parent := t.TempDir()
	if err := os.Chmod(parent, 0777); err != nil {
		t.Fatal(err)
	}
	if err := PrepareMigration(state, filepath.Join(parent, "snapshot"), "go",
		func(string) error { return nil }); err == nil {
		t.Fatal("world-writable parent accepted")
	}
	if err := PrepareMigration(state, migrationSnapshotDir(t), "rust",
		func(string) error { return errors.New("required failure") }); err == nil ||
		!strings.Contains(err.Error(), "preflight") {
		t.Fatal("expected target preflight rejection:", err)
	}
}

func TestPrepareMigrationReceiptJSONUsesExactVersionedFields(t *testing.T) {
	state := migrationTestState(t)
	destination := migrationSnapshotDir(t)
	if err := PrepareMigration(state, destination, "go", func(string) error { return nil }); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(filepath.Join(destination, migrationSnapshotReceipt))
	if err != nil {
		t.Fatal(err)
	}
	fields := map[string]json.RawMessage{}
	if err := json.Unmarshal(raw, &fields); err != nil {
		t.Fatal(err)
	}
	if len(fields) != 6 || string(fields["version"]) != "1" ||
		string(fields["target"]) != `"go"` {
		t.Fatal("receipt schema diverged from authority gate")
	}
}
