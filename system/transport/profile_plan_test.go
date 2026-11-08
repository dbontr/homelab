package transport

import (
 "encoding/json"
 "net/http"
 "net/http/httptest"
 "path/filepath"
 "strings"
 "sync/atomic"
 "testing"

 "github.com/dbontr/homelab-new/system/devices"
 "github.com/dbontr/homelab-new/system/profiles"
 "github.com/dbontr/homelab-new/system/store"
)

func TestProfilePlanningCannotBypassDeviceInventory(t *testing.T){
 var forwarded atomic.Int32
 backend:=httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter,r *http.Request){
   var rpc rpcRequest
   if err:=json.NewDecoder(r.Body).Decode(&rpc);err!=nil{t.Error(err);return}
   if rpc.Method=="tools/list"{
     _,_=w.Write([]byte(`{"jsonrpc":"2.0","id":1,"result":{"tools":[{"name":"resource_plan","description":"Plan resources","inputSchema":{"type":"object"}}]}}`));return
   }
   forwarded.Add(1)
   _,_=w.Write([]byte(`{"jsonrpc":"2.0","id":1,"result":{"content":[{"type":"text","text":"{\"eligible_count\":1,\"recommended_node\":\"worker-jupiter\"}"}],"isError":false}}`))
 }))
 defer backend.Close()
 db,err:=store.Open(filepath.Join(t.TempDir(),"state.json"),"UTC");if err!=nil{t.Fatal(err)}
 s,err:=New(db,strings.Repeat("p",40),[]Backend{{Name:"workspace",URL:backend.URL}});if err!=nil{t.Fatal(err)}
 registry,err:=profiles.Open(filepath.Join(t.TempDir(),"profiles.json"));if err!=nil{t.Fatal(err)}
 s.SetProfiles(registry)
 catalog,err:=devices.New([]devices.Device{{ID:"kv260",Kind:"fpga",Mode:devices.ModeSSH,Connection:"ssh",AllowedOperations:[]string{"connect","inspect"}}});if err!=nil{t.Fatal(err)}
 s.SetDeviceCatalog(catalog)
 token:=strings.Repeat("p",40)
 cpu:=profiles.Spec{ID:"cpu-build",Title:"CPU builder",Steps:[]profiles.Step{{ID:"build",Command:"echo build"}},Requires:profiles.Requirements{CPU:2,MemoryMB:2048}}
 if _,err:=registry.Save(cpu,0);err!=nil{t.Fatal(err)}
 native:=request(s.Handler(),"POST","/v1/compute/profiles/cpu-build/plan",`{"project_id":"engineering"}`,token)
 mcp:=request(s.Handler(),"POST","/mcp",`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"compute_profile_plan","arguments":{"id":"cpu-build","project_id":"engineering"}}}`,token)
 if native.Code!=200||mcp.Code!=200||forwarded.Load()!=2||!strings.Contains(native.Body.String(),"worker-jupiter")||!strings.Contains(mcp.Body.String(),"worker-jupiter"){
    t.Fatalf("profile plan not unified: native=%s mcp=%s",native.Body.String(),mcp.Body.String())
 }
 fpga:=cpu;fpga.ID="fpga-build";fpga.Requires.Hardware=map[string]int{"fpga":1}
 if _,err:=registry.Save(fpga,0);err!=nil{t.Fatal(err)}
 denied:=request(s.Handler(),"POST","/v1/compute/profiles/fpga-build/plan",`{"project_id":"engineering","node_id":"worker-jupiter"}`,token)
 if denied.Code==200||forwarded.Load()!=2{t.Fatalf("agentless FPGA entered resource planning: %s",denied.Body.String())}
}
