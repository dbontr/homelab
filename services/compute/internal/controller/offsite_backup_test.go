package controller

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func setupResticTest(t *testing.T) (ResticRepositoryConfig, string) {
	t.Helper()
	if _, err := exec.LookPath("restic"); err != nil {
		t.Skip("restic binary not installed")
	}
	root := t.TempDir()
	password := filepath.Join(root, "restic-password")
	if err := os.WriteFile(password, []byte("temporary-test-password-123456789\n"), 0600); err != nil {
		t.Fatal(err)
	}
	repo := filepath.Join(root, "encrypted")
	conf := ResticRepositoryConfig{Repository: repo, PasswordFile: password}
	cmd := conf.resticCommand(context.Background(), root, "init")
	if err := cmd.Run(); err != nil {
		t.Fatalf("restic init failed: %v", err)
	}
	return conf, root
}

func TestEncryptedOffsiteBackupRestoreAndVerification(t *testing.T) {
	conf, root := setupResticTest(t)
	db, err := OpenDatabase(filepath.Join(root, "source", "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.CreateProject("research", "Research"); err != nil {
		t.Fatal(err)
	}
	saved, err := db.ArtifactSave("research", "theorem.txt", []byte("original signed proof"))
	if err != nil {
		t.Fatal(err)
	}
	task, err := db.CreatePersonalTask("controller-owner", "Personal notebook", "Private task", []string{"research"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.SavePersonalTaskText("controller-owner", asString(task["id"]), "scratch.txt", "do not lose this work", 0); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	snapshot, err := db.BackupOffsite(ctx, conf)
	if err != nil || !resticSnapshotID.MatchString(snapshot) {
		t.Fatalf("encrypted backup failed: %q %v", snapshot, err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	recovered := filepath.Join(root, "independent-host", "state.db")
	if err := RestoreOffsite(ctx, conf, snapshot, recovered); err != nil {
		t.Fatal(err)
	}
	check, err := OpenDatabase(recovered)
	if err != nil {
		t.Fatal(err)
	}
	defer check.Close()
	a, err := check.ArtifactInfo("research", asString(saved["id"]))
	if err != nil {
		t.Fatal(err)
	}
	contents, err := os.ReadFile(check.artifactPath(asString(a["id"])))
	if err != nil || string(contents) != "original signed proof" {
		t.Fatalf("project artifact absent after offsite restore: %q %v", contents, err)
	}
	owner, err := check.PersonalTaskInfo("controller-owner", asString(task["id"]))
	if err != nil || owner["title"] != "Personal notebook" {
		t.Fatalf("personal task missing from offsite backup: %v %v", owner, err)
	}
	personal, err := check.PersonalTaskFileBase64("controller-owner", asString(task["id"]), "scratch.txt", 1)
	if err != nil || personal["sha256"] != checksum([]byte("do not lose this work")) {
		t.Fatalf("personal task bytes missing: %v %v", personal, err)
	}
	if err := VerifyOffsite(ctx, conf, snapshot); err != nil {
		t.Fatalf("full remote read and recovery verification: %v", err)
	}
	if err := RestoreOffsite(ctx, conf, snapshot, recovered); err == nil {
		t.Fatal("offsite restore cannot overwrite a populated database")
	}
	if err := RestoreOffsite(ctx, conf, "latest", filepath.Join(root, "unsafe", "state.db")); err == nil {
		t.Fatal("implicit latest snapshot must never be accepted")
	}
}

func TestResticCredentialsAndMissingBackend(t *testing.T) {
	password := filepath.Join(t.TempDir(), "password")
	if err := os.WriteFile(password, []byte("secret"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(password, 0644); err != nil {
		t.Fatal(err)
	}
	cfg := ResticRepositoryConfig{Repository: "/tmp/unused", PasswordFile: password}
	if err := cfg.Validate(); err == nil || !strings.Contains(err.Error(), "owner-only") {
		t.Fatalf("world-readable Restic secret must be refused: %v", err)
	}
	if err := os.Chmod(password, 0600); err != nil {
		t.Fatal(err)
	}
	cfg.PasswordFile = "./password"
	if err := cfg.Validate(); err == nil {
		t.Fatal("relative password location should fail")
	}
	cfg.PasswordFile = password
	cfg.Repository = ""
	if err := cfg.Validate(); err == nil {
		t.Fatal("missing repository should fail closed")
	}
	if err := cfg.run(context.Background(), "", "check"); err == nil {
		t.Fatal("repository command must fail without a configured backend")
	}
}

func TestOffsiteRetentionPreservesIndependentSnapshots(t *testing.T) {
	conf, root := setupResticTest(t)
	db, err := OpenDatabase(filepath.Join(root, "source", "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.CreateProject("research", "Research"); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	var snapshots []string
	for i := 0; i < 3; i++ {
		id, err := db.BackupOffsite(ctx, conf)
		if err != nil {
			t.Fatal(err)
		}
		snapshots = append(snapshots, id)
	}
	unrelated := filepath.Join(root, "unrelated.txt")
	if err := os.WriteFile(unrelated, []byte("unrelated backup"), 0600); err != nil {
		t.Fatal(err)
	}
	external := conf.resticCommand(ctx, root, "backup", "--tag", "independent", unrelated)
	if err := external.Run(); err != nil {
		t.Fatalf("independent backup: %v", err)
	}
	if err := PruneOffsite(ctx, conf); err != nil {
		t.Fatal(err)
	}
	cmd := conf.resticCommand(ctx, root, "snapshots", "--json")
	out, err := cmd.Output()
	if err != nil {
		t.Fatal(err)
	}
	var list []struct {
		ID   string   `json:"id"`
		Tags []string `json:"tags"`
	}
	if err := json.Unmarshal(out, &list); err != nil {
		t.Fatal(err)
	}
	keptCompute := map[string]bool{}
	independent := 0
	for _, snapshot := range list {
		for _, tag := range snapshot.Tags {
			if tag == "project-workspace" {
				keptCompute[snapshot.ID] = true
			}
			if tag == "independent" {
				independent++
			}
		}
	}
	// Restic policies preserve the two most recent Compute snapshots.
	// Older snapshots can also be retained by calendar policies.
	if !keptCompute[snapshots[1]] || !keptCompute[snapshots[2]] || independent != 1 {
		t.Fatalf("retention lost recovery points: compute=%v independent=%d", keptCompute, independent)
	}
}

func TestRepositoryOutageDoesNotDestroyExistingEncryptedRecovery(t *testing.T) {
	conf, root := setupResticTest(t)
	db, err := OpenDatabase(filepath.Join(root, "source", "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.CreateProject("research", "Research"); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ArtifactSave("research", "result.txt", []byte("independent recovery")); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	baseline, err := db.BackupOffsite(ctx, conf)
	if err != nil {
		t.Fatal(err)
	}
	unavailable := conf.Repository + ".offline"
	if err := os.Rename(conf.Repository, unavailable); err != nil {
		t.Fatal(err)
	}
	if _, err := db.BackupOffsite(ctx, conf); err == nil {
		t.Fatal("unavailable destination must not produce a successful receipt")
	}
	if err := os.Rename(unavailable, conf.Repository); err != nil {
		t.Fatal(err)
	}
	if err := VerifyOffsite(ctx, conf, baseline); err != nil {
		t.Fatalf("previous encrypted snapshot must survive destination outage: %v", err)
	}
}

func TestInterruptedOffsiteBackupPreservesPreviousSnapshot(t *testing.T) {
	conf, root := setupResticTest(t)
	db, err := OpenDatabase(filepath.Join(root, "source", "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.CreateProject("research", "Research"); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	baseline, err := db.BackupOffsite(ctx, conf)
	if err != nil {
		t.Fatal(err)
	}
	aborted, abort := context.WithCancel(context.Background())
	abort()
	if _, err := db.BackupOffsite(aborted, conf); err == nil {
		t.Fatal("cancelled backup must not produce a successful receipt")
	}
	if err := VerifyOffsite(ctx, conf, baseline); err != nil {
		t.Fatalf("cancelled attempt lost previously verified snapshot: %v", err)
	}
}

func TestEncryptedRepositoryFullCheckDetectsSimulatedPackCorruption(t *testing.T) {
	conf, root := setupResticTest(t)
	db, err := OpenDatabase(filepath.Join(root, "source", "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.CreateProject("research", "Research"); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ArtifactSave("research", "result.txt", []byte("protected artifact")); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	if _, err := db.BackupOffsite(ctx, conf); err != nil {
		t.Fatal(err)
	}
	var pack string
	if err := filepath.WalkDir(filepath.Join(conf.Repository, "data"), func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.Type().IsRegular() && pack == "" {
			pack = path
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if pack == "" {
		t.Fatal("encrypted Restic repository contains no data packs")
	}
	data, err := os.ReadFile(pack)
	if err != nil {
		t.Fatal(err)
	}
	if len(data) < 128 {
		t.Fatalf("data pack unexpectedly small: %d", len(data))
	}
	data[len(data)/2] ^= 0x80
	// Restic makes pack files read-only; this disposable fixture must
	// explicitly permit controlled corruption before editing ciphertext.
	if err := os.Chmod(pack, 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(pack, data, 0600); err != nil {
		t.Fatal(err)
	}
	if err := FullCheckOffsite(ctx, conf); err == nil {
		t.Fatal("restic check --read-data must detect altered encrypted data")
	}
}

func TestExactSnapshotIDRejectsPrefix(t *testing.T) {
	conf, root := setupResticTest(t)
	if err := RestoreOffsite(context.Background(), conf, "12345678", filepath.Join(root, "restored", "state.db")); err == nil {
		t.Fatal("a truncated snapshot prefix must not be considered an exact recovery identity")
	}
}

func TestResticUsesPinnedSFTPIdentity(t *testing.T) {
	conf, root := setupResticTest(t)
	config := filepath.Join(root, "ssh_config")
	if err := os.WriteFile(config, []byte("Host *\n  BatchMode yes\n"), 0600); err != nil {
		t.Fatal(err)
	}
	conf.Repository = "sftp:compute-backup@10.77.30.232:/repository"
	conf.SFTPSSHConfig = config
	if err := conf.Validate(); err != nil {
		t.Fatal(err)
	}
	cmd := conf.resticCommand(context.Background(), root, "check", "--read-data")
	if len(cmd.Args) != 5 || cmd.Args[1] != "-o" ||
		cmd.Args[2] != "sftp.command=ssh -F "+config+" -s compute-backup@10.77.30.232 sftp" ||
		cmd.Args[3] != "check" || cmd.Args[4] != "--read-data" {
		t.Fatalf("Restic did not use the pinned SFTP command: %q", cmd.Args)
	}
	conf.Repository = "sftp:compute-backup@10.77.30.232;echo-danger:/repository"
	if err := conf.Validate(); err == nil {
		t.Fatal("invalid SFTP host must be rejected")
	}
	conf.Repository = "sftp:compute-backup@10.77.30.232:/repository"
	if err := os.Chmod(config, 0644); err != nil {
		t.Fatal(err)
	}
	if err := conf.Validate(); err == nil {
		t.Fatal("world-readable SSH config must be rejected")
	}
	if err := os.Chmod(config, 0600); err != nil {
		t.Fatal(err)
	}
	conf.SFTPSSHConfig = "/tmp/.ssh/config;touch /tmp/unsafe"
	if err := conf.Validate(); err == nil {
		t.Fatal("shell metacharacters in SSH config path must be rejected")
	}
	conf.SFTPSSHConfig = config
	conf.Repository = filepath.Join(root, "encrypted")
	if err := conf.Validate(); err == nil {
		t.Fatal("pinned SFTP identity must not be used with local backend")
	}
}

func TestResticSFTPConfigFromEnvironment(t *testing.T) {
	t.Setenv("PWM_RESTIC_SFTP_SSH_CONFIG", "/tmp/.ssh/config")
	conf := ResticConfigFromEnv()
	if conf.SFTPSSHConfig != "/tmp/.ssh/config" {
		t.Fatalf("SFTP config source not retained: %q", conf.SFTPSSHConfig)
	}
}

func TestResticJSONShortSummaryResolvesExactSnapshot(t *testing.T) {
	conf, root := setupResticTest(t)
	stage := filepath.Join(root, "stage")
	source := filepath.Join(stage, "snapshot")
	if err := os.MkdirAll(source, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(source, "check.txt"), []byte("short JSON summary identity"), 0600); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	command := conf.resticCommand(ctx, stage, "backup", "--json", "--tag", "project-workspace", "snapshot")
	output, err := command.Output()
	if err != nil {
		t.Fatalf("fixture backup: %v", err)
	}
	var reported string
	for _, line := range bytes.Split(output, []byte("\n")) {
		var event struct {
			MessageType string `json:"message_type"`
			SnapshotID  string `json:"snapshot_id"`
		}
		if json.Unmarshal(line, &event) == nil && event.MessageType == "summary" {
			reported = event.SnapshotID
		}
	}
	if !resticShortSnapshotID.MatchString(reported) && !resticSnapshotID.MatchString(reported) {
		t.Fatalf("unexpected Restic summary identifier length %d", len(reported))
	}
	id, err := conf.resolveResticSummaryID(ctx, stage, reported)
	if err != nil || !resticSnapshotID.MatchString(id) || !strings.HasPrefix(id, reported) {
		t.Fatalf("failed to confirm exact archived snapshot: %v", err)
	}
	if len(reported) == 8 {
		if _, err := conf.resolveResticSummaryID(ctx, root, reported); err == nil {
			t.Fatal("abbreviated ID must not resolve another stage path")
		}
	}
	if _, err := conf.resolveResticSummaryID(ctx, stage, "00000000"); err == nil {
		t.Fatal("unknown summary ID must fail closed")
	}
}
