package transport

import (
    "context"
    "encoding/json"
    "net/http"
    "net/http/httptest"
    "path/filepath"
    "strings"
    "sync"
    "testing"

    "github.com/dbontr/homelab-new/system/agentflow"
    "github.com/dbontr/homelab-new/system/store"
)

type flowBackend struct {
    mu sync.Mutex
    calls map[string]int
    badScope bool
}
func (f *flowBackend) ServeHTTP(w http.ResponseWriter,r *http.Request) {
    var call struct {
        ID json.RawMessage `json:"id"`
        Method string `json:"method"`
        Params struct {
            Name string `json:"name"`
            Arguments map[string]any `json:"arguments"`
        } `json:"params"`
    }
    if err:=json.NewDecoder(r.Body).Decode(&call);err!=nil{http.Error(w,"invalid",400);return}
    var result any
    switch call.Method {
    case "tools/list":
        result=map[string]any{"tools":[]any{
            map[string]any{"name":"knowledge_context","inputSchema":map[string]any{"type":"object"}},
            map[string]any{"name":"research_search","inputSchema":map[string]any{"type":"object"}},
        }}
    case "tools/call":
        f.mu.Lock()
        f.calls[call.Params.Name]++
        if call.Params.Arguments["project_id"]!="study"{f.badScope=true}
        f.mu.Unlock()
        var payload any
        if call.Params.Name=="knowledge_context" {
            payload=map[string]any{"project":map[string]string{"id":"study","title":"Research"},
                "repositories":[]any{},"libraries":[]any{}}
        } else if call.Params.Name=="research_search" {
            payload=[]map[string]string{{"citation_ref":"source:provenance@1:1","excerpt":"Evidence"}}
        } else {http.Error(w,"tool unknown",400);return}
        data,_:=json.Marshal(payload)
        result=map[string]any{"content":[]any{map[string]string{"type":"text","text":string(data)}}}
    default:http.Error(w,"method unknown",400);return
    }
    w.Header().Set("Content-Type","application/json")
    _=json.NewEncoder(w).Encode(map[string]any{"jsonrpc":"2.0","id":call.ID,"result":result})
}

func flowFixture(t *testing.T)(*Server,*flowBackend,string) {
    t.Helper()
    backend:=&flowBackend{calls:map[string]int{}}
    downstream:=httptest.NewServer(backend)
    t.Cleanup(downstream.Close)
    db,err:=store.Open(filepath.Join(t.TempDir(),"records.json"),"UTC")
    if err!=nil{t.Fatal(err)}
    server,err:=New(db,strings.Repeat("z",40),[]Backend{{Name:"workspace",URL:downstream.URL}})
    if err!=nil{t.Fatal(err)}
    dir:=filepath.Join(t.TempDir(),"workflows")
    state,err:=agentflow.Open(dir)
    if err!=nil{t.Fatal(err)}
    server.SetWorkflows(state)
    return server,backend,dir
}

func TestCrossCapabilityWorkflowPersistedAndExactlyScoped(t *testing.T) {
    server,backend,dir:=flowFixture(t)
    ctx:=context.Background()
    goal,err:=server.db.AddGoal("Study transform theory","")
    if err!=nil{t.Fatal(err)}
    _,err=server.db.AddTask(store.TaskInput{Title:"Read source",GoalID:goal.ID,Links:[]string{"homelab:project:study"}})
    if err!=nil{t.Fatal(err)}
    submitted,err:=server.submitCapabilityWorkflow(ctx,CapabilityProgram{
        Project:"study",Objective:"Investigate Fourier methods",
        Steps:[]CapabilityStage{
            {ID:"context",Action:"project_context"},
            {ID:"research",Action:"research_search",Query:"Fourier",DependsOn:[]string{"context"},Limit:2},
            {ID:"tasks",Action:"project_tasks",DependsOn:[]string{"research"}},
        },
    })
    if err!=nil{t.Fatal(err)}
    if submitted.Status!="pending"||len(submitted.Steps)!=3{t.Fatalf("incorrect pending state: %+v",submitted)}
    if err:=server.advanceCapabilityWorkflow(ctx,submitted.ID,6);err!=nil{t.Fatal(err)}
    completed,err:=server.capabilityWorkflow(ctx,submitted.ID)
    if err!=nil||completed.Status!="succeeded"{t.Fatalf("unfinished workflow: %+v %v",completed,err)}
    if len(completed.Steps[2].Result)==0||!strings.Contains(string(completed.Steps[2].Result),"Read source"){
        t.Fatalf("project-linked task absent: %+v",completed)
    }
    backend.mu.Lock()
    before:=backend.calls["research_search"]
    mismatch:=backend.badScope
    backend.mu.Unlock()
    if mismatch||before!=1{t.Fatalf("read escaped project or repeated: %v %v",mismatch,before)}
    reopened,err:=agentflow.Open(dir)
    if err!=nil{t.Fatal(err)}
    server.SetWorkflows(reopened)
    if err:=server.advanceCapabilityWorkflow(ctx,submitted.ID,3);err!=nil{t.Fatal(err)}
    backend.mu.Lock()
    after:=backend.calls["research_search"]
    backend.mu.Unlock()
    if after!=before {t.Fatalf("successful read replayed after restart: %d %d",before,after)}
    public,err:=server.capabilityWorkflow(ctx,submitted.ID)
    if err!=nil{t.Fatal(err)}
    encoded,_:=json.Marshal(public)
    if strings.Contains(string(encoded),"lease_digest")||strings.Contains(string(encoded),"lease_token"){
        t.Fatalf("workflow exposed internal lease: %s",encoded)
    }
}

func TestCrossCapabilityWorkflowAuthorizationAndValidation(t *testing.T) {
    server,_,_:=flowFixture(t)
    h:=server.Handler()
    program:=`{"project":"study","objective":"Compare domains","steps":[{"id":"first","action":"project_context"}]}`
    if res:=request(h,"POST","/v1/workflows",program,"");res.Code!=401{t.Fatalf("anonymous submission: %d",res.Code)}
    if res:=request(h,"POST","/v1/workflows",program,strings.Repeat("z",40));res.Code!=202{
        t.Fatalf("valid read-only plan refused: %d %s",res.Code,res.Body.String())
    }
    for _,input:=range []string{
        `{"project":"study","objective":"Bad","steps":[{"id":"root","action":"infrastructure__fleet_activate"}]}`,
        `{"project":"study","objective":"Bad","steps":[{"id":"health","action":"health_goals"}]}`,
        `{"project":"study","objective":"Bad","steps":[{"id":"a","action":"project_context","depends_on":["b"]},{"id":"b","action":"project_context","depends_on":["a"]}]}`,
        `{"project":"../private","objective":"Bad","steps":[{"id":"a","action":"project_context"}]}`,
    } {
        res:=request(h,"POST","/v1/workflows",input,strings.Repeat("z",40))
        if res.Code!=400{t.Fatalf("unauthorized workflow accepted: %d %s",res.Code,res.Body.String())}
    }
    validPersonal:=CapabilityStage{ID:"health",Action:"health_goals"}
    if err:=validCapabilityStage(validPersonal,true);err!=nil{t.Fatal(err)}
    if err:=validCapabilityStage(CapabilityStage{ID:"privileged",Action:"infrastructure__fleet_activate"},true);err==nil{
        t.Fatal("personal read consent widened operation privileges")
    }
}
