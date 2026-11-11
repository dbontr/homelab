package worker

import (
    "context"
    "errors"
    "fmt"
    "os"
    "os/exec"
    "path/filepath"
    "strconv"
    "strings"
    "time"
)

// Native hardware custody is an explicit operator-selected Docker worker
// mode. The controller still owns project authorization and lease scheduling.
// The native worker owns only host-local device identity and exclusion.
type nativeDeviceLease struct {
    kind string
    alias string
}

func nativeCustodySelected() bool {
    return os.Getenv("PWM_NATIVE_DEVICE_CUSTODY") == "1"
}

func nativeCustodyCommand(ctx context.Context, verb string, args ...string) (string,error) {
    binary:=os.Getenv("PWM_NATIVE_HARDWARE_HELPER")
    if binary=="" || !filepath.IsAbs(binary) || filepath.Clean(binary)!=binary {
        return "",errors.New("native device custody requires the operator-selected Rust helper")
    }
    if ctx==nil {return "",errors.New("device custody requires a request context")}
    cmdCtx,cancel:=context.WithTimeout(ctx,20*time.Second)
    defer cancel()
    argv:=append([]string{"custody-v1",verb},args...)
    command:=exec.CommandContext(cmdCtx,binary,argv...)
    command.Env=[]string{"LC_ALL=C"}
    for _,pair:=range []struct{source,target string}{
        {"HOMELAB_NATIVE_DEVICE_POLICY","HOMELAB_NATIVE_DEVICE_POLICY"},
        {"PWM_NATIVE_USB_LSBLK","HOMELAB_NATIVE_LSBLK"},
        {"HOMELAB_NATIVE_CUSTODY_DIR","HOMELAB_NATIVE_CUSTODY_DIR"},
    }{
        if value:=os.Getenv(pair.source);value!=""{
            if !filepath.IsAbs(value) || filepath.Clean(value)!=value {
                return "",errors.New("native custody path must be an operator-controlled absolute path")
            }
            command.Env=append(command.Env,pair.target+"="+value)
        }
    }
    output,err:=command.Output()
    if err!=nil||len(output)>512{
        return "",fmt.Errorf("native device %s failed",verb)
    }
    wire:=strings.TrimSpace(string(output))
    if !strings.HasPrefix(wire,"custody-v1\n"){
        return "",errors.New("native device custody returned an incompatible protocol version")
    }
    payload:=strings.TrimPrefix(wire,"custody-v1\n")
    switch verb {
    case "acquire-device","verify-device":
        fields:=strings.Split(payload,"\n")
        if len(fields)!=2||!filepath.IsAbs(fields[0])||
            !strings.HasPrefix(fields[0],"/dev/"){
            return "",errors.New("native device custody returned invalid device identity")
        }
        if _,err:=strconv.ParseUint(fields[1],10,32);err!=nil{
            return "",errors.New("native device custody returned invalid device group")
        }
    case "verify-workspace","release-workspace":
        if _,err:=strconv.ParseUint(payload,10,16);err!=nil{
            return "",errors.New("native device custody returned invalid count")
        }
    case "release-device":
        if payload!="released"{
            return "",errors.New("native device custody did not confirm the exact release")
        }
    default:
        return "",errors.New("unknown native custody action")
    }
    return payload,nil
}

func approvedDeviceClaims(claims []DeviceClaim,devices []Device,allowUSB bool) ([]nativeDeviceLease,error){
    if len(claims)>8{return nil,errors.New("too many device claims")}
    known:=make(map[string]Device,len(devices))
    for _,device:=range devices{
        if _,found:=known[device.ID];found{
            return nil,errors.New("duplicate device in local admission policy")
        }
        known[device.ID]=device
    }
    leases:=make([]nativeDeviceLease,0,len(claims))
    used:=make(map[string]bool)
    usedPaths:=make(map[string]bool)
    for _,claim:=range claims{
        device,ok:=known[claim.ID]
        if !ok||used[claim.ID]||device.Kind!=claim.Kind{
            return nil,errors.New("leased device does not match worker policy")
        }
        used[claim.ID]=true
        if device.Kind=="usb"&&!allowUSB{
            return nil,errors.New("USB custody requires operator and workspace opt-in")
        }
        // CDI devices are exclusively assigned through a device plugin. This
        // local helper cannot claim or revoke their kernel resource handles.
        if device.CDI!=""{
            return nil,errors.New("CDI resource requires an independently enforced device-plugin lease")
        }
        if len(device.Paths)==0||len(device.Paths)>8{
            return nil,errors.New("device custody requires a bounded local path list")
        }
        for _,alias:=range device.Paths{
            if !filepath.IsAbs(alias)||filepath.Clean(alias)!=alias||
                !strings.HasPrefix(alias,"/dev/"){
                return nil,errors.New("invalid native device alias")
            }
            if usedPaths[alias]{
                return nil,errors.New("multiple native claims refer to the same device alias")
            }
            usedPaths[alias]=true
            leases=append(leases,nativeDeviceLease{kind:device.Kind,alias:alias})
        }
    }
    return leases,nil
}

func rollbackNativeLeases(leases []nativeDeviceLease,workspace string) error{
    var outcomes []error
    for i:=len(leases)-1;i>=0;i--{
        leased:=leases[i]
        ctx,cancel:=context.WithTimeout(context.Background(),10*time.Second)
        _,err:=nativeCustodyCommand(ctx,"release-device",leased.kind,leased.alias,workspace)
        cancel()
        if err!=nil{outcomes=append(outcomes,err)}
    }
    return errors.Join(outcomes...)
}

// acquireNativeLeases rejects a partial allocation and attempts to roll back
// only the records created by this call. Unknown release outcomes stay fenced
// in Rust rather than being silently reassigned.
func acquireNativeLeases(ctx context.Context,workspace string,leases []nativeDeviceLease) error{
    acquired:=make([]nativeDeviceLease,0,len(leases))
    for _,lease:=range leases{
        _,err:=nativeCustodyCommand(ctx,"acquire-device",lease.kind,lease.alias,workspace)
        if err!=nil{
            cleanup:=rollbackNativeLeases(acquired,workspace)
            return errors.Join(err,cleanup)
        }
        acquired=append(acquired,lease)
    }
    return nil
}

func verifyNativeLeases(ctx context.Context,workspace string,leases []nativeDeviceLease) error{
    for _,lease:=range leases{
        if _,err:=nativeCustodyCommand(ctx,"verify-device",
            lease.kind,lease.alias,workspace);err!=nil{
            return err
        }
    }
    return nil
}

func verifyNativeWorkspace(ctx context.Context,workspace string)error{
    if !nativeCustodySelected(){return nil}
    output,err:=nativeCustodyCommand(ctx,"verify-workspace",workspace)
    if err!=nil{return err}
    if _,err:=strconv.ParseUint(output,10,16);err!=nil{
        return errors.New("native custody helper returned invalid verification receipt")
    }
    return nil
}

func releaseNativeWorkspace(workspace string) error {
    if !nativeCustodySelected(){return nil}
    ctx,cancel:=context.WithTimeout(context.Background(),20*time.Second)
    defer cancel()
    output,err:=nativeCustodyCommand(ctx,"release-workspace",workspace)
    if err!=nil{return err}
    if _,err:=strconv.ParseUint(output,10,16);err!=nil{
        return errors.New("native custody helper returned an invalid release receipt")
    }
    return nil
}

// A successful rm is insufficient proof of device-handle release if Docker
// subsequently reports an existing managed container with the same identity.
func (r *Runner) assertWorkspaceContainerAbsent(ctx context.Context,id string)error{
    n,err:=name(id)
    if err!=nil{return err}
    output,stderr,status,err:=r.docker(ctx,20*time.Second,nil,"ps","-a",
        "--filter","name=^/"+n+"$","--format","{{.Names}}")
    if err!=nil{return fmt.Errorf("container custody inspection unavailable: %w",err)}
    if status!=0{
        return fmt.Errorf("container custody inspection returned %d: %s",status,stderr)
    }
    if strings.TrimSpace(string(output))!=""{
        return errors.New("managed workspace container still holds device access")
    }
    return nil
}
