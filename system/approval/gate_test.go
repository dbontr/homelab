package approval

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestScopedOnceAndExactArguments(t *testing.T) {
	gate := New()
	base := time.Now().UTC()
	gate.now = func() time.Time { return base }
	args := json.RawMessage(`{"node_id":"jupiter"}`)
	ok, challenge, err := gate.Check("cluster__configure", args)
	if err != nil || ok || challenge.ID == "" {
		t.Fatalf("challenge %#v %v", challenge, err)
	}
	if challenge.SHA256 == "" || challenge.Summary == "" {
		t.Fatal("approval request lacks an operation preview")
	}
	if _, err := gate.Approve(challenge.ID); err != nil {
		t.Fatal(err)
	}
	ok, other, err := gate.Check("cluster__configure", json.RawMessage(`{"node_id":"venus"}`))
	if err != nil || ok || other.ID == challenge.ID {
		t.Fatalf("changed arguments were accepted: %#v %v", other, err)
	}
	ok, _, err = gate.Check("cluster__configure", json.RawMessage(`{ "node_id": "jupiter" }`))
	if err != nil || !ok {
		t.Fatalf("approved call was rejected: %v", err)
	}
	ok, _, err = gate.Check("cluster__configure", args)
	if err != nil || ok {
		t.Fatal("single-use grant was reused")
	}
}

func TestExpiry(t *testing.T) {
	gate := New()
	now := time.Now().UTC()
	gate.now = func() time.Time { return now }
	_, challenge, _ := gate.Check("cluster__configure", nil)
	now = now.Add(PendingTTL + time.Second)
	if _, err := gate.Approve(challenge.ID); err == nil {
		t.Fatal("expired request was approved")
	}
	_, challenge, _ = gate.Check("cluster__configure", nil)
	if _, err := gate.Approve(challenge.ID); err != nil {
		t.Fatal(err)
	}
	now = now.Add(ApprovedTTL + time.Second)
	ok, _, err := gate.Check("cluster__configure", nil)
	if err != nil || ok {
		t.Fatal("expired grant was accepted")
	}
}

func TestRedactionAndScope(t *testing.T) {
	gate := New()
	_, challenge, err := gate.Check("cluster__configure", json.RawMessage(`{"host":"saturn","api_token":"private"}`))
	if err != nil || !strings.Contains(challenge.Summary, "[REDACTED]") || strings.Contains(challenge.Summary, "private") {
		t.Fatalf("secret leaked in preview: %q %v", challenge.Summary, err)
	}
	if ScopeFor("workspace__worker_revoke") != "cluster.modify" || ScopeFor("workspace__github_read_file") != "" {
		t.Fatal("incorrect privilege classification")
	}
}


func TestSensitiveBackendOperationsNeedIndependentApproval(t *testing.T) {
 cases:=map[string]string{
  "workspace__github_account_transfer":"source.modify",
  "workspace__github_repository_api":"source.modify",
  "workspace__pool_register_image":"software.manage",
  "workspace__pool_propose_program":"software.manage",
  "workspace__environment_build":"software.manage",
  "workspace__external_resource_reserve":"cluster.modify",
  "workspace__worker_revoke":"cluster.modify",
  "workspace__workspace_call":"compute.execute",
  "workspace__unrecognized_advanced_action":"workspace.modify",
  "health__delete_all":"personal.data.manage",
  "learning__reset_progress":"personal.data.manage",
  "workspace__resource_plan":"",
  "workspace__list_projects":"",
  "workspace__task_create":"",
 }
 for tool,want:=range cases{
  if got:=ScopeFor(tool);got!=want{t.Fatalf("%s: got %q, want %q",tool,got,want)}
 }
}
