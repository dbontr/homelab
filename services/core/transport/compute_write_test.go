package transport

import (
    "encoding/json"
    "net/http"
    "net/http/httptest"
    "strings"
    "sync/atomic"
    "testing"
)

func TestComputeBrowserWritesRequireIndependentAuthorization(t *testing.T) {
    var executed atomic.Int32
    backend:=httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter,r *http.Request){
        var rpc rpcRequest
        if err:=json.NewDecoder(r.Body).Decode(&rpc);err!=nil{t.Error(err);return}
        w.Header().Set("Content-Type","application/json")
        switch rpc.Method {
        case "tools/list":
            _,_=w.Write([]byte(`{"jsonrpc":"2.0","id":1,"result":{"tools":[{"name":"workspace_create"},{"name":"workspace_destroy"},{"name":"pool_register_image"},{"name":"pool_update_entry"}]}}`))
        case "tools/call":
            executed.Add(1)
            _,_=w.Write([]byte(`{"jsonrpc":"2.0","id":1,"result":{"content":[{"type":"text","text":"{\"created\":true}"}]}}`))
        default:
            t.Errorf("Unexpected controller request %s",rpc.Method)
        }
    }))
    defer backend.Close()
    token:=strings.Repeat("a",40)
    h:=makeTestServer(t,[]Backend{{Name:"workspace",URL:backend.URL,Token:strings.Repeat("k",32)}})

    for _,tc:=range []struct{path,body string;want int}{
        {"/v1/compute/catalog/register-image",
         `{"id":"go-build","image":"registry.example/go@sha256:`+strings.Repeat("a",64)+`","architecture":"amd64"}`,http.StatusForbidden},
        {"/v1/compute/catalog/update-entry",
         `{"id":"go-build","kind":"image","status":"disabled","reason":"retired","expected_revision":2}`,http.StatusForbidden},
        {"/v1/compute/workspaces/0123456789abcdef0123456789abcdef/stop",
         `{"project_id":"research","confirm":true}`,http.StatusForbidden},
        {"/v1/compute/workspaces",`{"project_id":"research","cpu":2,"memory_mb":2048,"duration_minutes":90}`,http.StatusForbidden},
        {"/v1/compute/catalog/register-image",`{"id":"go-build","image":"registry.example/go:latest"}`,http.StatusBadRequest},
        {"/v1/compute/catalog/update-entry",`{"id":"go-build","kind":"image","status":"disabled","expected_revision":0}`,http.StatusBadRequest},
        {"/v1/compute/catalog/unknown",`{"id":"go-build"}`,http.StatusNotFound},
        {"/v1/compute/workspaces",`{"project_id":"research","cpu":1000,"memory_mb":2048}`,http.StatusBadRequest},
        {"/v1/compute/workspaces",`{"project_id":"research","cpu":2,"memory_mb":2048,"unexpected":"yes"}`,http.StatusBadRequest},
        {"/v1/compute/workspaces/0123456789abcdef0123456789abcdef/stop",
         `{"project_id":"research","confirm":false}`,http.StatusBadRequest},
    } {
        response:=request(h,"POST",tc.path,tc.body,token)
        if response.Code!=tc.want{
            t.Errorf("Compute route %s returned %d, expected %d: %s",
                tc.path,response.Code,tc.want,response.Body.String())
        }
    }
    if executed.Load()!=0{
        t.Fatalf("Unauthenticated Compute mutations reached controller: %d",executed.Load())
    }
}

func TestComputeWorkspaceAdmissionValidatesResourceBounds(t *testing.T) {
    valid:=consoleWorkspaceRequest{ProjectID:"research",CPU:2,MemoryMB:2048,
        DurationMinutes:60,Hardware:map[string]int{"gpu":1},MinGPUMemoryMB:8000}
    if !validConsoleWorkspace(valid){t.Fatal("valid allocation denied")}
    valid.CPU=100
    if validConsoleWorkspace(valid){t.Fatal("excessive CPU allocation accepted")}
    valid.CPU=1
    valid.Hardware=map[string]int{"usb":1}
    if validConsoleWorkspace(valid){t.Fatal("USB allocation without device identity accepted")}
    valid.Hardware=map[string]int{"nvidia-admin":1}
    if validConsoleWorkspace(valid){t.Fatal("unapproved hardware class accepted")}
    valid.Hardware=map[string]int{"gpu":1}
    valid.MinGPUMemoryMB=8000
    valid.USBPassthrough=true
    valid.USBNode="jupiter"
    valid.USBDeviceID="jtag"
    if !validConsoleWorkspace(valid){t.Fatal("explicit USB identity rejected")}
    valid.Architecture="invalid-arch"
    if validConsoleWorkspace(valid){t.Fatal("unsupported architecture accepted")}
    valid.Architecture="arm64"
    valid.NodeID="../jupiter"
    if validConsoleWorkspace(valid){t.Fatal("invalid worker identity accepted")}
    valid.NodeID="jupiter"
    valid.Display=true
    valid.DisplayWidth=3840
    valid.DisplayHeight=2160
    if validConsoleWorkspace(valid){t.Fatal("excessive display allocation accepted")}
    valid.DisplayWidth=1440
    valid.DisplayHeight=900
    if !validConsoleWorkspace(valid){t.Fatal("valid display request rejected")}
}

func TestComputeBrowserCatalogRequiresExactImmutableDigest(t *testing.T){
    if !validImmutableImage("registry.example/go@sha256:"+strings.Repeat("a",64)){
        t.Fatal("exact OCI digest was rejected")
    }
    for _,image:=range []string{
        "registry.example/go:latest",
        "registry.example/go@sha256:"+strings.Repeat("a",63),
        "registry.example/go@sha256:"+strings.Repeat("G",64),
        "registry.example/go@sha256:"+strings.Repeat("a",64)+"-suffix",
        "@sha256:"+strings.Repeat("a",64),
    }{
        if validImmutableImage(image){t.Fatalf("mutable or invalid image accepted: %q",image)}
    }
}
