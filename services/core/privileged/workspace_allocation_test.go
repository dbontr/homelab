package privileged

import "testing"

func TestWorkspaceAllocationRequiresOperatorResourceLists(t *testing.T){
    policy:=&WorkspacePolicy{
        Projects:[]string{"research"},
        Images:[]string{"approved-image"},
        Workers:[]string{"jupiter"},
        Devices:[]string{"jtag"},
    }
    accepted:=map[string]any{
        "host":"saturn","project_id":"research",
        "image_pool_id":"approved-image","node_id":"jupiter",
        "cpu":2.0,"memory_mb":2048,"duration_minutes":90,
        "hardware":map[string]any{"gpu":1.0},
    }
    name,args,err:=policy.operationArgs("infrastructure__workspace_create",accepted)
    if err!=nil||name!="workspace_create"||args["project_id"]!="research"{
        t.Fatalf("exact allocation was rejected: %+v %v",args,err)
    }
    if _,exists:=args["host"];exists{
        t.Fatal("host executor identity leaked into controller workspace request")
    }
    cases:=[]struct{
        field string
        value any
    }{
        {"project_id","other"},
        {"image_pool_id","unknown"},
        {"node_id","untrusted"},
        {"architecture","other"},
        {"cpu",99.0},
        {"memory_mb",-1.0},
        {"duration_minutes",0.0},
        {"hardware",map[string]any{"root":1.0}},
        {"hardware",map[string]any{"gpu":9.0}},
        {"unknown","enabled"},
    }
    for _,tc:=range cases{
        changed:=make(map[string]any,len(accepted)+1)
        for k,v:=range accepted{changed[k]=v}
        changed[tc.field]=tc.value
        if _,_,err:=policy.operationArgs("infrastructure__workspace_create",changed);err==nil{
            t.Errorf("unsafe allocation accepted: field %s = %v",tc.field,tc.value)
        }
    }
    device:=make(map[string]any,len(accepted)+3)
    for k,v:=range accepted{device[k]=v}
    device["hardware"]=map[string]any{"usb":1.0}
    device["usb_passthrough"]=true
    device["usb_node"]="jupiter"
    device["usb_device_id"]="jtag"
    if _,_,err:=policy.operationArgs("infrastructure__workspace_create",device);err!=nil{
        t.Fatal("approved operator-selected USB resource was rejected:",err)
    }
    device["usb_node"]="outside"
    if _,_,err:=policy.operationArgs("infrastructure__workspace_create",device);err==nil{
        t.Fatal("unapproved USB worker was accepted")
    }
    device["usb_node"]="jupiter"
    device["usb_device_id"]="unlisted"
    if _,_,err:=policy.operationArgs("infrastructure__workspace_create",device);err==nil{
        t.Fatal("unapproved USB device was accepted")
    }
    delete(device,"usb_passthrough")
    delete(device,"usb_node")
    delete(device,"usb_device_id")
    device["hardware"]=map[string]any{"fpga":1}
    device["node_id"]=""
    if _,_,err:=policy.operationArgs("infrastructure__workspace_create",device);err==nil{
        t.Fatal("FPGA claim without an approved exact worker was accepted")
    }
}
