package security

import "testing"

func TestPrivilegedWorkspaceMutationsRequireOperationBoundConfirmation(t *testing.T){
 for _,operation:=range []string{
  "infrastructure__workspace_create",
  "infrastructure__workspace_register_image",
  "infrastructure__workspace_propose_program",
  "infrastructure__workspace_review_program",
  "infrastructure__workspace_update_entry",
  "infrastructure__workspace_stop",
  "infrastructure__workspace_build_environment",
  "infrastructure__workspace_register_software",
  "infrastructure__workspace_revoke_worker",
 }{
  if !dangerousOperation(operation){t.Fatalf("%s must require destructive owner confirmation",operation)}
 }
}
