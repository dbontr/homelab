package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"time"

	"github.com/dbontr/homelab-new/system/security"
	"github.com/dbontr/homelab-new/system/statelease"
)

func run(args []string) error {
	if len(args) != 7 || args[0] != "prepare" ||
		args[1] != "--target" || (args[2] != "go" && args[2] != "rust") ||
		args[3] != "--state-dir" || args[5] != "--snapshot-dir" {
		return errors.New("usage: homelab-security-migrate prepare --target go|rust --state-dir ABSOLUTE_STATE_DIR --snapshot-dir ABSOLUTE_NEW_DIR")
	}
	stateDir, snapshotDir := args[4], args[6]
	if !filepath.IsAbs(stateDir) || !filepath.IsAbs(snapshotDir) ||
		filepath.Clean(stateDir) != stateDir || filepath.Clean(snapshotDir) != snapshotDir {
		return errors.New("migration paths must be canonical and absolute")
	}
	preflight, err := checkedPreflight("HOMELAB_SECURITY_GO_PREFLIGHT")
	if err != nil {
		return err
	}
	compat, err := checkedPreflight("HOMELAB_SECURITY_COMPAT_PREFLIGHT")
	if err != nil {
		return err
	}
	if preflight == compat {
		return errors.New("independent security preflights must use distinct binaries")
	}
	lease, err := statelease.Acquire(stateDir, "securityd")
	if err != nil {
		return fmt.Errorf("security authority still owns the state lease: %w", err)
	}
	defer lease.Close()
	return security.PrepareMigration(stateDir, snapshotDir, args[2], func(copyDir string) error {
		for _, binary := range []string{compat, preflight} {
			ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
			command := exec.CommandContext(ctx, binary, "--state-dir", copyDir)
			command.Env = os.Environ()
			command.Stdout = os.Stdout
			command.Stderr = os.Stderr
			err := command.Run()
			cancel()
			if err != nil {
				return fmt.Errorf("independent %s preflight denied: %w", filepath.Base(binary), err)
			}
		}
		return nil
	})
}

// checkedPreflight requires reviewed, non-writable, regular executables from
// independent authorities. The Go and previous authority preflights must both
// accept one identical copied state before a transition receipt can be issued.
func checkedPreflight(key string) (string, error) {
	binary := os.Getenv(key)
	if !filepath.IsAbs(binary) || filepath.Clean(binary) != binary {
		return "", fmt.Errorf("%s must specify an exact absolute executable", key)
	}
	info, err := os.Lstat(binary)
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0022 != 0 ||
		info.Mode().Perm()&0111 == 0 {
		return "", fmt.Errorf("%s is not a trusted regular executable", key)
	}
	owner, ok := info.Sys().(*syscall.Stat_t)
	if !ok || (owner.Uid != 0 && owner.Uid != uint32(os.Geteuid())) {
		return "", fmt.Errorf("%s executable owner is not trusted", key)
	}
	return binary, nil
}

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "Security migration denied:", err)
		os.Exit(1)
	}
	fmt.Println("State-bound security migration receipt prepared.")
}
