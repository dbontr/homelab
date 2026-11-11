package transport

import (
    "encoding/json"
    "io"
    "math"
    "net/http"
    "net/url"
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

// Privileged Compute mutations cannot use direct Workspace tool forwarding.
// The executor owns its separate controller credential and validates exact
// host, resource allowlists, owner confirmation and a durable audit record.
func (s *Server) computeExecutorWrite(w http.ResponseWriter,r *http.Request,operation string,args any){
    if s.executorClient==nil{
        writeError(w,http.StatusServiceUnavailable,"independent executor is not configured")
        return
    }
    raw,err:=json.Marshal(args)
    if err!=nil{writeError(w,400,"invalid privileged Compute arguments");return}
    ctx:=security.WithAccessAssertion(r.Context(),r.Header.Get("Cf-Access-Jwt-Assertion"))
    output:=s.invoke(ctx,operation,raw,json.RawMessage("1"))
    if failed,_:=output["isError"].(bool);failed{
        writeError(w,http.StatusForbidden,"independent executor did not authorize this operation")
        return
    }
    parts,ok:=output["content"].([]any)
    if !ok||len(parts)!=1{
        writeError(w,502,"invalid executor response");return
    }
    item,ok:=parts[0].(map[string]any)
    if !ok{writeError(w,502,"invalid executor response");return}
    serialized,ok:=item["text"].(string)
    if !ok||len(serialized)>16<<10{
        writeError(w,502,"invalid executor response");return
    }
    var data map[string]any
    if json.Unmarshal([]byte(serialized),&data)!=nil||data==nil{
        writeError(w,502,"invalid executor result");return
    }
    if data["authorization_required"]==true{
        writeJSON(w,http.StatusPreconditionRequired,data);return
    }
    if data["accepted"]!=true&&data["completed"]!=true{
        writeError(w,502,"executor returned no accepted audit outcome");return
    }
    writeJSON(w,http.StatusAccepted,data)
}

type consoleWorkspaceRequest struct{
    Host string `json:"host,omitempty"`
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
        (req.NodeID!=""&&!consoleProjectID.MatchString(req.NodeID))||
        (req.ImagePoolID!=""&&!consoleProjectID.MatchString(req.ImagePoolID))||
        (req.Environment!=""&&!consoleProjectID.MatchString(req.Environment))||
        (req.Architecture!=""&&req.Architecture!="amd64"&&req.Architecture!="arm64")||
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
        if kind!="gpu"&&kind!="npu"&&kind!="fpga"&&kind!="usb" || quantity<0||quantity>2{
            return false
        }
    }
    if req.USBPassthrough&&req.Hardware["usb"]>1||
       !req.USBPassthrough&&req.Hardware["usb"]>0{
       return false
    }
    if req.MinGPUMemoryMB>0&&req.Hardware["gpu"]<1{return false}
    if req.Display&&(req.DisplayWidth<320||req.DisplayWidth>3840||
        req.DisplayHeight<240||req.DisplayHeight>2160||
        req.DisplayWidth*req.DisplayHeight>8_000_000) {
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
    if !consoleProjectID.MatchString(in.Host)||
        in.ImagePoolID==""||in.Image!=""||in.Environment!=""{
        writeError(w,400,"workspace creation requires an exact executor host and approved image pool")
        return
    }
    s.computeExecutorWrite(w,r,"infrastructure__workspace_create",in)
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
    // Planning does not invoke the privileged executor. Host identity is
    // relevant only when the owner explicitly requests an allocation.
    in.Host=""
    s.computeWrite(w,r,"resource_plan",in,http.StatusOK)
}

func (s *Server) uiComputeWorkspaceStop(w http.ResponseWriter,r *http.Request){
    workspace:=r.PathValue("workspace")
    var in struct{
        Host string `json:"host"`
        ProjectID string `json:"project_id"`
        Confirm bool `json:"confirm"`
    }
    if err:=decodeComputeRequest(w,r,&in);err!=nil||!in.Confirm||
        !consoleProjectID.MatchString(in.Host)||
        !consoleProjectID.MatchString(in.ProjectID)||!learningID.MatchString(workspace){
        writeError(w,400,"workspace stop requires an exact executor host, project and confirmation");return
    }
    s.computeExecutorWrite(w,r,"infrastructure__workspace_stop",map[string]any{
        "host":in.Host,"project_id":in.ProjectID,"workspace_id":workspace,
    })
}

type consoleCatalogRequest struct{
    ID string `json:"id,omitempty"`
    ImageID string `json:"image_id,omitempty"`
    SoftwareID string `json:"software_id,omitempty"`
    Host string `json:"host,omitempty"`
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
// An OCI catalog entry must identify exactly one immutable content digest.
func validImmutableImage(image string) bool {
    prefix,digest,found:=strings.Cut(image,"@sha256:")
    if !found||len(prefix)==0||len(digest)!=64{
        return false
    }
    for _,r:=range digest{
        if r<'0'||r>'9'&&r<'a'||r>'f'{
            return false
        }
    }
    return true
}

func validConsoleProgramDistribution(platform,distribution string)bool{
    return platform=="linux-container"&&(distribution=="apt"||distribution=="oci")||
        (platform=="windows-native"||platform=="macos-native")&&distribution=="native"
}
func validConsoleProgramSource(raw string)bool{
    if len(raw)>512||strings.ContainsAny(raw," \t\n\r\\") {return false}
    uri,err:=url.Parse(raw)
    return err==nil&&uri.Scheme=="https"&&uri.Hostname()!=""&&
        uri.User==nil&&uri.Fragment==""
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
    if !consoleProjectID.MatchString(in.Host) {
        writeError(w,400,"independent executor host is required")
        return
    }
    operation:=r.PathValue("operation")
    action:=map[string]any{"host":in.Host}
    tool:=""
    switch operation{
    case "register-image":
        if !catalogID(in.ID)||len(in.Image)>256||len(in.Description)>512||
            !validImmutableImage(in.Image)||
            in.Architecture!=""&&in.Architecture!="amd64"&&in.Architecture!="arm64"{
            writeError(w,400,"image registration requires an approved immutable digest");return
        }
        tool="infrastructure__workspace_register_image"
        action["image_id"]=in.ID
        action["image"]=in.Image
        action["description"]=in.Description
        action["architecture"]=in.Architecture
    case "register-software":
        if !catalogID(in.ID)||!catalogID(in.ImageID)||len(in.Packages)==0||
            len(in.Packages)>64||len(in.Description)>512{
            writeError(w,400,"invalid software collection");return
        }
        for _,pkg:=range in.Packages{
            if pkg==""||len(pkg)>80||strings.ContainsAny(pkg," \t\r\n;/$"){
                writeError(w,400,"invalid package name");return
            }
        }
        tool="infrastructure__workspace_register_software"
        action["software_id"]=in.ID
        action["image_id"]=in.ImageID
        action["packages"]=in.Packages
        if in.Description!=""{action["description"]=in.Description}
    case "propose-program":
        if !catalogID(in.ID)||strings.TrimSpace(in.Name)==""||len(in.Name)>128||
            len(in.Description)>1024||len(in.Version)>64||
            len(in.LicenseNotes)>1024||len(in.SourceURL)>512||
            !validConsoleProgramSource(in.SourceURL)||
            !validConsoleProgramDistribution(in.Platform,in.Distribution){
            writeError(w,400,"program proposal metadata is invalid");return
        }
        tool="infrastructure__workspace_propose_program"
        action["id"]=in.ID
        action["name"]=in.Name
        action["version"]=in.Version
        action["platform"]=in.Platform
        action["distribution"]=in.Distribution
        action["description"]=in.Description
        action["source_url"]=in.SourceURL
        action["license_notes"]=in.LicenseNotes
    case "review-program":
        if !catalogID(in.ID)||in.ExpectedRevision<1||
            in.ExpectedRevision>1_000_000||strings.TrimSpace(in.ReviewNote)==""||
            len(in.ReviewNote)>512||
            (in.Status!="reviewed"&&in.Status!="available"&&in.Status!="disabled"&&
                in.Status!="rejected"&&in.Status!="retired"){
            writeError(w,400,"invalid versioned program review");return
        }
        if in.Status=="available"{
            if (in.PoolKind!="image"&&in.PoolKind!="software")||
                !catalogID(in.PoolID){
                writeError(w,400,"available program requires an approved catalog entry");return
            }
        } else if in.PoolKind!=""||in.PoolID!=""{
            writeError(w,400,"only available programs may reference a pool");return
        }
        tool="infrastructure__workspace_review_program"
        action["id"]=in.ID
        action["status"]=in.Status
        action["review_note"]=in.ReviewNote
        action["pool_kind"]=in.PoolKind
        action["pool_id"]=in.PoolID
        action["expected_revision"]=in.ExpectedRevision
    case "update-entry":
        if !catalogID(in.ID)||in.ExpectedRevision<1||
            in.ExpectedRevision>1_000_000||
            strings.TrimSpace(in.Reason)==""||len(in.Reason)>512||
            (in.Kind!="image"&&in.Kind!="software")||
            (in.Status!="active"&&in.Status!="disabled"&&in.Status!="retired"){
            writeError(w,400,"invalid versioned catalog state change");return
        }
        tool="infrastructure__workspace_update_entry"
        action["kind"]=in.Kind
        action["id"]=in.ID
        action["status"]=in.Status
        action["expected_revision"]=in.ExpectedRevision
        action["reason"]=in.Reason
    default:
        writeError(w,404,"unknown Compute catalog action");return
    }
    s.computeExecutorWrite(w,r,tool,action)
}
