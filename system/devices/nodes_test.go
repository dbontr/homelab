package devices

import (
    "encoding/json"
    "os"
    "path/filepath"
    "testing"
)

func TestNodeMembershipFollowsConfiguration(t *testing.T) {
    nodes, err := NewNodes([]Node{
        {ID:"saturn",Mode:ModeEnrolled,Role:"controller",System:"x86_64-linux"},
        {ID:"venus",Mode:ModeEnrolled,Role:"compute",System:"x86_64-linux"},
        {ID:"external",Mode:ModeSSH},
    })
    if err != nil { t.Fatal(err) }
    c,err:=New([]Device{{ID:"fpga-1",Kind:"fpga",Mode:ModeEnrolled,Connection:"usb",Node:"venus",AllowedOperations:[]string{"allocate"},EligibleForCompute:true}})
    if err!=nil {t.Fatal(err)}
    if err:=c.ValidateNodes(nodes);err!=nil {t.Fatal(err)}
    for _,entry:=range []Node{
        {ID:"venus",Mode:ModeSSH},
        {ID:"venus",Mode:ModeEnrolled,Role:"controller",System:"x86_64-linux"},
    } {
        modified,err:=NewNodes([]Node{entry})
        if err!=nil {t.Fatal(err)}
        if err:=c.ValidateNodes(modified);err==nil {t.Fatal("compute lease accepted on ineligible node")}
    }
    empty,_:=NewNodes(nil)
    if err:=c.ValidateNodes(empty);err==nil {t.Fatal("missing node accepted")}
    if len(nodes.List())!=3 {t.Fatal("node list incomplete")}
}

func TestNodeInventoryValidity(t *testing.T) {
    for _,items:=range [][]Node{
        {{ID:"duplicate",Mode:ModeSSH},{ID:"duplicate",Mode:ModeSSH}},
        {{ID:"bad_name",Mode:ModeSSH}},
        {{ID:"test",Mode:ModeEnrolled,Role:"compute",System:""}},
        {{ID:"test",Mode:ModeEnrolled,Role:"unknown",System:"x86_64-linux"}},
        {{ID:"test",Mode:ModeSSH,Role:"compute"}},
    } {
        if _,err:=NewNodes(items);err==nil {t.Fatalf("invalid nodes accepted: %+v",items)}
    }
    path:=filepath.Join(t.TempDir(),"nodes.json")
    raw,_:=json.Marshal([]Node{{ID:"jupiter",Mode:ModeEnrolled,Role:"compute",System:"x86_64-linux"}})
    if err:=os.WriteFile(path,raw,0600);err!=nil {t.Fatal(err)}
    nodes,err:=LoadNodes(path)
    if err!=nil || len(nodes.List())!=1 {t.Fatalf("load nodes: %v",err)}
    if err:=os.WriteFile(path,[]byte("[] {}"),0600);err!=nil {t.Fatal(err)}
    if _,err:=LoadNodes(path);err==nil {t.Fatal("accepted trailing input")}
}
