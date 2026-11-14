package worker

import (
    "context"
    "errors"
    "os"
    "os/exec"
    "path/filepath"
    "strconv"
    "strings"
    "time"
)

// The Rust helper enforces a host operator policy independently of the Go
// device manifest. Workspace and project leases remain owned by the Go worker.
func nativeCharMapping(kind, path string) error {
    executable := os.Getenv("PWM_NATIVE_HARDWARE_HELPER")
    if executable == "" {
        return nil // Go compatibility path until a host enables native enforcement.
    }
    if !filepath.IsAbs(executable) || filepath.Clean(executable) != executable {
        return errors.New("native hardware helper must be an absolute executable")
    }
    policy := os.Getenv("PWM_NATIVE_DEVICE_POLICY")
    if !filepath.IsAbs(policy) || filepath.Clean(policy) != policy {
        return errors.New("native device policy must be an absolute path")
    }
    ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
    defer cancel()
    command := exec.CommandContext(ctx, executable, "map-char", kind, path)
    command.Env = []string{"LC_ALL=C", "HOMELAB_NATIVE_DEVICE_POLICY=" + policy}
    output, err := command.Output()
    if err != nil || len(output) > 512 {
        return errors.New("native accelerator identity validation failed")
    }
    lines := strings.Split(strings.TrimSpace(string(output)), "\n")
    if len(lines) != 2 || lines[0] != path ||
        strings.ContainsAny(lines[0], "\r\n\t ") {
        return errors.New("native accelerator mapping differed from authorized path")
    }
    if _, err := strconv.ParseUint(lines[1], 10, 32); err != nil {
        return errors.New("malformed native accelerator group")
    }
    return nil
}
