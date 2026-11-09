package agentflow

import (
 "context"
 "encoding/json"
 "net/http"
 "net/http/httptest"
 "strings"
 "testing"
)

func TestPlanningRouterProvidesReadOnlyTaskDecomposition(t *testing.T){
 requests:=0
 upstream:=httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter,r *http.Request){
  requests++
  if r.Method!=http.MethodPost||r.URL.Path!="/v1/chat/completions"{
   t.Errorf("unexpected model call: %s %s",r.Method,r.URL.Path)
   http.Error(w,"invalid",400);return
  }
  var input struct{
   Messages []struct{
    Role string `json:"role"`
    Content string `json:"content"`
   } `json:"messages"`
  }
  if err:=json.NewDecoder(r.Body).Decode(&input);err!=nil{t.Error(err);return}
  if len(input.Messages)!=2||input.Messages[0].Role!="system"||
   !strings.Contains(input.Messages[0].Content,"read-only draft"){
   t.Error("untrusted model proposal did not receive explicit constraints")
  }
  var brief map[string]any
  if err:=json.Unmarshal([]byte(input.Messages[1].Content),&brief);err!=nil{t.Error(err);return}
  if len(brief)!=4||brief["task_title"]!="Study circuit theory"||brief["estimated_minutes"]!=float64(90){
   t.Errorf("unexpected personal model disclosure: %+v",brief)
  }
  w.Header().Set("Content-Type","application/json")
  _=json.NewEncoder(w).Encode(map[string]any{
   "choices":[]any{map[string]any{
    "finish_reason":"stop",
    "message":map[string]any{"content":`{"steps":[{"title":"Derive Ohm law","minutes":25},{"title":"Solve a circuit","minutes":40,"depends_on_steps":[0]}]}`},
   }},
  })
 }))
 defer upstream.Close()
 route,err:=NewModelRouter([]ModelRoute{
  {Name:"planning",Purpose:"planning",Endpoint:upstream.URL+"/v1/chat/completions",
   Model:"example",LocalOnly:true},
 })
 if err!=nil{t.Fatal(err)}
 raw,err:=route.ProposeTaskSteps(context.Background(),"Study circuit theory",90)
 if err!=nil||!strings.Contains(string(raw),"Solve a circuit")||requests!=1{
  t.Fatalf("task planning route failed: %s %v",raw,err)
 }
 for _,bad:=range []struct{title string;minutes int}{
  {"",10},{"Any title",-1},{"Task",1441},
 }{
  if _,err:=route.ProposeTaskSteps(context.Background(),bad.title,bad.minutes);err==nil{
   t.Fatalf("invalid task title/estimate sent to model: %+v",bad)
  }
 }
 if requests!=1{t.Fatal("invalid task planning sent additional network requests")}
}

func TestModelRouterWillNotFallbackForTaskPlanning(t *testing.T){
 route,err:=NewModelRouter([]ModelRoute{
  {Name:"continuation",Purpose:"continuation",
   Endpoint:"http://127.0.0.1:9010/v1/chat/completions",Model:"continuation"},
 })
 if err!=nil{t.Fatal(err)}
 if _,err:=route.ProposeTaskSteps(context.Background(),"Plan a derivation",60);err==nil{
  t.Fatal("task proposal silently routed to a non-planning model")
 }
}
