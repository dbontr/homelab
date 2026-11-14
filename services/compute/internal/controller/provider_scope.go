package controller

import (
 "bytes"
 "context"
 "encoding/json"
 "errors"
 "io"
 "net/http"
 "strings"
 "sync"
)

// Provider credentials are authorized per invocation, never shared across
// independent MCP tools, workers or HTTP requests.
type providerCredential struct {
 once sync.Once
 value string
 err error
}

type providerScope struct {
 broker *VaultSecretClient
 assertion string
 operation string
 target string
 arguments json.RawMessage
 approveOnce sync.Once
 tickets map[string]VaultCredentialTicket
 authorizeErr error
 aliases map[string]*providerCredential
}

func newProviderScope(
 broker *VaultSecretClient,assertion,operation,target string,
 arguments json.RawMessage,aliases ...string,
)*providerScope{
 available:=make(map[string]*providerCredential,len(aliases))
 for _,alias:=range aliases{
  if alias!=""{available[alias]=&providerCredential{}}
 }
 return &providerScope{
  broker:broker,assertion:assertion,operation:operation,
  target:target,arguments:bytes.Clone(arguments),aliases:available,
 }
}

// Each bearer credential is redeemed once and kept only in the invocation's
// private scope. The owner decides which aliases belong to this exact
// operation, target, arguments and verified service certificate.
func(p *providerScope) credential(ctx context.Context,alias string)(string,error){
 if p==nil||p.broker==nil||!vaultSecretAlias.MatchString(alias){
  return "",errors.New("provider credential authority unavailable")
 }
 cache:=p.aliases[alias]
 if cache==nil{return "",errors.New("provider credential alias outside the request")}
 p.approveOnce.Do(func(){
  p.tickets,p.authorizeErr=p.broker.AuthorizeOperation(
   ctx,p.assertion,p.operation,p.target,p.arguments)
 })
 if p.authorizeErr!=nil{
  return "",p.authorizeErr
 }
 ticket,ok:=p.tickets[alias]
 if !ok{return "",errors.New("provider credential not granted for this operation")}
 cache.once.Do(func(){
  cache.value,cache.err=p.broker.CredentialForOperation(
   ctx,ticket,p.operation,p.target,p.arguments)
 })
 if cache.err!=nil{return "",cache.err}
 if cache.value==""{return "",errors.New("provider credential is unavailable")}
 return cache.value,nil
}

func(p *providerScope) clear(){
 if p==nil{return}
 for _,value:=range p.aliases{
  // Go strings cannot be securely zeroized. Do not retain them after the
  // request returns, log them or store them in controller state.
  value.value=""
 }
 p.tickets=nil
 p.assertion=""
 p.arguments=nil
}

// forProviderOperation creates private provider clients. No credential or
// owner assertion becomes a field of the persistent server or project state.
// Nonprovider operations continue without invoking the owner authority.
func(s *Server)forProviderOperation(ctx context.Context,assertion,operation string,
 arguments json.RawMessage)(*Server,func()){
 if s==nil||s.VaultBroker==nil{return s,func(){}}
 scope:=newProviderScope(s.VaultBroker,assertion,operation,"controller",arguments,
  s.VaultGitHubAlias,s.VaultGitLabAlias,s.VaultEmbeddingsAlias)
 scoped:=*s
 if s.VaultGitHubAlias!=""&&s.GitHub!=nil{
  github:=*s.GitHub
  github.TokenSource=func()(string,error){
   return scope.credential(ctx,s.VaultGitHubAlias)
  }
  scoped.GitHub=&github
 }
 if s.VaultGitLabAlias!=""&&s.GitLab!=nil{
  gitlab:=*s.GitLab
  gitlab.TokenSource=func()(string,error){
   return scope.credential(ctx,s.VaultGitLabAlias)
  }
  scoped.GitLab=&gitlab
 }
 if s.VaultEmbeddingsAlias!=""&&s.Embedder!=nil{
  embedder:=*s.Embedder
  embedder.TokenSource=func()(string,error){
   return scope.credential(ctx,s.VaultEmbeddingsAlias)
  }
  scoped.Embedder=&embedder
 }
 return &scoped,scope.clear
}

// Bind REST operations to the actual method, path, query and bounded JSON
// body before any downstream provider credential is requested. The original
// body remains available to the REST handler.
func(s *Server)forProviderHTTP(r *http.Request)(*Server,func(),error){
 if s==nil||s.VaultBroker==nil{return s,func(){},nil}
 if r==nil{return nil,nil,errors.New("HTTP operation context required")}
 var body json.RawMessage
 if r.Body!=nil {
  raw,err:=io.ReadAll(io.LimitReader(r.Body,256<<10+1))
  if err!=nil||len(raw)>256<<10{
   return nil,nil,errors.New("provider operation body exceeds limit")
  }
  _=r.Body.Close()
  r.Body=io.NopCloser(bytes.NewReader(raw))
  if len(bytes.TrimSpace(raw))!=0{
   if !json.Valid(raw){return nil,nil,errors.New("provider operation JSON invalid")}
   body=raw
  }
 }
 if len(body)==0{body=json.RawMessage("{}")}
 operation:="controller__http_"+strings.ToLower(r.Method)
 arguments,err:=json.Marshal(map[string]any{
  "method":r.Method,"path":r.URL.EscapedPath(),
  "query":r.URL.Query(),"body":body,
 })
 if err!=nil{return nil,nil,errors.New("provider operation context invalid")}
 scoped,finish:=s.forProviderOperation(r.Context(),
  r.Header.Get("Cf-Access-Jwt-Assertion"),operation,arguments)
 return scoped,finish,nil
}
