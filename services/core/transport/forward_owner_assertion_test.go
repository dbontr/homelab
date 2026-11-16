package transport

import (
 "context"
 "encoding/json"
 "fmt"
 "net/http"
 "net/http/httptest"
 "testing"
 "time"

 "github.com/dbontr/homelab-new/system/security"
)

func TestFederationForwardsOwnerIdentityOnlyToSelectedWorkspace(t *testing.T){
 assertion:="signed-owner-assertion"
 received:=[]string{}
 upstream:=httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter,r *http.Request){
  received=append(received,r.Header.Get("Cf-Access-Jwt-Assertion"))
  w.Header().Set("Content-Type","application/json")
  _,_=fmt.Fprint(w,`{"jsonrpc":"2.0","id":17,"result":{"content":[{"type":"text","text":"ok"}]}}`)
 }))
 defer upstream.Close()
 server:=&Server{client:&http.Client{Timeout:5*time.Second}}
 ctx:=security.WithAccessAssertion(context.Background(),assertion)
 request:=rpcRequest{
  JSONRPC:"2.0",ID:json.RawMessage("17"),Method:"tools/call",
  Params:json.RawMessage(`{"name":"project_info","arguments":{}}`),
 }
 for _,backend:=range []Backend{
  {Name:"workspace",URL:upstream.URL,ForwardOwnerAssertion:true},
  {Name:"workspace",URL:upstream.URL},
  {Name:"health",URL:upstream.URL,ForwardOwnerAssertion:true},
 }{
  var result any
  if err:=server.forward(ctx,backend,request,&result);err!=nil{t.Fatal(err)}
 }
 if len(received)!=3||received[0]!=assertion||received[1]!=""||received[2]!=""{
  t.Fatal("owner token was forwarded outside the explicitly selected Workspace backend")
 }
 var result any
 if err:=server.forward(context.Background(),
  Backend{Name:"workspace",URL:upstream.URL,ForwardOwnerAssertion:true},request,&result);err!=nil{
  t.Fatal(err)
 }
 if len(received)!=4||received[3]!=""{
  t.Fatal("missing owner assertion was synthesized or cached")
 }
}

func TestToolDiscoveryForwardsOwnerAssertionOnlyWhenEnabled(t *testing.T) {
 const identity = "signed-owner-assertion"
 upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
  var call struct { Method string `json:"method"` }
  if err := json.NewDecoder(r.Body).Decode(&call); err != nil || call.Method != "tools/list" {
   http.Error(w, "invalid discovery request", http.StatusBadRequest)
   return
  }
  if r.Header.Get("Cf-Access-Jwt-Assertion") != identity {
   http.Error(w, "owner identity required", http.StatusUnauthorized)
   return
  }
  w.Header().Set("Content-Type", "application/json")
  _, _ = fmt.Fprint(w, `{"jsonrpc":"2.0","id":1,"result":{"tools":[{"name":"list_projects","description":"List authorized projects","inputSchema":{"type":"object"}},{"name":"submit_compute_job","description":"Queue a compute job","inputSchema":{"type":"object"}},{"name":"compute_job_status","description":"Inspect a batch job","inputSchema":{"type":"object"}}]}}`)
 }))
 defer upstream.Close()

 cases := []struct {
  name string
  ownerAssertion string
  forward bool
  wantComputeTool bool
 }{
  {"signed owner", identity, true, true},
  {"missing owner", "", true, false},
  {"forwarding disabled", identity, false, false},
 }
 for _, tc := range cases {
  t.Run(tc.name, func(t *testing.T) {
   gateway := &Server{
    client: &http.Client{Timeout: 5*time.Second},
    backend: []Backend{{Name:"workspace", URL: upstream.URL, ForwardOwnerAssertion:tc.forward}},
   }
   req := httptest.NewRequest(http.MethodPost, "/mcp", nil)
   if tc.ownerAssertion != "" {
    req.Header.Set("Cf-Access-Jwt-Assertion", tc.ownerAssertion)
   }
   found := false
   foundStatus := false
   for _, tool := range gateway.toolList(req) {
    if tool["name"] == "compute__list_projects" {
     found = true
    }
    if tool["name"] == "compute__compute_job_status" {
     foundStatus = true
    }
   }
   if found != tc.wantComputeTool || foundStatus != tc.wantComputeTool {
    t.Fatalf("Compute read-only discovery: projects=%t status=%t, want %t", found, foundStatus, tc.wantComputeTool)
   }
   for _, tool := range gateway.toolList(req) {
    if tool["name"] == "compute__submit_compute_job" {
     t.Fatal("privileged compute execution appeared in federated discovery")
    }
   }
  })
 }
}
