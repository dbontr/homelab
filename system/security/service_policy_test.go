package security

import "testing"

func TestSystemdLifecycleAlwaysRequiresDestructiveStepUp(t *testing.T){
 for _,operation:=range []string{
  "infrastructure__service_start",
  "infrastructure__service_stop",
  "infrastructure__service_reload",
  "infrastructure__service_restart",
 }{
  if !dangerousOperation(operation){t.Fatalf("%s must require fresh owner confirmation",operation)}
 }
}
