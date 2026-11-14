package release

import (
 "context"
 "os"
 "os/exec"
 "path/filepath"
 "strings"
 "testing"
)

func TestReleaseCommandUsesProtectedHome(t *testing.T) {
 envExecutable, err := exec.LookPath("env")
 if err != nil {
  t.Skipf("environment inspection utility unavailable: %v", err)
 }
 home := t.TempDir()
 t.Setenv("RELEASE_TEST_SECRET_SHOULD_NOT_PROPAGATE", "private-parent-value")
 output, err := ExecWithHome(home)(context.Background(), envExecutable)
 if err != nil {
  t.Fatal(err)
 }
 variables := map[string]string{}
 for _, line := range strings.Split(output, "\n") {
  key, value, ok := strings.Cut(line, "=")
  if ok { variables[key] = value }
 }
 if variables["HOME"] != home || variables["XDG_CACHE_HOME"] != filepath.Join(home, ".cache") {
  t.Fatalf("release cache escaped its private home: HOME=%q XDG_CACHE_HOME=%q",
   variables["HOME"], variables["XDG_CACHE_HOME"])
 }
 if variables["LANG"] != "C" || variables["PATH"] != "/run/current-system/sw/bin:/usr/bin:/bin" {
  t.Fatalf("unexpected release command environment: %#v", variables)
 }
 if _, leaked := variables["RELEASE_TEST_SECRET_SHOULD_NOT_PROPAGATE"]; leaked {
  t.Fatal("untrusted parent environment leaked into release command")
 }
}

func TestReleaseCommandRejectsUnsafeHome(t *testing.T) {
 for _, home := range []string{"", ".", "relative/path", "/", "/private/../escape"} {
  _, err := ExecWithHome(home)(context.Background(), "/bin/true")
  if err == nil { t.Fatalf("accepted unsafe release home %q", home) }
 }
}
