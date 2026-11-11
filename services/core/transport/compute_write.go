package transport

import (
    "encoding/json"
    "io"
    "math"
    "net/http"
    "strings"

    "github.com/dbontr/homelab-new/system/security"
)

// A browser session can request only declared Compute operations. The
// independent owner authority evaluates the same exact arguments at dispatch.
func decodeComputeRequest(w http.ResponseWriter,r *http.Request,value any) error {
    decoder:=json.NewDecoder(http.MaxBytesReader(w,r.Body,16<<10))
    decoder.DisallowUnknownFields()
    if err:=decoder.Decode(value);err!=nil{return err}
    var extra any
    if err:=decoder.Decode(&extra);err!=io.EOF{return io.ErrUnexpectedEOF}
    return nil
}

func (s *Server) computeWrite(w http.ResponseWriter,r *http.Request,tool string,args any,success int){
    if !s.hasBackend("workspace") {
        writeError(w,http.StatusServiceUnavailable,"Compute service is not configured")
        return
    }
    raw,err:=json.Marshal(args)
    if err!=nil{writeError(w,400,"invalid Compute request");return}
    ctx:=security.WithAccessAssertion(r.Context(),r.Header.Get("Cf-Access-Jwt-Assertion"))
    result:=s.invoke(ctx,"workspace__"+tool,raw,json.RawMessage("1"))
    if failed,_:=result["isError"].(bool);failed{
        writeError(w,http.StatusForbidden,"Compute operation was not authorized or accepted")
        return
    }
    parts,ok:=result["content"].([]any)
    if !ok||len(parts)!=1{writeError(w,502,"invalid Compute response");return}
    first,ok:=parts[0].(map[string]any)
    if !ok{writeError(w,502,"invalid Compute response");return}
    text,ok:=first["text"].(string)
    if !ok||len(text)>2<<20{writeError(w,502,"invalid Compute response");return}
    var data map[string]any
    if json.Unmarshal([]byte(text),&data)!=nil||data==nil{
        writeError(w,502,"Compute service returned invalid JSON");return
    }
    if required,_:=data["authorization_required"].(bool);required{
        writeJSON(w,http.StatusPreconditionRequired,data)
        return
    }
    if failed,_:=data["isError"].(bool);failed{
        writeError(w,502,"Compute operation was not completed");return
    }
    writeJSON(w,success,data)
}

type consoleWorkspaceRequest struct{
    ProjectID string `json:"project_id"`
    Image string `json:"image,omitempty"`
    Environment string `json:"environment,omitempty"`
    ImagePoolID string `json:"image_pool_id,omitempty"`
    NodeID string `json:"node_id,omitempty"`
    Architecture string `json:"architecture,omitempty"`
    CPU float64 `json:"cpu"`
    MemoryMB int `json:"memory_mb"`
    DurationMinutes int `json:"duration_minutes,omitempty"`
    Hardware map[string]int `json:"hardware,omitempty"`
    USBPassthrough bool `json:"usb_passthrough,omitempty"`
    USBNode string `json:"usb_node,omitempty"`
    USBDeviceID string `json:"usb_device_id,omitempty"`
    MinGPUMemoryMB int `json:"min_gpu_memory_mb,omitempty"`
    Display bool `json:"display,omitempty"`
    DisplayWidth int `json:"display_width,omitempty"`
    DisplayHeight int `json:"display_height,omitempty"`
}

func validConsoleWorkspace(req consoleWorkspaceRequest) bool {
    if !consoleProjectID.MatchString(req.ProjectID)||
        math.IsNaN(req.CPU)||math.IsInf(req.CPU,0)||
        req.CPU<0.1||req.CPU>32||req.MemoryMB<64||req.MemoryMB>131072||
        req.DurationMinutes<1||req.DurationMinutes>1440||
        len(req.Image)>256||len(req.Environment)>80||len(req.ImagePoolID)>80||
        len(req.NodeID)>80||len(req.Architecture)>12||
        len(req.USBNode)>80||len(req.USBDeviceID)>128||
        req.MinGPUMemoryMB<0||req.MinGPUMemoryMB>1000000||
        len(req.Hardware)>4 {
        return false
    }
    if req.Image!=""&&req.ImagePoolID!=""||req.Environment!=""&&req.ImagePoolID!=""{
        return false
    }
    if req.USBPassthrough&&(req.USBNode==""||req.USBDeviceID=="")||
        !req.USBPassthrough&&(req.USBNode!=""||req.USBDeviceID!=""){
        return false
    }
    for kind,quantity:=range req.Hardware{
        if kind!="gpu"&&kind!="npu"&&kind!="fpga"&&kind!="usb" || quantity<0||quantity>8{
            return false
        }
    }
    if req.USBPassthrough&&req.Hardware["usb"]>1||
       !req.USBPassthrough&&req.Hardware["usb"]>0{
       return false
    }
    if req.MinGPUMemoryMB>0&&req.Hardware["gpu"]<1{return false}
    if req.Display&&(req.DisplayWidth<320||req.DisplayWidth>3840||
        req.DisplayHeight<240||req.DisplayHeight>2160) {
        return false
    }
    if !req.Display&&(req.DisplayWidth>0||req.DisplayHeight>0){return false}
    return true
}

func (s *Server) uiComputeWorkspace(w http.ResponseWriter,r *http.Request){
    var in consoleWorkspaceRequest
    if err:=decodeComputeRequest(w,r,&in);err!=nil{
        writeError(w,400,"invalid Compute workspace request");return
    }
    if in.CPU==0{in.CPU=1}
    if in.MemoryMB==0{in.MemoryMB=512}
    if in.DurationMinutes==0{in.DurationMinutes=120}
    if in.Display&&in.DisplayWidth==0&&in.DisplayHeight==0{
        in.DisplayWidth=1440;in.DisplayHeight=900
    }
    if !validConsoleWorkspace(in){
        writeError(w,400,"invalid Compute resource selection");return
    }
    s.computeWrite(w,r,"workspace_create",in,http.StatusAccepted)
}

func (s *Server) uiComputeResourcePlan(w http.ResponseWriter,r *http.Request){
    var in consoleWorkspaceRequest
    if err:=decodeComputeRequest(w,r,&in);err!=nil{
        writeError(w,400,"invalid Compute workspace planning request");return
    }
    if in.CPU==0{in.CPU=1}
    if in.MemoryMB==0{in.MemoryMB=512}
    if in.DurationMinutes==0{in.DurationMinutes=120}
    if in.Display&&in.DisplayWidth==0&&in.DisplayHeight==0{
        in.DisplayWidth=1440;in.DisplayHeight=900
    }
    if !validConsoleWorkspace(in){
        writeError(w,400,"invalid Compute resource selection");return
    }
    s.computeWrite(w,r,"resource_plan",in,http.StatusOK)
}

func (s *Server) uiComputeWorkspaceStop(w http.ResponseWriter,r *http.Request){
    workspace:=r.PathValue("workspace")
    var in struct{
        ProjectID string `json:"project_id"`
        Confirm bool `json:"confirm"`
    }
    if err:=decodeComputeRequest(w,r,&in);err!=nil||!in.Confirm||
        !consoleProjectID.MatchString(in.ProjectID)||!learningID.MatchString(workspace){
        writeError(w,400,"workspace stop requires a valid project and explicit confirmation");return
    }
    s.computeWrite(w,r,"workspace_destroy",map[string]any{
        "project_id":in.ProjectID,"workspace_id":workspace,"confirm":true,
    },http.StatusAccepted)
}

type consoleCatalogRequest struct{
    ID string `json:"id,omitempty"`
    ImageID string `json:"image_id,omitempty"`
    SoftwareID string `json:"software_id,omitempty"`
    Image string `json:"image,omitempty"`
    Architecture string `json:"architecture,omitempty"`
    Description string `json:"description,omitempty"`
    Packages []string `json:"packages,omitempty"`
    Name string `json:"name,omitempty"`
    Version string `json:"version,omitempty"`
    Platform string `json:"platform,omitempty"`
    Distribution string `json:"distribution,omitempty"`
    SourceURL string `json:"source_url,omitempty"`
    LicenseNotes string `json:"license_notes,omitempty"`
    Kind string `json:"kind,omitempty"`
    Status string `json:"status,omitempty"`
    Reason string `json:"reason,omitempty"`
    ReviewNote string `json:"review_note,omitempty"`
    PoolKind string `json:"pool_kind,omitempty"`
    PoolID string `json:"pool_id,omitempty"`
    ExpectedRevision int `json:"expected_revision,omitempty"`
}
func catalogID(id string) bool {
    return id!=""&&len(id)<=80&&id[0]>='a'&&id[0]<='z'&&
        strings.IndexFunc(id,func(r rune)bool{
            return !(r>='a'&&r<='z'||r>='0'&&r<='9'||r=='-')
        })<0
}

func (s *Server) uiComputeCatalog(w http.ResponseWriter,r *http.Request){
    var in consoleCatalogRequest
    if err:=decodeComputeRequest(w,r,&in);err!=nil{
        writeError(w,400,"invalid Compute catalog request");return
    }
    operation:=r.PathValue("operation")
    tool:=""
    var args any
    switch operation{
    case "register-image":
        if !catalogID(in.ID)||len(in.Image)>256||in.Image==""||len(in.Description)>2000||
            !strings.Contains(in.Image,"@sha256:"){
            writeError(w,400,"register an immutable image digest and valid catalog ID");return
        }
        tool="pool_register_image"
        args=map[string]any{"image_id":in.ID,"image":in.Image,
            "description":in.Description,"architecture":in.Architecture}
    case "register-software":
        if !catalogID(in.ID)||!catalogID(in.ImageID)||len(in.Packages)==0||len(in.Packages)>128||
           len(in.Description)>2000{
            writeError(w,400,"invalid software collection");return
        }
        for _,pkg:=range in.Packages{
            if pkg==""||len(pkg)>128||strings.ContainsAny(pkg," \t\n\r;/$"){
                writeError(w,400,"invalid package name");return
            }
        }
        tool="pool_register_software"
        args=map[string]any{"software_id":in.ID,"image_id":in.ImageID,
            "description":in.Description,"packages":in.Packages}
    case "propose-program":
        if !catalogID(in.ID)||in.Name==""||len(in.Name)>250||
            len(in.Description)>2000||len(in.SourceURL)>2048{
            writeError(w,400,"invalid program catalog proposal");return
        }
        tool="pool_propose_program"
        args=map[string]any{"id":in.ID,"name":in.Name,"version":in.Version,
            "platform":in.Platform,"distribution":in.Distribution,
            "source_url":in.SourceURL,"license_notes":in.LicenseNotes,
            "description":in.Description}
    case "review-program":
        if !catalogID(in.ID)||in.ExpectedRevision<1||
            (in.Status!="available"&&in.Status!="rejected"&&in.Status!="retired"){
            writeError(w,400,"invalid versioned program review");return
        }
        tool="pool_review_program"
        args=map[string]any{"id":in.ID,"status":in.Status,"review_note":in.ReviewNote,
            "pool_kind":in.PoolKind,"pool_id":in.PoolID,
            "expected_revision":in.ExpectedRevision}
    case "update-entry":
        if !catalogID(in.ID)||in.ExpectedRevision<1||
            (in.Kind!="image"&&in.Kind!="software")||
            (in.Status!="active"&&in.Status!="disabled"&&in.Status!="retired"){
            writeError(w,400,"invalid versioned catalog state change");return
        }
        tool="pool_update_entry"
        args=map[string]any{"kind":in.Kind,"id":in.ID,"status":in.Status,
            "expected_revision":in.ExpectedRevision,"reason":in.Reason}
    default:
        writeError(w,404,"unknown Compute catalog action");return
    }
    s.computeWrite(w,r,tool,args,http.StatusAccepted)
}
