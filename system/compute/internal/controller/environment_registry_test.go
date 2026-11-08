package controller

import (
	"strings"
	"testing"
)

func TestEnvironmentImageNameUsesApprovedRegistry(t *testing.T) {
	local, err := environmentImageName("research", "toolchain", 1, "")
	if err != nil || local != "pwm-env-research-toolchain:v1" {
		t.Fatalf("Docker workspace image identity changed: %q %v", local, err)
	}
	remote, err := environmentImageName("research", "toolchain", 3, "ghcr.io/dbontr")
	if err != nil || remote != "ghcr.io/dbontr/pwm-env-research-toolchain:v3" {
		t.Fatalf("registry image not qualified: %q %v", remote, err)
	}
	for _, invalid := range []string{
		"http://registry.example", "registry..example", "registry.example/../../secrets",
		"registry.example/", "registry.example/project?token=x", "local",
		strings.Repeat("a", 129) + ".example",
	} {
		if _, err := environmentImageName("research", "toolchain", 1, invalid); err == nil {
			t.Fatalf("unsafe registry accepted: %q", invalid)
		}
	}
}
