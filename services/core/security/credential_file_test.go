package security

import (
	"os"
	"path/filepath"
	"testing"
)

func TestPrivateCredentialFileAllowed(t *testing.T) {
	uid := uint32(os.Geteuid())
	protected := filepath.Join(t.TempDir(), "password")
	t.Setenv("CREDENTIALS_DIRECTORY", filepath.Dir(protected))
	for _, tc := range []struct {
		name string
		path string
		mode os.FileMode
		owner uint32
		want bool
	}{
		{"owner-only", protected, 0600, uid, true},
		{"systemd-group-read", protected, 0440, uid, true},
		{"other-read", protected, 0444, uid, false},
		{"group-write", protected, 0660, uid, false},
		{"wrong-owner", protected, 0440, uid + 1000, false},
		{"outside-credential-dir", filepath.Join(t.TempDir(), "password"), 0440, uid, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := PrivateCredentialFileAllowed(tc.path, tc.mode, tc.owner); got != tc.want {
				t.Fatalf("admission = %v, want %v", got, tc.want)
			}
		})
	}
	t.Setenv("CREDENTIALS_DIRECTORY", "")
	if PrivateCredentialFileAllowed(protected, 0440, uid) {
		t.Fatal("group-readable file accepted without systemd directory")
	}
}
