package transport

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dbontr/homelab-new/system/security"
)

func TestIndependentAuthorizationFailsClosedWhenSocketUnavailable(t *testing.T) {
	client, err := security.NewAdminClient(filepath.Join(t.TempDir(), "missing.sock"))
	if err != nil {
		t.Fatal(err)
	}
	server := &Server{privilegeClient: client}
	output, allowed := server.checkPrivilege("workspace__workspace_create", json.RawMessage(`{"project_id":"homelab-platform","cpu":2}`))
	if allowed || output == nil {
		t.Fatal("unavailable independent authority permitted a mutation")
	}
	if serialized, _ := json.Marshal(output); strings.Contains(string(serialized), "token") || strings.Contains(string(serialized), "passphrase") {
		t.Fatal("error exposed credential prompts")
	}
}

func TestPrivilegeTargetSelection(t *testing.T) {
	cases := []struct {
		name     string
		args     string
		expected string
	}{
		{"workspace__workspace_create", `{"project_id":"homelab-platform","node_id":"jupiter"}`, "homelab-platform"},
		{"workspace__github_merge_pull_request", `{"project_id":"homelab-platform","repository":"dbontr/homelab-new"}`, "dbontr/homelab-new"},
		{"cluster__configure", `{"host":"saturn","node_id":"jupiter"}`, "saturn"},
		{"workspace__pool_register_image", `{"name":"go-build"}`, "platform"},
	}
	for _, tc := range cases {
		if got := privilegeTarget(tc.name, json.RawMessage(tc.args)); got != tc.expected {
			t.Errorf("%s: got %s, want %s", tc.name, got, tc.expected)
		}
	}
}
