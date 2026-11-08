package agentflow

import (
 "context"
 "encoding/json"
 "fmt"
 "net/http"
 "net/http/httptest"
 "strings"
 "testing"
)

func TestModelAuthorAcceptsOnlyApprovedFiles(t *testing.T){
 proposed:=`{"files":{"README.md":"Corrected contents"}}`
 server:=httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter,r *http.Request){
  if r.URL.Path!="/v1/chat/completions"||r.Method!="POST"{t.Errorf("unexpected request %s %s",r.Method,r.URL.Path)}
  w.Header().Set("Content-Type","application/json")
  _=json.NewEncoder(w).Encode(map[string]any{
   "choices":[]map[string]any{{"message":map[string]string{"content":proposed},"finish_reason":"stop"}},
  })
 }))
 defer server.Close()
 author,err:=NewModelAuthor(server.URL+"/v1/chat/completions","local-model","")
 if err!=nil{t.Fatal(err)}
 files,err:=author.Propose(context.Background(),"Improve documentation",map[string]string{"README.md":"Old contents"},[]string{"README.md"})
 if err!=nil{t.Fatal(err)}
 if files["README.md"]!="Corrected contents"{t.Fatalf("unexpected changes %v",files)}
 proposed=`{"files":{"secrets/private.key":"stolen"}}`
 if _,err:=author.Propose(context.Background(),"Improve documentation",nil,[]string{"README.md"});err==nil{
  t.Fatal("accepted model output outside owner-approved file scope")
 }
}
func TestModelAuthorRejectsRemotePlainHTTPAndRedirects(t *testing.T){
 if _,err:=NewModelAuthor("http://example.net/v1/chat/completions","model","");err==nil{t.Fatal("remote HTTP model accepted")}
 remote:=httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter,_ *http.Request){
  http.Redirect(w,&http.Request{}, "http://example.org/",http.StatusFound)
 }))
 defer remote.Close()
 author,err:=NewModelAuthor(remote.URL+"/v1/chat/completions","model","");if err!=nil{t.Fatal(err)}
 if _,err:=author.Propose(context.Background(),"Review",nil,[]string{"README.md"});err==nil{t.Fatal("model redirect followed")}
}
func TestComputeClientEnforcesToolAllowlistAndResponseIdentity(t *testing.T){
 id:=1
 server:=httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter,r *http.Request){
  if r.URL.Path!="/mcp"||r.Header.Get("Authorization")!="Bearer "+strings.Repeat("t",32){t.Error("unexpected compute request")}
  fmt.Fprintf(w,`{"jsonrpc":"2.0","id":%d,"result":{"isError":false,"structuredContent":{"result":[]}}}`,id)
 }))
 defer server.Close()
 client,err:=NewComputeClient(server.URL+"/mcp",strings.Repeat("t",32));if err!=nil{t.Fatal(err)}
 if _,err:=client.Call(context.Background(),"github_merge_pull_request",map[string]any{});err==nil{t.Fatal("privileged repository operation permitted")}
 if _,err:=client.Call(context.Background(),"workspace_list",map[string]any{"project_id":"research"});err!=nil{t.Fatal(err)}
 id=2
 if _,err:=client.Call(context.Background(),"workspace_list",map[string]any{"project_id":"research"});err==nil{t.Fatal("accepted mismatched RPC reply")}
}
