package devices

import (
 "net/http"
 "net/http/httptest"
 "strings"
 "testing"
)

func TestInventoryRoutesAreReadOnlyAndAllowIndependentConfiguration(t *testing.T){
 var catalog *Catalog
 var nodes *NodeCatalog
 mux:=http.NewServeMux()
 Register(mux,func()(*Catalog,*NodeCatalog){return catalog,nodes})
 for _,path:=range []string{"/v1/devices","/v1/nodes"}{
  got:=httptest.NewRecorder();mux.ServeHTTP(got,httptest.NewRequest("GET",path,nil))
  if got.Code!=503{t.Fatalf("%s: expected unconfigured inventory, got %d",path,got.Code)}
 }
 var err error
 catalog,err=New([]Device{{ID:"kv260",Kind:"fpga",Mode:ModeSSH,Connection:"ssh",AllowedOperations:[]string{"inspect"}}})
 if err!=nil{t.Fatal(err)}
 nodes,err=NewNodes([]Node{{ID:"jupiter",Mode:ModeSSH}})
 if err!=nil{t.Fatal(err)}
 for _,tc:=range []struct{path,id string}{{"/v1/devices","kv260"},{"/v1/nodes","jupiter"}}{
  got:=httptest.NewRecorder();mux.ServeHTTP(got,httptest.NewRequest("GET",tc.path,nil))
  if got.Code!=200||!strings.Contains(got.Body.String(),tc.id){t.Fatalf("%s: %d %s",tc.path,got.Code,got.Body.String())}
  rejected:=httptest.NewRecorder();mux.ServeHTTP(rejected,httptest.NewRequest("POST",tc.path,nil))
  if rejected.Code!=http.StatusMethodNotAllowed{t.Fatalf("%s: mutation permitted: %d",tc.path,rejected.Code)}
 }
}
