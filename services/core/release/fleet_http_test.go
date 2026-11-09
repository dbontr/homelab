package release

import (
 "bytes"
 "context"
 "encoding/json"
 "net/http/httptest"
 "os"
 "path/filepath"
 "strings"
 "testing"

 "github.com/dbontr/homelab-new/system/security"
)

type fleetHTTPAuthorizer struct{allow bool;calls int;operations []string}
func(a *fleetHTTPAuthorizer)Authorize(ctx context.Context,op,target string,args json.RawMessage)(security.Authorization,error){
 a.calls++
 a.operations=append(a.operations,op)
 if security.AccessAssertion(ctx)!="signed-owner"||target!="fleet"{return security.Authorization{Reason:"invalid owner"},nil}
 return security.Authorization{Allowed:a.allow,Reason:"confirmation required"},nil
}
func testFleetHTTP(t *testing.T)(*FleetService,*orchestratorFake){
 t.Helper()
 dir:=t.TempDir()
 if err:=os.Chmod(dir,0700);err!=nil{t.Fatal(err)}
 agent:=&orchestratorFake{stageFake:stageFake{host:"jupiter"}}
 o:=&FleetOrchestrator{
  Fleet:&DurableFleet{StateFile:filepath.Join(dir,"fleet.json"),Agents:map[string]HostReleaseAgent{"jupiter":agent}},
  StageTargets:[]FleetStageTarget{{Host:"jupiter",Role:"compute"}},
  StageAgents:map[string]FleetStageAgent{"jupiter":agent},
  PlanFile:filepath.Join(dir,"plan.json"),
 }
 return &FleetService{Controller:o,Authorizer:&fleetHTTPAuthorizer{}},agent
}
func fleetHTTPRequest(s *FleetService,method,path,body,assertion string)*httptest.ResponseRecorder{
 req:=httptest.NewRequest(method,path,bytes.NewBufferString(body))
 if assertion!=""{req.Header.Set("Cf-Access-Jwt-Assertion",assertion)}
 w:=httptest.NewRecorder();s.Handler().ServeHTTP(w,req);return w
}
func TestFleetServiceCannotStageWithoutIndependentAuthorization(t *testing.T){
 s,agent:=testFleetHTTP(t)
 revision:=strings.Repeat("b",40)
 body:=`{"revision":"`+revision+`"}`
 r:=fleetHTTPRequest(s,"POST","/v1/stage",body,"")
 if r.Code!=403||agent.calls!=0{t.Fatal("unsigned fleet request permitted")}
 r=fleetHTTPRequest(s,"POST","/v1/stage",body,"signed-owner")
 if r.Code!=200||agent.calls!=0{t.Fatal("unapproved staging executed",r.Code)}
 auth:=s.Authorizer.(*fleetHTTPAuthorizer);auth.allow=true
 r=fleetHTTPRequest(s,"POST","/v1/stage",body,"signed-owner")
 if r.Code!=200||agent.calls!=1{t.Fatal("approved staging denied",r.Code,r.Body.String())}
 if !strings.Contains(r.Body.String(),revision){t.Fatal("wrong stage record")}
}
func TestFleetServiceActivatesOnlyStoredPlan(t *testing.T){
 s,agent:=testFleetHTTP(t)
 auth:=s.Authorizer.(*fleetHTTPAuthorizer)
 auth.allow=true
 revision:=strings.Repeat("b",40)
 r:=fleetHTTPRequest(s,"POST","/v1/stage",`{"revision":"`+revision+`"}`,"signed-owner")
 if r.Code!=200{t.Fatal(r.Code,r.Body.String())}
 var stage struct{Result FleetStagePlan `json:"result"`}
 if err:=json.Unmarshal(r.Body.Bytes(),&stage);err!=nil{t.Fatal(err)}
 r=fleetHTTPRequest(s,"POST","/v1/activate",`{"plan_id":"`+strings.Repeat("c",48)+`"}`,"signed-owner")
 if r.Code==200||len(agent.actions)!=0{t.Fatal("unknown plan activated")}
 r=fleetHTTPRequest(s,"POST","/v1/activate",`{"plan_id":"`+stage.Result.ID+`"}`,"signed-owner")
 if r.Code!=200||len(agent.actions)!=1{t.Fatal("staged plan not activated",r.Code,r.Body.String())}
 r=fleetHTTPRequest(s,"GET","/v1/status","","signed-owner")
 if r.Code!=200||!strings.Contains(r.Body.String(),`"complete"`){t.Fatal("fleet status not reported")}
 r=fleetHTTPRequest(s,"DELETE","/v1/plan/"+stage.Result.ID,"","signed-owner")
 if r.Code!=200{t.Fatal("resolved plan could not be cleared",r.Code,r.Body.String())}
}
