package approval

import "testing"

func TestBackendOperationScopes(t *testing.T){
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
 for name,want:=range cases{
  if got:=ScopeFor(name);got!=want{t.Errorf("%s: scope %q, want %q",name,got,want)}
 }
}
