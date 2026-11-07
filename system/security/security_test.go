package security

import (
 "context"
 "encoding/json"
 "net/url"
 "path/filepath"
 "strings"
 "testing"
 "time"
)

func TestIndependentOneTimeApproval(t *testing.T) {
 key:=[]byte(strings.Repeat("x",32))
 path:=filepath.Join(t.TempDir(),"spent.json")
 a,err:=NewAuthority(key,path);if err!=nil{t.Fatal(err)}
 args:=json.RawMessage(`{"node":"jupiter","action":"install"}`)
 grant,err:=a.Issue("cluster.install","jupiter",args,time.Minute);if err!=nil{t.Fatal(err)}
 if err:=a.Consume(grant,"cluster.install","venus",args);err==nil{t.Fatal("changed target accepted")}
 if err:=a.Consume(grant,"cluster.delete","jupiter",args);err==nil{t.Fatal("changed action accepted")}
 if err:=a.Consume(grant,"cluster.install","jupiter",json.RawMessage(`{"node":"jupiter","action":"delete"}`));err==nil{t.Fatal("changed arguments accepted")}
 if err:=a.Consume(grant,"cluster.install","jupiter",json.RawMessage(`{"action":"install","node":"jupiter"}`));err!=nil{t.Fatal(err)}
 if err:=a.Consume(grant,"cluster.install","jupiter",args);err==nil{t.Fatal("replayed grant accepted")}
 other,err:=NewAuthority(key,path);if err!=nil{t.Fatal(err)}
 if err:=other.Consume(grant,"cluster.install","jupiter",args);err==nil{t.Fatal("grant replayed after restart")}
 if _,err:=NewAuthority([]byte("short"),path);err==nil{t.Fatal("weak executor key accepted")}
 if _,err:=a.Issue("cluster.install","jupiter",args,time.Hour);err==nil{t.Fatal("long grant accepted")}
}

func TestExecutorUsesExplicitHandlers(t *testing.T) {
 a,err:=NewAuthority([]byte(strings.Repeat("z",32)),filepath.Join(t.TempDir(),"spent.json"));if err!=nil{t.Fatal(err)}
 calls:=0
 executor,err:=NewExecutor(a,map[string]Handler{"software.approve":func(_ context.Context,target string,_ json.RawMessage)error{
   if target!="library"{t.Fatal("wrong target")}
   calls++
   return nil
 }})
 if err!=nil{t.Fatal(err)}
 args:=json.RawMessage(`{"package":"tool"}`)
 grant,err:=a.Issue("software.approve","library",args,time.Minute);if err!=nil{t.Fatal(err)}
 if err:=executor.Execute(context.Background(),grant,"software.delete","library",args);err==nil{t.Fatal("unregistered action executed")}
 if err:=executor.Execute(context.Background(),grant,"software.approve","library",args);err!=nil{t.Fatal(err)}
 if err:=executor.Execute(context.Background(),grant,"software.approve","library",args);err==nil||calls!=1{t.Fatal("replayed action executed")}
}

func TestOAuthBrokerKeepsVerifierPrivate(t *testing.T) {
 broker,err:=NewOAuthBroker([]Provider{{Name:"vendor",AuthorizeURL:"https://provider.example/oauth/authorize",ClientID:"client",RedirectURL:"http://127.0.0.1:9823/callback",Scopes:[]string{"read"}}})
 if err!=nil{t.Fatal(err)}
 flow,err:=broker.Begin("vendor");if err!=nil{t.Fatal(err)}
 u,_:=url.Parse(flow.URL)
 state:=u.Query().Get("state")
 if u.Query().Get("code_challenge_method")!="S256"||u.Query().Get("code_challenge")==""||state==""{t.Fatal("missing PKCE")}
 if err:=broker.Finish(context.Background(),"wrong","code",func(context.Context,Provider,string,string)error{t.Fatal("wrong state exchanged");return nil});err==nil{t.Fatal("unknown state accepted")}
 calls:=0
 err=broker.Finish(context.Background(),state,"code",func(_ context.Context,p Provider,code,verifier string)error{
  calls++
  if p.Name!="vendor"||code!="code"||len(verifier)<43||strings.Contains(flow.URL,verifier){t.Fatal("verifier escaped or invalid")}
  return nil
 })
 if err!=nil{t.Fatal(err)}
 if err:=broker.Finish(context.Background(),state,"code",func(context.Context,Provider,string,string)error{return nil});err==nil||calls!=1{t.Fatal("state reused")}
 if _,err:=NewOAuthBroker([]Provider{{Name:"unsafe",AuthorizeURL:"http://provider.example/auth",ClientID:"a",RedirectURL:"https://evil.example/callback"}});err==nil{t.Fatal("insecure provider accepted")}
}
