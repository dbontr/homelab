package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"
)

type approvedCommand struct {
	binary string
	args   []string
}

func lowerHex(value string, sizes ...int) bool {
	allowed := false
	for _, n := range sizes {
		if len(value) == n {
			allowed = true
		}
	}
	if !allowed {
		return false
	}
	for i := 0; i < len(value); i++ {
		if value[i] < '0' || value[i] > '9' {
			if value[i] < 'a' || value[i] > 'f' {
				return false
			}
		}
	}
	return true
}

func validRepoPart(value string) bool {
	if len(value) == 0 || len(value) > 100 || value == "." || value == ".." ||
		value[0] == '.' || value[len(value)-1] == '.' {
		return false
	}
	for _, b := range []byte(value) {
		if (b >= 'a' && b <= 'z') || (b >= 'A' && b <= 'Z') ||
			(b >= '0' && b <= '9') || b == '.' || b == '_' || b == '-' {
			continue
		}
		return false
	}
	return true
}

func fileCustody(path string) (os.FileInfo, error) {
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0022 != 0 {
		return nil, errors.New("tool has unsafe custody")
	}
	return info, nil
}

func trustedBinary(key string) (string, error) {
	path := os.Getenv(key)
	if !filepath.IsAbs(path) || !strings.HasPrefix(path, "/nix/store/") ||
		filepath.Clean(path) != path {
		return "", errors.New("sandbox tool must be an immutable store path")
	}
	if _, err := fileCustody(path); err != nil {
		return "", err
	}
	return path, nil
}

func workspace(id string) (string, error) {
	if !lowerHex(id, 32) {
		return "", errors.New("invalid workspace identity")
	}
	base := os.Getenv("HOMELAB_SANDBOX_WORKSPACES")
	root, err := filepath.EvalSymlinks(base)
	if err != nil || root == "/" || !filepath.IsAbs(root) {
		return "", errors.New("sandbox workspace root unavailable")
	}
	candidate := filepath.Join(root, id)
	info, err := os.Lstat(candidate)
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 || info.Mode().Perm()&0002 != 0 {
		return "", errors.New("workspace has unsafe custody")
	}
	realPath, err := filepath.EvalSymlinks(candidate)
	if err != nil || filepath.Dir(realPath) != root {
		return "", errors.New("workspace escaped configured root")
	}
	return realPath, nil
}

func sourceRevision(root string) (string, error) {
	git, err := trustedBinary("HOMELAB_SANDBOX_GIT")
	if err != nil {
		return "", err
	}
	cmd := exec.Command(git, "--no-optional-locks", "-C", root, "rev-parse", "--verify", "HEAD")
	cmd.Env = []string{"HOME=/tmp", "GIT_CONFIG_NOSYSTEM=1"}
	cmd.Stdin = nil
	output, err := cmd.Output()
	if err != nil || len(output) > 100 {
		return "", errors.New("Git revision check failed")
	}
	revision := strings.TrimSpace(string(output))
	if !lowerHex(revision, 40, 64) {
		return "", errors.New("malformed source revision")
	}
	return revision, nil
}

func storeTool(action string) (approvedCommand, error) {
	var key string
	var args []string
	switch action {
	case "go-test":
		key, args = "HOMELAB_SANDBOX_GO", []string{"test", "-count=1", "./..."}
	case "go-vet":
		key, args = "HOMELAB_SANDBOX_GO", []string{"vet", "./..."}
	case "cargo-test":
		key, args = "HOMELAB_SANDBOX_CARGO", []string{"test", "--locked", "--offline", "--all-targets"}
	case "cargo-check":
		key, args = "HOMELAB_SANDBOX_CARGO", []string{"check", "--locked", "--offline"}
	case "cargo-clippy":
		key, args = "HOMELAB_SANDBOX_CARGO", []string{"clippy", "--locked", "--offline", "--all-targets", "--", "-D", "warnings"}
	case "nix-check":
		key, args = "HOMELAB_SANDBOX_NIX", []string{"flake", "check", "--offline", "--no-build"}
	default:
		return approvedCommand{}, errors.New("unregistered sandbox operation")
	}
	binary, err := trustedBinary(key)
	if err != nil {
		return approvedCommand{}, err
	}
	return approvedCommand{binary, args}, nil
}

func containedTool(action string) (approvedCommand, error) {
	switch action {
	case "go-test":
		return approvedCommand{"/usr/local/go/bin/go", []string{"test", "-count=1", "./..."}}, nil
	case "go-vet":
		return approvedCommand{"/usr/local/go/bin/go", []string{"vet", "./..."}}, nil
	case "cargo-test":
		return approvedCommand{"/usr/local/cargo/bin/cargo", []string{"test", "--locked", "--offline", "--all-targets"}}, nil
	case "cargo-check":
		return approvedCommand{"/usr/local/cargo/bin/cargo", []string{"check", "--locked", "--offline"}}, nil
	case "cargo-clippy":
		return approvedCommand{"/usr/local/cargo/bin/cargo", []string{"clippy", "--locked", "--offline", "--all-targets", "--", "-D", "warnings"}}, nil
	default:
		return approvedCommand{}, errors.New("unknown verification action")
	}
}

func sameFile(a, b os.FileInfo) bool {
	x, okX := a.Sys().(*syscall.Stat_t)
	y, okY := b.Sys().(*syscall.Stat_t)
	return okX && okY && x.Dev == y.Dev && x.Ino == y.Ino
}

func timeoutFromEnv() time.Duration {
	seconds, err := strconv.Atoi(os.Getenv("HOMELAB_SANDBOX_TIMEOUT_SECONDS"))
	if err != nil || seconds < 1 || seconds > 1800 {
		seconds = 300
	}
	return time.Duration(seconds) * time.Second
}

func boundedCommand(timeout time.Duration, binary string, args []string, dir string, env []string) error {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, binary, args...)
	cmd.Env = env
	cmd.Dir = dir
	cmd.Stdin = nil
	cmd.Stdout = io.Discard
	cmd.Stderr = io.Discard
	if err := cmd.Run(); err != nil {
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			return errors.New("verification timed out")
		}
		return errors.New("verification command failed")
	}
	return nil
}

func runWorkspace(id, revision, action string) error {
	if os.Geteuid() == 0 {
		return errors.New("engineering sandbox must not run with root authority")
	}
	if !lowerHex(revision, 40, 64) {
		return errors.New("invalid revision")
	}
	root, err := workspace(id)
	if err != nil {
		return err
	}
	actual, err := sourceRevision(root)
	if err != nil || actual != revision {
		return errors.New("workspace revision is stale")
	}
	command, err := storeTool(action)
	if err != nil {
		return err
	}
	bwrap, err := trustedBinary("HOMELAB_SANDBOX_BWRAP")
	if err != nil {
		return err
	}
	before, err := os.Stat(root)
	if err != nil {
		return errors.New("workspace unavailable")
	}
	args := []string{
		"--unshare-all", "--die-with-parent", "--new-session",
		"--ro-bind", "/nix/store", "/nix/store",
		"--ro-bind", root, "/workspace",
		"--proc", "/proc", "--dev", "/dev", "--tmpfs", "/tmp",
		"--dir", "/home", "--chdir", "/workspace",
		"--setenv", "HOME", "/tmp", "--setenv", "TMPDIR", "/tmp",
		"--setenv", "CARGO_TARGET_DIR", "/tmp/cargo-target",
		"--setenv", "GOCACHE", "/tmp/go-cache", "--setenv", "GOPATH", "/tmp/go",
		"--setenv", "GOPROXY", "off", "--setenv", "GOSUMDB", "off",
		"--setenv", "GOTOOLCHAIN", "local",
		"--setenv", "CARGO_NET_OFFLINE", "true",
		"--setenv", "LC_ALL", "C", "--",
		command.binary,
	}
	args = append(args, command.args...)
	after, err := os.Stat(root)
	if err != nil || !sameFile(before, after) {
		return errors.New("workspace replaced before verification")
	}
	if err := boundedCommand(timeoutFromEnv(), bwrap, args, "", []string{}); err != nil {
		return err
	}
	fmt.Println("completed")
	return nil
}

func containedCheckout(repository string) (string, error) {
	pieces := strings.Split(repository, "/")
	if len(pieces) != 2 || !validRepoPart(pieces[0]) || !validRepoPart(pieces[1]) {
		return "", errors.New("repository must be owner/name")
	}
	base := "/workspace/repos"
	baseInfo, err := os.Lstat(base)
	if err != nil || !baseInfo.IsDir() || baseInfo.Mode()&os.ModeSymlink != 0 {
		return "", errors.New("repository ancestry is unsafe")
	}
	source := filepath.Join(base, pieces[0]+"__"+pieces[1])
	real, err := filepath.EvalSymlinks(source)
	if err != nil || real != source {
		return "", errors.New("repository checkout is redirected")
	}
	info, err := os.Lstat(source)
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 || info.Mode().Perm()&0002 != 0 {
		return "", errors.New("checkout permissions are unsafe")
	}
	return source, nil
}

func importReceipt(source, repository, revision string) error {
	marker := filepath.Join(source, ".pwm-source-revision")
	info, err := fileCustody(marker)
	if err != nil || info.Size() > 256 {
		return errors.New("source import receipt unavailable")
	}
	data, err := os.ReadFile(marker)
	if err != nil || len(data) > 256 || string(data) != repository+"\n"+revision+"\n" {
		return errors.New("source does not match approved archive reference")
	}
	return nil
}

func runContained(repository, revision, action string) error {
	if os.Geteuid() == 0 {
		return errors.New("verification must not run as root")
	}
	if !lowerHex(revision, 40, 64) {
		return errors.New("invalid expected Git revision")
	}
	source, err := containedCheckout(repository)
	if err != nil {
		return err
	}
	if err := importReceipt(source, repository, revision); err != nil {
		return err
	}
	command, err := containedTool(action)
	if err != nil {
		return err
	}
	before, err := os.Stat(source)
	if err != nil {
		return errors.New("checkout unavailable")
	}
	after, err := os.Stat(source)
	if err != nil || !sameFile(before, after) {
		return errors.New("checkout was replaced before verification")
	}
	env := []string{
		"HOME=/tmp", "TMPDIR=/tmp", "GOENV=off", "GOPATH=/tmp/go",
		"GOCACHE=/tmp/go-cache", "GOPROXY=off", "GOSUMDB=off",
		"GOTOOLCHAIN=local", "CARGO_HOME=/usr/local/cargo",
		"CARGO_TARGET_DIR=/tmp/cargo-target", "CARGO_NET_OFFLINE=true",
		"RUSTUP_HOME=/usr/local/rustup", "LC_ALL=C",
		"PATH=/usr/local/go/bin:/usr/local/cargo/bin:/usr/bin:/bin",
	}
	if err := boundedCommand(600*time.Second, command.binary, command.args, source, env); err != nil {
		return err
	}
	if err := importReceipt(source, repository, revision); err != nil {
		return err
	}
	fmt.Println("completed")
	return nil
}

func main() {
	args := os.Args[1:]
	var err error
	if len(args) == 4 && args[0] == "run" {
		err = runWorkspace(args[1], args[2], args[3])
	} else if len(args) == 4 && args[0] == "run-contained" {
		err = runContained(args[1], args[2], args[3])
	} else {
		err = errors.New("usage: homelab-engineering-sandbox run WORKSPACE_ID GIT_SHA ACTION")
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
