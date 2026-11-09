package transport

import (
    "context"
    "encoding/json"
    "net/http"
    "net/http/httptest"
    "path/filepath"
    "strings"
    "testing"

    "github.com/dbontr/homelab-new/system/agentflow"
    "github.com/dbontr/homelab-new/system/store"
)

func TestFederatedCanonicalResourceReadsUseOwningService(t *testing.T) {
    makeDownstream:=func(verb string)*httptest.Server{
        return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter,r *http.Request){
            var req struct{
                Method string `json:"method"`
                Params struct{
                    Name string `json:"name"`
                    Arguments map[string]any `json:"arguments"`
                } `json:"params"`
            }
            if err:=json.NewDecoder(r.Body).Decode(&req);err!=nil{t.Error(err);return}
            var content any
            if req.Method=="tools/list"{
                content=map[string]any{"tools":[]any{map[string]any{"name":verb,"inputSchema":map[string]string{"type":"object"}}}}
            } else if req.Method=="tools/call" {
                if req.Params.Name!=verb{t.Errorf("unauthorized verb %s",req.Params.Name);return}
                payload,_:=json.Marshal(map[string]any{"id":"record-1","status":"active"})
                content=map[string]any{"content":[]any{map[string]string{"type":"text","text":string(payload)}}}
            } else {t.Errorf("unexpected method %s",req.Method);return}
            _=json.NewEncoder(w).Encode(map[string]any{"jsonrpc":"2.0","id":1,"result":content})
        }))
    }
    learning:=makeDownstream("get_learning_session")
    defer learning.Close()
    health:=makeDownstream("health_plan_get")
    defer health.Close()
    db,err:=store.Open(filepath.Join(t.TempDir(),"records.json"),"UTC")
    if err!=nil{t.Fatal(err)}
    server,err:=New(db,strings.Repeat("u",40),[]Backend{
        {Name:"learning",URL:learning.URL},{Name:"health",URL:health.URL},
    })
    if err!=nil{t.Fatal(err)}
    engineering:=&engineeringFixture{run:agentflow.RunSummary{ID:"run-test",Project:"study",Revision:1}}
    server.SetEngineering(engineering)
    for _,tc:=range []struct{kind,owner string}{
        {"learning_session","learning"},{"health_plan","health"},{"engineering_run","engineering"},
    }{
        id:="record-1"
        if tc.kind=="engineering_run"{id="run-test"}
        resolved,err:=server.resource(context.Background(),tc.kind,id)
        if err!=nil||resolved.Owner!=tc.owner||resolved.Reference!=resourceRef(tc.kind,id){
            t.Fatalf("resource %s: %+v %v",tc.kind,resolved,err)
        }
    }
    if _,err:=server.resource(context.Background(),"admin","record-1");err==nil{
        t.Fatal("privileged resource resolved through public resource API")
    }
    if _,err:=server.resource(context.Background(),"health_plan","../private");err==nil{
        t.Fatal("malformed identity was accepted")
    }
}

func TestWorkflowListHidesDetailedObservations(t *testing.T) {
    server,_,_:=flowFixture(t)
    flow,err:=server.submitCapabilityWorkflow(context.Background(),CapabilityProgram{
        Project:"study",Objective:"Examine private context",
        Steps:[]CapabilityStage{{ID:"context",Action:"project_context"}},
    })
    if err!=nil{t.Fatal(err)}
    if err:=server.advanceCapabilityWorkflow(context.Background(),flow.ID,2);err!=nil{t.Fatal(err)}
    listing,err:=server.capabilityWorkflows()
    if err!=nil||len(listing)!=1{t.Fatalf("workflow listing failed: %v",err)}
    if len(listing[0].Steps[0].Result)!=0{t.Fatal("list disclosed detailed result")}
    detail,err:=server.capabilityWorkflow(context.Background(),flow.ID)
    if err!=nil||len(detail.Steps[0].Result)==0{t.Fatal("authorized detail lost verified source result")}
}
