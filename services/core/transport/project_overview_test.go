package transport

import (
    "context"
    "encoding/json"
    "net/http"
    "net/http/httptest"
    "path/filepath"
    "strings"
    "testing"

    "github.com/dbontr/homelab-new/system/store"
)

func TestProjectOverviewIsScopedAndReadThrough(t *testing.T) {
    downstream:=httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter,r *http.Request){
        var req struct{Method string `json:"method"`;Params json.RawMessage `json:"params"`}
        if err:=json.NewDecoder(r.Body).Decode(&req);err!=nil{t.Error(err);return}
        w.Header().Set("Content-Type","application/json")
        var response any
        if req.Method=="tools/list" {
            response=map[string]any{"jsonrpc":"2.0","id":1,"result":map[string]any{"tools":[]any{map[string]any{"name":"knowledge_context","description":"Read project context","inputSchema":map[string]any{"type":"object"}}}}}
        } else {
            var call struct{Arguments struct{ProjectID string `json:"project_id"`} `json:"arguments"`}
            if err:=json.Unmarshal(req.Params,&call);err!=nil{t.Error(err);return}
            payload,_:=json.Marshal(map[string]any{"project":map[string]string{"id":call.Arguments.ProjectID,"title":"Research"},"repositories":[]any{},"libraries":[]any{}})
            response=map[string]any{"jsonrpc":"2.0","id":1,"result":map[string]any{"content":[]any{map[string]any{"type":"text","text":string(payload)}}}}
        }
        if err:=json.NewEncoder(w).Encode(response);err!=nil{t.Error(err)}
    }))
    defer downstream.Close()
    db,err:=store.Open(filepath.Join(t.TempDir(),"tasks.json"),"UTC")
    if err!=nil{t.Fatal(err)}
    goal,err:=db.AddGoal("Derive a proof","")
    if err!=nil{t.Fatal(err)}
    task,err:=db.AddTask(store.TaskInput{Title:"Check theorem",GoalID:goal.ID,Links:[]string{"homelab:project:study"}})
    if err!=nil{t.Fatal(err)}
    _,err=db.AddTask(store.TaskInput{Title:"Unrelated task",Links:[]string{"homelab:project:unrelated"}})
    if err!=nil{t.Fatal(err)}
    server,err:=New(db,strings.Repeat("x",40),[]Backend{{Name:"workspace",URL:downstream.URL}})
    if err!=nil{t.Fatal(err)}
    h:=server.Handler()
    if got:=request(h,"GET","/v1/projects/study/overview","","");got.Code!=401{t.Fatalf("unauthenticated: %d",got.Code)}
    got:=request(h,"GET","/v1/projects/study/overview","",strings.Repeat("x",40))
    if got.Code!=200 {t.Fatalf("overview: %d %s",got.Code,got.Body.String())}
    var overview ProjectOverview
    if err:=json.Unmarshal(got.Body.Bytes(),&overview);err!=nil{t.Fatal(err)}
    if len(overview.Tasks)!=1||overview.Tasks[0].ID!=task.ID||len(overview.Goals)!=1||len(overview.Links)!=2 {
        t.Fatalf("unrelated data joined to project: %+v",overview)
    }
    resource,err:=server.resource(context.Background(),"project","study")
    if err!=nil||resource.Reference!="homelab:project:study"||resource.Owner!="compute"{t.Fatalf("project reference: %+v %v",resource,err)}
}
