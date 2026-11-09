package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoopbackListener(t *testing.T) {
	for _, tc := range []struct {
		address string
		local bool
	}{
		{"127.0.0.1:8200", true},
		{"[::1]:8200", true},
		{"localhost:8200", true},
		{":8200", false},
		{"0.0.0.0:8200", false},
		{"[::]:8200", false},
		{"198.51.100.12:8200", false},
		{"invalid", false},
	} {
		if got := loopbackListener(tc.address); got != tc.local {
			t.Errorf("%q: loopback = %t; want %t", tc.address, got, tc.local)
		}
	}
}

func TestReadBearerCredential(t *testing.T) {
	path := filepath.Join(t.TempDir(), "token")
	t.Setenv("ADAPTIVE_TEACHING_BEARER_TOKEN_FILE", path)
	const token = "0123456789abcdef0123456789abcdef"
	if err := os.WriteFile(path, []byte(token+"\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if value, err := readBearerCredential(); err != nil || value != token {
		t.Fatalf("token read failed: %q, %v", value, err)
	}
	if err := os.Chmod(path, 0644); err != nil {
		t.Fatal(err)
	}
	if _, err := readBearerCredential(); err == nil {
		t.Fatal("world-readable credential accepted")
	}
	if err := os.Chmod(path, 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("short"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := readBearerCredential(); err == nil || !strings.Contains(err.Error(), "32 characters") {
		t.Fatalf("weak credential accepted: %v", err)
	}
}
