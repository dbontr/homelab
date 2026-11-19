package main

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

 "github.com/dbontr/homelab-new/system/security"
)

func newMigrationCLITest(t *testing.T, rejectCompat bool) (string, string, string, []byte) {
	t.Helper()
	root := t.TempDir()
	state := filepath.Join(root, "state")
	parent := filepath.Join(root, "snapshots")
	for _, dir := range []string{state, parent} {
		if err := os.Mkdir(dir, 0700); err != nil {
			t.Fatal(err)
		}
	}
	record := []byte(`{"version":1,"last_totp_step":12,"failures":0,"locked_until":"0001-01-01T00:00:00Z"}`)
	if err := os.WriteFile(filepath.Join(state, "authenticator.json"), record, 0600); err != nil {
		t.Fatal(err)
	}
	calls := filepath.Join(root, "calls")
	t.Setenv("MIGRATION_PREFLIGHT_TEST_CALLS", calls)
	compat := filepath.Join(root, "compat-preflight")
	goPre := filepath.Join(root, "go-preflight")
	compatScript := "#!/bin/sh\nprintf 'compat\\n' >> \"$MIGRATION_PREFLIGHT_TEST_CALLS\"\n"
	if rejectCompat {
		compatScript += "exit 21\n"
	}
	for path, script := range map[string]string{
		compat: compatScript,
		goPre:  "#!/bin/sh\nprintf 'go\\n' >> \"$MIGRATION_PREFLIGHT_TEST_CALLS\"\n",
	} {
		if err := os.WriteFile(path, []byte(script), 0700); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("HOMELAB_SECURITY_GO_PREFLIGHT", goPre)
	t.Setenv("HOMELAB_SECURITY_COMPAT_PREFLIGHT", compat)
	return state, filepath.Join(parent, "migration"), calls, record
}

func TestGoMigrationCLIRequiresBothPreflights(t *testing.T) {
	state, destination, calls, original := newMigrationCLITest(t, false)
	args := []string{"prepare", "--target", "go", "--state-dir", state, "--snapshot-dir", destination}
	if err := run(args); err != nil {
		t.Fatal(err)
	}
	logged, err := os.ReadFile(calls)
	if err != nil || string(logged) != "compat\ngo\n" {
		t.Fatalf("preflights not sequential and independent: %q, %v", logged, err)
	}
	raw, err := os.ReadFile(filepath.Join(destination, "authority-cutover.json"))
	if err != nil {
		t.Fatal(err)
	}
	var receipt struct {
		Version             int    `json:"version"`
		Target              string `json:"target"`
		AuthenticatorSHA256 string `json:"authenticator_sha256"`
	}
	if err := json.Unmarshal(raw, &receipt); err != nil ||
		receipt.Version != 1 || receipt.Target != "go" || len(receipt.AuthenticatorSHA256) != 64 {
		t.Fatalf("unexpected state-bound receipt: %+v, %v", receipt, err)
	}
	live, err := os.ReadFile(filepath.Join(state, "authenticator.json"))
	if err != nil || string(live) != string(original) {
		t.Fatal("migration changed live replay state")
	}
}

func TestGoMigrationCLIDeniesCompatibilityFailure(t *testing.T) {
	state, destination, calls, _ := newMigrationCLITest(t, true)
	args := []string{"prepare", "--target", "go", "--state-dir", state, "--snapshot-dir", destination}
	err := run(args)
	if err == nil || !strings.Contains(err.Error(), "preflight") {
		t.Fatalf("expected denied legacy preflight: %v", err)
	}
	logged, err := os.ReadFile(calls)
	if err != nil || string(logged) != "compat\n" {
		t.Fatalf("Go preflight ran after compatibility rejection: %q, %v", logged, err)
	}
	if _, err := os.Lstat(destination); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("failed preflight retained protected snapshot: %v", err)
	}
}

func TestGoMigrationCLIDeniesSamePreflightBinary(t *testing.T) {
	state, destination, _, _ := newMigrationCLITest(t, false)
	t.Setenv("HOMELAB_SECURITY_COMPAT_PREFLIGHT", os.Getenv("HOMELAB_SECURITY_GO_PREFLIGHT"))
	err := run([]string{"prepare", "--target", "go", "--state-dir", state, "--snapshot-dir", destination})
	if err == nil || !strings.Contains(err.Error(), "distinct") {
		t.Fatalf("same preflight binary accepted: %v", err)
	}
}

func TestRustMigrationCLIUsesSameStatePreservingPreflight(t *testing.T) {
 state, destination, calls, original := newMigrationCLITest(t, false)
 if err := run([]string{"prepare", "--target", "rust", "--state-dir", state, "--snapshot-dir", destination}); err != nil {
  t.Fatal(err)
 }
 logged,err:=os.ReadFile(calls)
 if err!=nil||string(logged)!="compat\ngo\n" {
  t.Fatalf("independent preflights did not both execute: %q, %v",logged,err)
 }
 raw,err:=os.ReadFile(filepath.Join(destination, "authority-cutover.json"))
 if err!=nil {t.Fatal(err)}
 var receipt struct {Target string `json:"target"`; AuthenticatorSHA256 string `json:"authenticator_sha256"`}
 if err:=json.Unmarshal(raw,&receipt);err!=nil||receipt.Target!="rust"||len(receipt.AuthenticatorSHA256)!=64 {
  t.Fatalf("invalid Rust recovery receipt: %+v, %v",receipt,err)
 }
 live,err:=os.ReadFile(filepath.Join(state,"authenticator.json"))
 if err!=nil||string(live)!=string(original){t.Fatal("Rust recovery reset authentication state")}
}

func TestRustRecoveryAfterGoSelectionPreservesCurrentAuthenticator(t *testing.T){
 state,first,_,_:=newMigrationCLITest(t,false)
 if err:=run([]string{"prepare","--target","go","--state-dir",state,"--snapshot-dir",first});err!=nil{t.Fatal(err)}
 goReceipt,err:=os.ReadFile(filepath.Join(first,"authority-cutover.json"))
 if err!=nil{t.Fatal(err)}
 if err:=security.AdmitMigration(state,"go",goReceipt);err!=nil{t.Fatal(err)}
 // A real login advances the protected TOTP replay record between release
 // selection and rollback. The recovery receipt must bind to that new state.
 advanced:=[]byte(`{"version":1,"last_totp_step":13,"failures":0,"locked_until":"0001-01-01T00:00:00Z"}`)
 if err:=os.WriteFile(filepath.Join(state,"authenticator.json"),advanced,0600);err!=nil{t.Fatal(err)}
 second:=filepath.Join(filepath.Dir(first),"rollback")
 if err:=run([]string{"prepare","--target","rust","--state-dir",state,"--snapshot-dir",second});err!=nil{t.Fatal(err)}
 receipt,err:=os.ReadFile(filepath.Join(second,"authority-cutover.json"))
 if err!=nil{t.Fatal(err)}
 var metadata struct{
  Target string `json:"target"`
  ParentMarkerSHA256 *string `json:"parent_marker_sha256"`
  AuthenticatorSHA256 string `json:"authenticator_sha256"`
 }
 if err:=json.Unmarshal(receipt,&metadata);err!=nil||metadata.Target!="rust"||metadata.ParentMarkerSHA256==nil{
  t.Fatalf("Rust rollback lost its Go parent selection: %+v %v",metadata,err)
 }
 if err:=security.AdmitMigration(state,"rust",receipt);err!=nil{t.Fatal(err)}
 final,err:=os.ReadFile(filepath.Join(state,"authenticator.json"))
 if err!=nil||string(final)!=string(advanced){t.Fatal("recovery rewound TOTP replay state")}
 if err:=security.AdmitMigration(state,"go",goReceipt);err==nil{
  t.Fatal("obsolete Go receipt replay succeeded after Rust recovery")
 }
}
