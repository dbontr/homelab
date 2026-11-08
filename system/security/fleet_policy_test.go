package security

import "testing"

func TestFleetRecoveryAndClearanceAlwaysRequireStepUp(t *testing.T){
 for _,operation:=range []string{
  "infrastructure__fleet_activate",
  "infrastructure__fleet_recover",
  "infrastructure__fleet_clear",
 }{
  if !dangerousOperation(operation){t.Fatalf("operation %s may bypass owner confirmation",operation)}
 }
 if dangerousOperation("infrastructure__fleet_status"){t.Fatal("read-only fleet status requires unexpected destructive confirmation")}
}
