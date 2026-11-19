package main

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"syscall"
	"time"

	"github.com/dbontr/homelab-new/system/compute/internal/controller"
)

var (
	ttyName = regexp.MustCompile("^/dev/tty[1-9][0-9]*$")
	sessionName = regexp.MustCompile("^[0-9]{1,8}$")
)

const auditDirectory = "/var/lib/homelab/compute-console-audit"

// A physical root login is the separate administrative authority. An SSH
// session, pseudoterminal, or root process without an attached login fails.
func validConsoleSession(uid int, tty, session string, properties map[string]string) bool {
	return uid == 0 && ttyName.MatchString(tty) &&
		sessionName.MatchString(session) &&
		properties["User"] == "0" &&
		properties["Seat"] == "seat0" &&
		properties["Type"] == "tty" &&
		properties["Remote"] == "no" &&
		properties["Service"] == "login" &&
		properties["TTY"] == strings.TrimPrefix(tty, "/dev/")
}

func localConsole() (string, string, error) {
	if os.Getuid() != 0 || os.Geteuid() != 0 {
		return "", "", errors.New("root login required")
	}
	info, err := os.Stdin.Stat()
	if err != nil || info.Mode()&os.ModeCharDevice == 0 {
		return "", "", errors.New("physical console input required")
	}
	tty, err := os.Readlink("/proc/self/fd/0")
	if err != nil || !ttyName.MatchString(tty) {
		return "", "", errors.New("local virtual terminal required")
	}
	session := os.Getenv("XDG_SESSION_ID")
	if !sessionName.MatchString(session) {
		return "", "", errors.New("local login session unavailable")
	}
	output, err := exec.Command("/run/current-system/sw/bin/loginctl",
		"show-session", session, "-p", "User", "-p", "Seat", "-p",
		"Type", "-p", "Remote", "-p", "Service", "-p", "TTY").Output()
	if err != nil {
		return "", "", errors.New("cannot verify local login session")
	}
	properties := make(map[string]string)
	for _, line := range strings.Split(string(output), "\n") {
		if parts := strings.SplitN(line, "=", 2); len(parts) == 2 {
			properties[parts[0]] = parts[1]
		}
	}
	if !validConsoleSession(os.Getuid(), tty, session, properties) {
		return "", "", errors.New("login is not an attached local root session")
	}
	return tty, session, nil
}

type auditRecord struct {
	ID           string    `json:"request_id"`
	Authority    string    `json:"authority"`
	Session      string    `json:"session"`
	TTY          string    `json:"tty"`
	Project      string    `json:"project"`
	Principal    string    `json:"principal"`
	Role         string    `json:"role"`
	ExpectedRole string    `json:"expected_role"`
	Outcome      string    `json:"outcome"`
	Created      time.Time `json:"created"`
}

func auditFile(kind string, record auditRecord) error {
	if err := os.MkdirAll(auditDirectory, 0700); err != nil {
		return err
	}
	info, err := os.Lstat(auditDirectory)
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 ||
		info.Mode().Perm()&0077 != 0 {
		return errors.New("private console audit directory unavailable")
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok || stat.Uid != 0 {
		return errors.New("console audit directory is not root-owned")
	}
	file, err := os.OpenFile(filepath.Join(auditDirectory,
		record.ID+"-"+kind+".json"), os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return err
	}
	if err := json.NewEncoder(file).Encode(record); err != nil {
		file.Close()
		return err
	}
	if err := file.Sync(); err != nil {
		file.Close()
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	dir, err := os.Open(auditDirectory)
	if err != nil {
		return err
	}
	defer dir.Close()
	return dir.Sync()
}

func grant(args []string) error {
	if len(args) != 5 {
		return errors.New("usage: project-workspace-console grant DB_PATH PROJECT OIDC_PRINCIPAL ROLE EXPECTED_ROLE (use 'none' when no grant exists)")
	}
	tty, session, err := localConsole()
	if err != nil {
		return err
	}
	dbPath, project, principal, role, expected := args[0], args[1], args[2], args[3], args[4]
	if expected == "none" {
		expected = ""
	}
	if !filepath.IsAbs(dbPath) {
		return errors.New("controller database path must be absolute")
	}
	info, err := os.Lstat(dbPath)
	if err != nil || !info.Mode().IsRegular() {
		return errors.New("existing regular controller database required")
	}
	db, err := controller.OpenDatabase(dbPath)
	if err != nil {
		return err
	}
	defer db.Close()
	previous, err := db.ProjectRole(project, principal)
	if err != nil {
		return err
	}
	if previous != expected {
		return fmt.Errorf("project grant revision conflict: observed role %q", previous)
	}
	var id [16]byte
	if _, err := rand.Read(id[:]); err != nil {
		return err
	}
	record := auditRecord{
		ID: hex.EncodeToString(id[:]), Authority: "local-root-console",
		Session: session, TTY: tty, Project: project, Principal: principal,
		Role: role, ExpectedRole: expected, Outcome: "requested",
		Created: time.Now().UTC(),
	}
	if err := auditFile("intent", record); err != nil {
		return fmt.Errorf("console grant not applied: %w", err)
	}
	_, applyErr := db.SetProjectGrant(project, principal, role, expected)
	if applyErr == nil {
		db.AuditAccess("local-root-console", project, "console:project-grant:"+role, true)
		actual, verifyErr := db.ProjectRole(project, principal)
		if verifyErr != nil {
			applyErr = verifyErr
		} else if role != "revoke" && actual != role || role == "revoke" && actual != "" {
			applyErr = errors.New("project grant readback mismatch")
		}
	}
	record.Created = time.Now().UTC()
	record.Outcome = "completed"
	if applyErr != nil {
		record.Outcome = "failed"
	}
	if err := auditFile("result", record); err != nil {
		return fmt.Errorf("grant outcome requires reconciliation, request %s: %w", record.ID, err)
	}
	if applyErr != nil {
		return fmt.Errorf("grant %s failed: %w", record.ID, applyErr)
	}
	return json.NewEncoder(os.Stdout).Encode(map[string]string{
		"request_id": record.ID, "authority": record.Authority,
		"project": project, "principal": principal, "role": role,
		"previous_role": previous, "status": "completed",
	})
}

func main() {
	if len(os.Args) < 2 || os.Args[1] != "grant" {
		fmt.Fprintln(os.Stderr, "usage: project-workspace-console grant DB_PATH PROJECT OIDC_PRINCIPAL ROLE EXPECTED_ROLE")
		os.Exit(2)
	}
	if err := grant(os.Args[2:]); err != nil {
		fmt.Fprintln(os.Stderr, "project-workspace-console:", err)
		os.Exit(1)
	}
}
