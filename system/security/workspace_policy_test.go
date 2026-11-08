package security

import "testing"

func TestPrivilegedWorkspaceMutationsRequireOperationBoundConfirmation(t *testing.T){
 for _,operation:=range []string{
  "infrastructure__workspace_stop",
  "infrastructure__workspace_build_environment",
  "infrastructure__workspace_register_software",
  "infrastructure__workspace_revoke_worker",
 }{
  if !dangerousOperation(operation){t.Fatalf("%s must require destructive owner confirmation",operation)}
 }
}
