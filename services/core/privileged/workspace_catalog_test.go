package privileged

import (
    "strings"
    "testing"
)

func catalogPolicy() *WorkspacePolicy {
    return &WorkspacePolicy{
        Projects:[]string{"research"},
        Workers:[]string{"jupiter"},
        Images:[]string{"builder-image"},
        ImageReferences:[]string{"registry.example/builder@sha256:"+strings.Repeat("a",64)},
        Software:[]string{"toolchain"},
        Programs:[]string{"compiler"},
        Packages:[]string{"git"},
    }
}
func TestWorkspaceUSBDeviceAllowlistMatchesControllerSyntax(t *testing.T) {
    policy:=&WorkspacePolicy{
        Endpoint:"https://controller.example/mcp",
        TokenSource:func()(string,error){return strings.Repeat("a",40),nil},
        Projects:[]string{"research"},
        Devices:[]string{"jtag:1"},
    }
    if err:=policy.validate();err!=nil{
        t.Fatalf("controller-compatible device identity rejected: %v",err)
    }
    for _,identity:=range []string{
        "/dev/bus/usb/001/002","device id","-device",strings.Repeat("j",129),
    }{
        policy.Devices=[]string{identity}
        if err:=policy.validate();err==nil{
            t.Errorf("invalid device policy identity accepted: %q",identity)
        }
    }
    policy.Devices=[]string{"jtag:1"}
    for _,identity:=range []string{
        "a","A-device","not_valid",strings.Repeat("n",64),
    }{
        policy.Projects=[]string{identity}
        if err:=policy.validate();err==nil{
            t.Errorf("invalid controller resource identity accepted: %q",identity)
        }
    }
}

func TestWorkspaceCatalogUsesExactOperatorResourceNames(t *testing.T){
    policy:=catalogPolicy()
    cases:=[]struct{
        action string
        args map[string]any
        tool string
    }{
        {"infrastructure__workspace_register_image",
            map[string]any{"host":"saturn","image_id":"builder-image",
                "image":policy.ImageReferences[0],"architecture":"amd64",
                "description":"Pinned build image"},
            "pool_register_image"},
        {"infrastructure__workspace_propose_program",
            map[string]any{"host":"saturn","id":"compiler","name":"A Compiler",
                "version":"1.0","platform":"linux-container","distribution":"oci",
                "source_url":"https://example.org/compiler","license_notes":"MIT",
                "description":"Static compiler catalog metadata"},
            "pool_propose_program"},
        {"infrastructure__workspace_review_program",
            map[string]any{"host":"saturn","id":"compiler","status":"reviewed",
                "review_note":"Licensing verified","expected_revision":1,
                "pool_kind":"","pool_id":""},
            "pool_review_program"},
        {"infrastructure__workspace_review_program",
            map[string]any{"host":"saturn","id":"compiler","status":"available",
                "review_note":"Pinned image available","expected_revision":2,
                "pool_kind":"image","pool_id":"builder-image"},
            "pool_review_program"},
        {"infrastructure__workspace_update_entry",
            map[string]any{"host":"saturn","kind":"software","id":"toolchain",
                "status":"disabled","reason":"Vulnerability","expected_revision":1},
            "pool_update_entry"},
    }
    for _,tc:=range cases{
        name,args,err:=policy.operationArgs(tc.action,tc.args)
        if err!=nil||name!=tc.tool{
            t.Fatalf("%s expected %s, got %s (%v)",tc.action,tc.tool,name,err)
        }
        if _,ok:=args["host"];ok{
            t.Errorf("%s exposed executor host as controller metadata",tc.action)
        }
        changed:=map[string]any{}
        for key,value:=range tc.args{changed[key]=value}
        changed["unexpected"]="escalation"
        if _,_,err:=policy.operationArgs(tc.action,changed);err==nil{
            t.Errorf("%s accepted extra fields",tc.action)
        }
    }
}

func TestWorkspaceCatalogNeverBroadensImmutableImageOrProgramPolicy(t *testing.T){
    policy:=catalogPolicy()
    image:=map[string]any{"host":"saturn","image_id":"builder-image",
        "image":policy.ImageReferences[0],"architecture":"amd64"}
    for _,reference:=range []string{
        "registry.example/builder:latest",
        "registry.example/builder@sha256:"+strings.Repeat("a",63),
        "registry.example/builder@sha256:"+strings.Repeat("G",64),
        "registry.example/builder@sha256:"+strings.Repeat("b",64),
        "@sha256:"+strings.Repeat("a",64),
    }{
        image["image"]=reference
        if _,_,err:=policy.operationArgs("infrastructure__workspace_register_image",image);err==nil{
            t.Fatalf("unapproved immutable image accepted: %q",reference)
        }
    }
    proposal:=map[string]any{"host":"saturn","id":"compiler",
        "name":"A Compiler","platform":"linux-container",
        "distribution":"apt","source_url":"https://vendor.example/tools"}
    if _,_,err:=policy.operationArgs("infrastructure__workspace_propose_program",proposal);err!=nil{
        t.Fatal("allowlisted program proposal rejected:",err)
    }
    for _,source:=range []string{
        "http://vendor.example/tools","https://u:p@vendor.example/tools",
        "https://vendor.example/tools#unverified","file:///etc/passwd",
    }{
        proposal["source_url"]=source
        if _,_,err:=policy.operationArgs("infrastructure__workspace_propose_program",proposal);err==nil{
            t.Fatalf("untrusted program URL accepted: %q",source)
        }
    }
    proposal["source_url"]="https://vendor.example/tools"
    proposal["id"]="unlisted"
    if _,_,err:=policy.operationArgs("infrastructure__workspace_propose_program",proposal);err==nil{
        t.Fatal("unlisted program admitted")
    }
    review:=map[string]any{"host":"saturn","id":"compiler","status":"available",
        "review_note":"Reviewed","pool_kind":"image","pool_id":"unlisted","expected_revision":2}
    if _,_,err:=policy.operationArgs("infrastructure__workspace_review_program",review);err==nil{
        t.Fatal("available program linked an unapproved image")
    }
    review["pool_id"]="builder-image"
    review["expected_revision"]=0
    if _,_,err:=policy.operationArgs("infrastructure__workspace_review_program",review);err==nil{
        t.Fatal("program review omitted expected revision")
    }
    update:=map[string]any{"host":"saturn","kind":"software",
        "id":"unlisted","status":"disabled","reason":"Retire","expected_revision":1}
    if _,_,err:=policy.operationArgs("infrastructure__workspace_update_entry",update);err==nil{
        t.Fatal("unlisted software entry could be disabled")
    }
}
