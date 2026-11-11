package privileged

import (
    "bytes"
    "encoding/json"
    "errors"
    "math"
    "regexp"
)

var workspaceDeviceIdentity=regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.:-]{0,127}$`)

// workspaceAllocation describes only an allowlisted controller operation.
// Host is present solely for exact independent-executor target selection.
type workspaceAllocation struct {
    Host string `json:"host"`
    ProjectID string `json:"project_id"`
    ImagePoolID string `json:"image_pool_id"`
    NodeID string `json:"node_id,omitempty"`
    Architecture string `json:"architecture,omitempty"`
    CPU float64 `json:"cpu"`
    MemoryMB int `json:"memory_mb"`
    DurationMinutes int `json:"duration_minutes"`
    Hardware map[string]int `json:"hardware,omitempty"`
    MinGPUMemoryMB int `json:"min_gpu_memory_mb,omitempty"`
    USBPassthrough bool `json:"usb_passthrough,omitempty"`
    USBNode string `json:"usb_node,omitempty"`
    USBDeviceID string `json:"usb_device_id,omitempty"`
    Display bool `json:"display,omitempty"`
    DisplayWidth int `json:"display_width,omitempty"`
    DisplayHeight int `json:"display_height,omitempty"`
}

func(p *WorkspacePolicy)allocationArgs(fields map[string]any)(map[string]any,error){
    raw,err:=json.Marshal(fields)
    if err!=nil||len(raw)>16<<10{
        return nil,errors.New("Workspace allocation exceeds request limit")
    }
    decoder:=json.NewDecoder(bytes.NewReader(raw))
    decoder.DisallowUnknownFields()
    var input workspaceAllocation
    if err:=decoder.Decode(&input);err!=nil{
        return nil,errors.New("Workspace allocation contains unknown or invalid fields")
    }
    if input.Host==""||!workspaceIdentifier.MatchString(input.Host)||
        !containsID(p.Projects,input.ProjectID)||
        !containsID(p.Images,input.ImagePoolID)||
        input.ImagePoolID==""||!workspaceIdentifier.MatchString(input.ImagePoolID)||
        input.NodeID!=""&&(!workspaceIdentifier.MatchString(input.NodeID)||
            !containsID(p.Workers,input.NodeID))||
        input.Architecture!=""&&input.Architecture!="amd64"&&input.Architecture!="arm64"||
        math.IsNaN(input.CPU)||math.IsInf(input.CPU,0)||
        input.CPU<0.1||input.CPU>32||
        input.MemoryMB<64||input.MemoryMB>131072||
        input.DurationMinutes<1||input.DurationMinutes>1440||
        input.MinGPUMemoryMB<0||input.MinGPUMemoryMB>1000000||
        len(input.Hardware)>4{
        return nil,errors.New("Workspace allocation does not match operator resource policy")
    }
    for kind,count:=range input.Hardware{
        if kind!="gpu"&&kind!="npu"&&kind!="fpga"&&kind!="usb"||
            count<1||count>2{
            return nil,errors.New("Workspace allocation requests an unsupported device count")
        }
    }
    if input.MinGPUMemoryMB>0&&input.Hardware["gpu"]==0{
        return nil,errors.New("GPU memory requires an approved GPU request")
    }
    if input.Hardware==nil{input.Hardware=make(map[string]int)}
    if input.USBPassthrough{
        if input.Hardware["usb"]>1 ||input.USBNode==""||
            !workspaceIdentifier.MatchString(input.USBNode)||
            !containsID(p.Workers,input.USBNode)||
            !workspaceDeviceIdentity.MatchString(input.USBDeviceID)||
            input.NodeID!=""&&input.NodeID!=input.USBNode{
            return nil,errors.New("USB passthrough requires exact allowlisted worker and device")
        }
        input.Hardware["usb"]=1
    }else if input.Hardware["usb"]>0||input.USBNode!=""||input.USBDeviceID!=""{
        return nil,errors.New("USB passthrough is not explicitly allowed")
    }
    if input.Display{
        if input.DisplayWidth<320||input.DisplayWidth>3840||
            input.DisplayHeight<240||input.DisplayHeight>2160||
            input.DisplayWidth*input.DisplayHeight>8_000_000{
            return nil,errors.New("Workspace display exceeds its configured limits")
        }
    }else if input.DisplayWidth!=0||input.DisplayHeight!=0{
        return nil,errors.New("Workspace display dimensions require display opt-in")
    }
    // A validated copy, not the original argument map, is sent downstream.
    return map[string]any{
        "project_id":input.ProjectID,
        "image_pool_id":input.ImagePoolID,
        "node_id":input.NodeID,
        "architecture":input.Architecture,
        "cpu":input.CPU,
        "memory_mb":input.MemoryMB,
        "duration_minutes":input.DurationMinutes,
        "hardware":input.Hardware,
        "min_gpu_memory_mb":input.MinGPUMemoryMB,
        "usb_passthrough":input.USBPassthrough,
        "usb_node":input.USBNode,
        "usb_device_id":input.USBDeviceID,
        "display":input.Display,
        "display_width":input.DisplayWidth,
        "display_height":input.DisplayHeight,
    },nil
}
