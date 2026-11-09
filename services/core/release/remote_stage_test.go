package release

import (
 "context"
 "encoding/json"
 "net/http"
 "strings"
 "testing"
)

func TestRemoteStageValidatesSourceAndResponse(t *testing.T){
 rev:=strings.Repeat("b",40)
 id:=strings.Repeat("a",48)
 calls:=0
 handler:=http.HandlerFunc(func(w http.ResponseWriter,r *http.Request){
  calls++
  if r.URL.Path!="/v1/release/stage"||r.Header.Get("Cf-Access-Jwt-Assertion")!="signed-owner-assertion"{t.Fatal("untrusted stage request")}
  var request struct{Host string `json:"host"`;Revision string `json:"revision"`}
  if err:=json.NewDecoder(r.Body).Decode(&request);err!=nil||request.Host!="jupiter"||request.Revision!=rev{
   t.Fatal("wrong stage request",request,err)
  }
  _=json.NewEncoder(w).Encode(map[string]any{"allowed":true,"manifest":Manifest{Host:"jupiter",Revision:rev,ID:id,System:"/nix/store/staged-system"}})
 })
 cfg,closeServer:=remoteFixture(t,handler);defer closeServer()
 agent,err:=NewRemoteReleaseAgent(cfg);if err!=nil{t.Fatal(err)}
 if _,err:=agent.Stage(context.Background(),"main");err==nil||calls!=0{t.Fatal("invalid revision sent")}
 manifest,err:=agent.Stage(context.Background(),rev)
 if err!=nil||manifest.ID!=id||calls!=1{t.Fatal("stage failed",manifest,err)}
}
func TestRemoteStageRejectsConflictingHostManifest(t *testing.T){
 rev:=strings.Repeat("b",40)
 handler:=http.HandlerFunc(func(w http.ResponseWriter,r *http.Request){
  _=json.NewEncoder(w).Encode(map[string]any{"allowed":true,"manifest":Manifest{Host:"saturn",Revision:rev,ID:strings.Repeat("a",48),System:"/nix/store/other"}})
 })
 cfg,closeServer:=remoteFixture(t,handler);defer closeServer()
 agent,err:=NewRemoteReleaseAgent(cfg);if err!=nil{t.Fatal(err)}
 if _,err:=agent.Stage(context.Background(),rev);err==nil{t.Fatal("host substitution accepted")}
}
