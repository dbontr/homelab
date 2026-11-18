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
		args[1] != "--target" || args[2] != "go" ||
		args[3] != "--state-dir" || args[5] != "--snapshot-dir" {
		return errors.New("usage: homelab-security-migrate prepare --target go --state-dir ABSOLUTE_STATE_DIR --snapshot-dir ABSOLUTE_NEW_DIR")
	}
	stateDir, snapshotDir := args[4], args[6]
	if !filepath.IsAbs(stateDir) || !filepath.IsAbs(snapshotDir) ||
		filepath.Clean(stateDir) != stateDir || filepath.Clean(snapshotDir) != snapshotDir {
		return errors.New("migration paths must be canonical and absolute")
	}
	preflight := os.Getenv("HOMELAB_SECURITY_GO_PREFLIGHT")
	if !filepath.IsAbs(preflight) || filepath.Clean(preflight) != preflight {
		return errors.New("exact Go security preflight executable required")
	}
	info, err := os.Lstat(preflight)
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0022 != 0 ||
		info.Mode().Perm()&0111 == 0 {
		return errors.New("unsafe Go security preflight binary")
	}
	owner, ok := info.Sys().(*syscall.Stat_t)
	if !ok || (owner.Uid != 0 && owner.Uid != uint32(os.Geteuid())) {
		return errors.New("Go security preflight binary owner is not trusted")
	}
	lease, err := statelease.Acquire(stateDir, "securityd")
	if err != nil {
		return fmt.Errorf("security authority still owns the state lease: %w", err)
	}
	defer lease.Close()
	return security.PrepareMigration(stateDir, snapshotDir, "go", func(copyDir string) error {
		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cancel()
		command := exec.CommandContext(ctx, preflight, "--state-dir", copyDir)
		command.Env = os.Environ()
		command.Stdout = os.Stdout
		command.Stderr = os.Stderr
		return command.Run()
	})
}

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "Go security migration denied:", err)
		os.Exit(1)
	}
	fmt.Println("Go state-bound security migration receipt prepared.")
}
