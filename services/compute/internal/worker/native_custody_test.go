package worker

import (
    "context"
    "os"
    "path/filepath"
    "strings"
    "testing"
)

func TestNativeDeviceCustodyRequiresExactClaims(t *testing.T) {
    known:=[]Device{
        {ID:"gpu0",Kind:"gpu",Paths:[]string{"/dev/dri/renderD128"}},
        {ID:"usb0",Kind:"usb",Paths:[]string{"/dev/disk/by-id/usb-example"}},
        {ID:"cdi",Kind:"gpu",CDI:"vendor.example.com/device=gpu0"},
    }
    approved,err:=approvedDeviceClaims([]DeviceClaim{{ID:"gpu0",Kind:"gpu"}},known,false)
    if err!=nil||len(approved)!=1||approved[0].alias!="/dev/dri/renderD128"{
        t.Fatalf("approved accelerator was rejected: %+v %v",approved,err)
    }
    for _,claims:=range [][]DeviceClaim{
        {{ID:"unknown",Kind:"gpu"}},
        {{ID:"gpu0",Kind:"npu"}},
        {{ID:"gpu0",Kind:"gpu"},{ID:"gpu0",Kind:"gpu"}},
        {{ID:"usb0",Kind:"usb"}},
        {{ID:"cdi",Kind:"gpu"}},
    }{
        if _,err:=approvedDeviceClaims(claims,known,false);err==nil{
            t.Fatalf("invalid device claim accepted: %+v",claims)
        }
    }
    granted,err:=approvedDeviceClaims([]DeviceClaim{{ID:"usb0",Kind:"usb"}},known,true)
    if err!=nil||len(granted)!=1{t.Fatalf("explicit USB custody rejected: %+v %v",granted,err)}
}

func TestNativeDeviceCustodyRefusesDuplicatePhysicalPaths(t *testing.T) {
    devices:=[]Device{
        {ID:"gpu0",Kind:"gpu",Paths:[]string{"/dev/dri/renderD128"}},
        {ID:"gpu1",Kind:"gpu",Paths:[]string{"/dev/dri/renderD128"}},
    }
    claims:=[]DeviceClaim{
        {ID:"gpu0",Kind:"gpu"}, {ID:"gpu1",Kind:"gpu"},
    }
    if _,err:=approvedDeviceClaims(claims,devices,false);err==nil{
        t.Fatal("two independent workspace claims acquired the same raw device path")
    }
}

func TestNativeDeviceCustodyFailsClosedOnMissingHelper(t *testing.T) {
    t.Setenv("PWM_NATIVE_DEVICE_CUSTODY","1")
    t.Setenv("PWM_NATIVE_HARDWARE_HELPER","")
    if _,err:=nativeCustodyCommand(context.Background(),"verify-workspace",
        "0123456789abcdef0123456789abcdef");err==nil{
        t.Fatal("device custody admitted work without its native executor")
    }
    t.Setenv("PWM_NATIVE_HARDWARE_HELPER","relative/path")
    if _,err:=nativeCustodyCommand(context.Background(),"acquire-device",
        "gpu","/dev/dri/renderD128","0123456789abcdef0123456789abcdef");err==nil{
        t.Fatal("relative native helper was authorized")
    }
}

func TestNativeDeviceCustodyRollbackAndVerificationReceipts(t *testing.T) {
    root:=t.TempDir()
    logPath:=filepath.Join(root,"operations")
    helper:=filepath.Join(root,"native-helper")
    script:="#!/bin/sh\n"+
        "printf '%s\\n' \"$2 $3 $4 $5\" >> '"+logPath+"'\n"+
        "case \"$2\" in\n"+
        "  acquire-device) [ \"$4\" = /dev/fail ] && exit 1; printf 'custody-v1\\n/dev/null\\n0\\n' ;;\n"+
        "  release-device) printf 'custody-v1\\nreleased\\n' ;;\n"+
        "  verify-device) printf 'custody-v1\\n/dev/null\\n0\\n' ;;\n"+
        "  verify-workspace|release-workspace) printf 'custody-v1\\n1\\n' ;;\n"+
        "  *) exit 2 ;;\n"+
        "esac\n"
    if err:=os.WriteFile(helper,[]byte(script),0700);err!=nil{t.Fatal(err)}
    t.Setenv("PWM_NATIVE_HARDWARE_HELPER",helper)
    t.Setenv("PWM_NATIVE_DEVICE_CUSTODY","1")
    workspace:="0123456789abcdef0123456789abcdef"
    leases:=[]nativeDeviceLease{
        {kind:"gpu",alias:"/dev/gpu-ok"},
        {kind:"gpu",alias:"/dev/fail"},
    }
    if err:=acquireNativeLeases(context.Background(),workspace,leases);err==nil{
        t.Fatal("partial hardware claim was accepted")
    }
    log,err:=os.ReadFile(logPath)
    if err!=nil{t.Fatal(err)}
    if !strings.Contains(string(log),"release-device gpu /dev/gpu-ok "+workspace) {
        t.Fatalf("failed allocation did not roll back its acquired device: %s",log)
    }
    if strings.Contains(string(log),"release-device gpu /dev/fail ") {
        t.Fatal("failed allocation attempted to release a device it never acquired")
    }
    if err:=verifyNativeWorkspace(context.Background(),workspace);err!=nil{t.Fatal(err)}
    if err:=releaseNativeWorkspace(workspace);err!=nil{t.Fatal(err)}
}
