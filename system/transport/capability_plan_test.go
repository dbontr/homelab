package transport

import (
 "context"
 "encoding/json"
 "fmt"
 "net/http"
 "net/http/httptest"
 "strings"
 "testing"

 "github.com/dbontr/homelab-new/system/agentflow"
)

func TestProposedModelPlanRequiresIndependentSubmission(t *testing.T){
 server,_,_:=flowFixture(t)
 model:=httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter,r *http.Request){
  if r.URL.Path!="/v1/chat/completions" {t.Errorf("unexpected model endpoint: %s",r.URL);return}
  if r.Method!=http.MethodPost {t.Errorf("model requested with %s",r.Method);return}
  var req map[string]any
  if err:=json.NewDecoder(r.Body).Decode(&req);err!=nil{t.Error(err);return}
  if req["model"]!="planner-test"{t.Errorf("wrong model: %#v",req["model"])}
  content:=`{"steps":[{"id":"context","action":"project_context"},{"id":"sources","action":"research_search","query":"fourier","depends_on":["context"]}]}`
  _=json.NewEncoder(w).Encode(map[string]any{"choices":[]any{map[string]any{"finish_reason":"stop","message":map[string]any{"content":content}}}})
 }))
 defer model.Close()
 author,err:=agentflow.NewModelAuthor(model.URL+"/v1/chat/completions","planner-test","")
 if err!=nil{t.Fatal(err)}
 server.SetCapabilityPlanner(author)
 response:=request(server.Handler(),"POST","/v1/workflows/plan",`{"project":"study","objective":"Find Fourier evidence"}`,strings.Repeat("z",40))
 if response.Code!=200{t.Fatalf("planning: %d %s",response.Code,response.Body.String())}
 var proposal CapabilityPlanProposal
 if err:=json.Unmarshal(response.Body.Bytes(),&proposal);err!=nil{t.Fatal(err)}
 if !proposal.RequiresOwnerSubmission||len(proposal.Program.Steps)!=2{t.Fatalf("unexpected proposal: %+v",proposal)}
 records,err:=server.workflows.List()
 if err!=nil||len(records)!=0{t.Fatalf("model secretly submitted workflow: %v %v",records,err)}
 saved,err:=server.submitCapabilityWorkflow(context.Background(),proposal.Program)
 if err!=nil||saved.Status!="pending"{t.Fatalf("owner submission failed: %v %v",saved,err)}
}

func TestUntrustedModelPlanCannotExpandPrivileges(t *testing.T){
 for _,tc:=range []struct{output string; personal bool}{
  {`{"steps":[{"id":"unsafe","action":"infrastructure__service_restart"}]}`,true},
  {`{"steps":[{"id":"private","action":"health_goals"}]}`,false},
  {`{"steps":[{"id":"one","action":"project_context","depends_on":["two"]},{"id":"two","action":"project_context","depends_on":["one"]}]}`,true},
  {`{"steps":[{"id":"safe","action":"project_context"}],"tool":"arbitrary"}`,true},
 } {
  t.Run(fmt.Sprintf("case_%d_%t",len(tc.output),tc.personal),func(t *testing.T){
   server,_,_:=flowFixture(t)
   model:=httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter,r *http.Request){
    _=json.NewEncoder(w).Encode(map[string]any{"choices":[]any{map[string]any{"finish_reason":"stop","message":map[string]any{"content":tc.output}}}})
   }))
   defer model.Close()
   author,err:=agentflow.NewModelAuthor(model.URL+"/v1/chat/completions","planner-test","")
   if err!=nil{t.Fatal(err)}
   server.SetCapabilityPlanner(author)
   _,err=server.proposeCapabilityWorkflow(context.Background(),CapabilityPlanInput{Project:"study",Objective:"Study",PersonalRead:tc.personal})
   if err==nil{t.Fatal("untrusted model gained privilege")}
  })
 }
}
