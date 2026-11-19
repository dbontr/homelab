package worker

import (
	"archive/tar"
	"testing"
)

func TestRepositoryArchivePath(t *testing.T) {
	tests := []struct {
		name string
		path string
		kind byte
		want string
		skip bool
		fail bool
	}{
		{name: "archive root", path: "owner-repo-commit/", kind: tar.TypeDir, skip: true},
		{name: "nested directory", path: "owner-repo-commit/services/compute/", kind: tar.TypeDir, want: "services/compute"},
		{name: "nested file", path: "owner-repo-commit/services/compute/main.go", kind: tar.TypeReg, want: "services/compute/main.go"},
		{name: "duplicate separator", path: "owner-repo-commit/services//", kind: tar.TypeDir, fail: true},
		{name: "parent escape", path: "owner-repo-commit/../secret", kind: tar.TypeReg, fail: true},
		{name: "nested parent escape", path: "owner-repo-commit/dir/../secret", kind: tar.TypeReg, fail: true},
		{name: "absolute header", path: "/owner-repo-commit/file", kind: tar.TypeReg, fail: true},
		{name: "empty path file", path: "owner-repo-commit/", kind: tar.TypeReg, fail: true},
		{name: "trailing slash file", path: "owner-repo-commit/regular/", kind: tar.TypeReg, fail: true},
		{name: "single top-level archive component", path: "owner-repo-commit", kind: tar.TypeDir, skip: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, skip, err := repositoryArchivePath(tt.path, tt.kind)
			if (err != nil) != tt.fail || got != tt.want || skip != tt.skip {
				t.Fatalf("repositoryArchivePath(%q) = %q,%t,%v; want %q,%t,fail=%t",
					tt.path, got, skip, err, tt.want, tt.skip, tt.fail)
			}
		})
	}
}
