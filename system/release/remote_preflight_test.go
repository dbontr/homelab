package release

import (
 "context"
 "encoding/json"
 "net/http"
 "strings"
 "testing"
)

func TestRemoteReleasePreviewForwardsExactScopeWithoutExecuting(t *testing.T){
 id:=strings.Repeat("a",48)
 calls:=0
 handler:=http.HandlerFunc(func(w http.ResponseWriter,r *http.Request){
  calls++
  if r.URL.Path!="/v1/release/preview"||r.Header.Get("Cf-Access-Jwt-Assertion")!="signed-owner-assertion"{
   t.Fatal("untrusted release preflight")
  }
  var req struct{Host string `json:"host"`; ReleaseID string `json:"release_id"`;Action string `json:"action"`}
  if err:=json.NewDecoder(r.Body).Decode(&req);err!=nil||req.Host!="jupiter"||req.ReleaseID!=id||req.Action!="activate"{
   t.Fatal("unexpected preflight request",req,err)
  }
  _=json.NewEncoder(w).Encode(map[string]any{"allowed":false,"reason":"confirmation required","confirmation_id":"pending-exact"})
 })
 cfg,closeServer:=remoteFixture(t,handler);defer closeServer()
 agent,err:=NewRemoteReleaseAgent(cfg);if err!=nil{t.Fatal(err)}
 if _,err:=agent.Preview(context.Background(),"bad-id","activate");err==nil||calls!=0{
  t.Fatal("unscoped preflight reached host")
 }
 preview,err:=agent.Preview(context.Background(),id,"activate")
 if err!=nil||preview.Allowed||preview.ConfirmationID!="pending-exact"||
  preview.Operation!="infrastructure__release_activate"||calls!=1{
  t.Fatal("incorrect preflight result",preview,err)
 }
}
