package devices

import (
 "encoding/json"
 "testing"
)

func TestUnpinnedFPGAPlanRequiresOneEligibleComputeNode(t *testing.T) {
    untrusted := Device{ID:"external",Kind:"fpga",Mode:ModeSSH,Connection:"ssh",AllowedOperations:[]string{"connect","inspect"}}
    catalog, err := New([]Device{untrusted})
    if err != nil {t.Fatal(err)}
    request := json.RawMessage(`{"hardware":{"fpga":1}}`)
    if err := catalog.ValidatePlan(request); err == nil {t.Fatal("SSH-only FPGA was included in resource planning")}
    registered := Device{ID:"dev-1",Kind:"fpga",Mode:ModeEnrolled,Connection:"usb",Node:"compute-1",AllowedOperations:[]string{"allocate"},EligibleForCompute:true}
    catalog,err=New([]Device{untrusted,registered}); if err!=nil {t.Fatal(err)}
    if err := catalog.ValidatePlan(request); err!=nil {t.Fatalf("valid unpinned plan rejected: %v",err)}
    if err := catalog.ValidateAllocation(request); err==nil {t.Fatal("unpinned allocation accepted")}
    if err := catalog.ValidatePlan(json.RawMessage(`{"hardware":{"fpga":2}}`)); err==nil {t.Fatal("insufficient FPGA capacity allowed")}
    if err := catalog.ValidatePlan(json.RawMessage(`{"hardware":{"fpga":1},"node_id":"compute-2"}`)); err==nil {t.Fatal("ineligible compute node allowed")}
    if err := catalog.ValidateAllocation(json.RawMessage(`{"hardware":{"fpga":1},"node_id":"compute-1"}`)); err!=nil {t.Fatal(err)}
}
