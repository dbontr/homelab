package security

import (
	"io/fs"
	"os"
	"path/filepath"
)

// PrivateCredentialFileAllowed accepts owner-only files and systemd's
// group-readable credential copies. A copy with group read permission is
// allowed only inside the credential directory supplied to this service.
func PrivateCredentialFileAllowed(path string, mode fs.FileMode, ownerUID uint32) bool {
	if ownerUID != 0 && ownerUID != uint32(os.Geteuid()) {
		return false
	}
	if mode.Perm()&0077 == 0 {
		return true
	}
	directory := os.Getenv("CREDENTIALS_DIRECTORY")
	return directory != "" &&
		filepath.Dir(filepath.Clean(path)) == filepath.Clean(directory) &&
		mode.Perm()&0037 == 0
}
