package security

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"syscall"
	"strings"
)

const migrationSnapshotReceipt = "authority-cutover.json"

// PrepareMigration captures the current security state and emits a receipt for
// the target implementation. The caller must hold .securityd.lock throughout
// this function and must verify that the active authority has stopped.
// A failed preflight never produces a usable receipt or changes live state.
func PrepareMigration(stateDir, snapshotDir, target string, preflight func(string) error) error {
	if (target != "go" && target != "rust") || preflight == nil ||
		!filepath.IsAbs(stateDir) || filepath.Clean(stateDir) != stateDir ||
		!filepath.IsAbs(snapshotDir) || filepath.Clean(snapshotDir) != snapshotDir ||
		stateDir == snapshotDir ||
		pathWithin(stateDir, snapshotDir) || pathWithin(snapshotDir, stateDir) {
		return errors.New("invalid security migration path or target")
	}
	parent := filepath.Dir(snapshotDir)
	parentInfo, err := os.Lstat(parent)
	if err != nil || !parentInfo.IsDir() || parentInfo.Mode()&os.ModeSymlink != 0 ||
		parentInfo.Mode().Perm()&0022 != 0 {
		return errors.New("unsafe migration snapshot parent")
	}
	canonical, err := filepath.EvalSymlinks(parent)
	if err != nil || canonical != parent {
		return errors.New("migration snapshot parent must be canonical")
	}
	parentStat, ok := parentInfo.Sys().(*syscall.Stat_t)
	if !ok || parentStat.Uid != uint32(os.Geteuid()) {
		return errors.New("migration snapshot parent has incorrect owner")
	}
	stateInfo, err := os.Lstat(stateDir)
	if err != nil || !stateInfo.IsDir() || stateInfo.Mode()&os.ModeSymlink != 0 ||
		stateInfo.Mode().Perm()&0077 != 0 {
		return errors.New("invalid protected security state directory")
	}
	stateStat, ok := stateInfo.Sys().(*syscall.Stat_t)
	if !ok || stateStat.Uid != uint32(os.Geteuid()) {
		return errors.New("security state must be owned by its writer")
	}
	auth, exists, err := protectedMigrationFile(filepath.Join(stateDir, "authenticator.json"), migrationMaxAuth)
	if err != nil || !exists {
		return errors.New("protected authenticator state unavailable")
	}
	var replay authenticatorState
	if err := migrationJSON(auth, &replay); err != nil ||
		replay.Version != 1 || replay.LastTOTPStep < -1 ||
		replay.Failures < 0 || replay.Failures > 4 {
		return errors.New("incompatible authenticator replay state")
	}
	vault, hasVault, err := protectedMigrationFile(filepath.Join(stateDir, "credentials.json"), migrationMaxVault)
	if err != nil {
		return errors.New("protected encrypted vault unavailable")
	}
	_, parentHash, err := migrationMarkerRead(stateDir)
	if err != nil {
		return fmt.Errorf("invalid authority selection marker: %w", err)
	}
	if err := os.Mkdir(snapshotDir, 0700); err != nil {
		return fmt.Errorf("create exclusive migration snapshot: %w", err)
	}
	success := false
	defer func() {
		if !success {
			_ = os.RemoveAll(snapshotDir)
			_ = syncMigrationDir(parent)
		}
	}()
	if err := writeMigrationFile(filepath.Join(snapshotDir, "authenticator.json"), auth); err != nil {
		return err
	}
	if hasVault {
		if err := writeMigrationFile(filepath.Join(snapshotDir, "credentials.json"), vault); err != nil {
			return err
		}
	}
	if err := syncMigrationDir(snapshotDir); err != nil {
		return err
	}
	if err := preflight(snapshotDir); err != nil {
		return fmt.Errorf("security migration preflight failed: %w", err)
	}
	nonce := make([]byte, 16)
	if _, err := rand.Read(nonce); err != nil {
		return err
	}
	receipt := migrationReceipt{
		Version: 1,
		Target: target,
		AuthenticatorSHA256: migrationSHA(auth),
		ParentMarkerSHA256: parentHash,
		Nonce: hex.EncodeToString(nonce),
	}
	if hasVault {
		digest := migrationSHA(vault)
		receipt.VaultSHA256 = &digest
	}
	raw, err := json.Marshal(receipt)
	if err != nil || len(raw) > migrationMaxReceipt {
		return errors.New("cannot encode bounded migration receipt")
	}
	if err := writeMigrationFile(filepath.Join(snapshotDir, migrationSnapshotReceipt), raw); err != nil {
		return err
	}
	if err := syncMigrationDir(snapshotDir); err != nil {
		return err
	}
	if err := syncMigrationDir(parent); err != nil {
		return err
	}
	success = true
	return nil
}

func pathWithin(base, path string) bool {
	rel, err := filepath.Rel(base, path)
	return err == nil && rel != "." && rel != ".." && !filepath.IsAbs(rel) &&
		!strings.HasPrefix(rel, ".."+string(os.PathSeparator))
}

func writeMigrationFile(path string, value []byte) error {
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL|syscall.O_NOFOLLOW|syscall.O_CLOEXEC, 0600)
	if err != nil {
		return err
	}
	if _, err := f.Write(value); err != nil {
		f.Close()
		return err
	}
	if err := f.Sync(); err != nil {
		f.Close()
		return err
	}
	return f.Close()
}

func syncMigrationDir(path string) error {
	dir, err := os.Open(path)
	if err != nil {
		return err
	}
	defer dir.Close()
	return dir.Sync()
}
