package agentflow

import (
 "context"
 "encoding/json"
 "net/http"
 "net/http/httptest"
 "sync"
 "testing"
)

func TestModelRouterIsPurposeAndPrivacySpecific(t *testing.T){
 var mu sync.Mutex
 calls:=[]string{}
 remote:=httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter,r *http.Request){
  var in struct{Model string `json:"model"`}
  if err:=json.NewDecoder(r.Body).Decode(&in);err!=nil{http.Error(w,"invalid",400);return}
  mu.Lock();calls=append(calls,in.Model);mu.Unlock()
  w.Header().Set("Content-Type","application/json")
  _=json.NewEncoder(w).Encode(map[string]any{"choices":[]any{
   map[string]any{"finish_reason":"stop","message":map[string]any{
    "content":`{"finish":true,"steps":[]}`}},
  }})
 }))
 defer remote.Close()
 endpoint:=remote.URL+"/v1/chat/completions"
 router,err:=NewModelRouter([]ModelRoute{
  {Name:"plan",Purpose:"planning",Endpoint:endpoint,Model:"planner",LocalOnly:true},
  {Name:"continue",Purpose:"continuation",Endpoint:endpoint,Model:"continuation",LocalOnly:true},
  {Name:"cited",Purpose:"evidence",Endpoint:endpoint,Model:"research",LocalOnly:true},
 })
 if err!=nil{t.Fatal(err)}
 if _,err:=router.ProposeWorkflowStages(context.Background(),"project","Study derivations",false);err!=nil{t.Fatal(err)}
 if _,err:=router.ContinueWorkflowStages(context.Background(),"project","Continue",
  []map[string]any{{"status":"succeeded"}},1);err!=nil{t.Fatal(err)}
 if _,err:=router.ContinueWorkflowStages(context.Background(),"project","Continue",
  []map[string]any{{"evidence":[]string{"source:one@1:1"}}},1);err!=nil{t.Fatal(err)}
 mu.Lock();defer mu.Unlock()
 if len(calls)!=3||calls[0]!="planner"||calls[1]!="continuation"||calls[2]!="research"{
  t.Fatalf("wrong route selection: %+v",calls)
 }
}

func TestModelRouterRefusesUndeclaredEvidenceRecipient(t *testing.T){
 endpoint:="http://127.0.0.1:9001/v1/chat/completions"
 router,err:=NewModelRouter([]ModelRoute{
  {Name:"plan",Purpose:"planning",Endpoint:endpoint,Model:"plan"},
  {Name:"ordinary",Purpose:"continuation",Endpoint:endpoint,Model:"ordinary"},
 })
 if err!=nil{t.Fatal(err)}
 if _,err:=router.ContinueWorkflowStages(context.Background(),"p","objective",
  []map[string]any{{"evidence":"private passage"}},1);err==nil{
  t.Fatal("research evidence was silently routed to an ordinary model")
 }
 if _,err:=NewModelRouter([]ModelRoute{{Name:"external",Purpose:"planning",
  Endpoint:"https://example.com/v1/chat/completions",Model:"external",LocalOnly:true}});err==nil{
  t.Fatal("local-only purpose permitted external endpoint")
 }
 if _,err:=NewModelRouter([]ModelRoute{
  {Name:"first",Purpose:"planning",Endpoint:endpoint,Model:"first"},
  {Name:"second",Purpose:"planning",Endpoint:endpoint,Model:"second"},
 });err==nil{t.Fatal("conflicting purpose routed to two providers")}
 if router.SupportsEvidence(){t.Fatal("router claims undeclared evidence route")}
}
