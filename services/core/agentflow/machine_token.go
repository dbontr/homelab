package agentflow

import (
 "context"
 "encoding/json"
 "errors"
 "io"
 "net/http"
 "net/url"
 "os"
 "strings"
 "sync"
 "syscall"
 "time"

 "github.com/dbontr/homelab-new/system/security"
)

type machineTokenSource struct {
 endpoint string
 clientID string
 secretFile string
 scope string
 resource string
 client *http.Client
 mu sync.Mutex
 token string
 expires time.Time
 now func()time.Time
}

// NewMachineTokenSource obtains a short-lived bearer from a separately
// configured OAuth 2.0 client-credentials issuer. The issuer, not Engineering,
// signs the token. Compute still verifies that signature, its audience and the
// current project editor grant before any source operation.
func NewMachineTokenSource(endpoint,clientID,secretFile,scope,resource string)(func()(string,error),error){
 target,err:=url.Parse(endpoint)
 if err!=nil||target.Scheme!="https"||target.Host==""||target.User!=nil||
  target.RawQuery!=""||target.Fragment!=""||target.Opaque!=""||target.Path==""{
  return nil,errors.New("engineering token endpoint must be an exact HTTPS URL")
 }
 if clientID==""||len(clientID)>256||strings.ContainsAny(clientID,"\x00\r\n\t ")||
  len(scope)>512||strings.ContainsAny(scope,"\x00\r\n") {
  return nil,errors.New("invalid engineering machine identity")
 }
 if resource!=""{
  target,err:=url.Parse(resource)
  if err!=nil||target.Scheme!="https"||target.Host==""||target.User!=nil||
   target.RawQuery!=""||target.Fragment!=""||target.Opaque!="" {
   return nil,errors.New("machine-token resource must be an HTTPS origin")
  }
 }
 if _,err:=readMachineSecret(secretFile);err!=nil{return nil,err}
 source:=&machineTokenSource{
  endpoint:endpoint,clientID:clientID,secretFile:secretFile,
  scope:scope,resource:resource,now:time.Now,
  client:&http.Client{
   Timeout:10*time.Second,
   CheckRedirect:func(*http.Request,[]*http.Request)error{return errors.New("machine token redirects are prohibited")},
  },
 }
 return source.get,nil
}

func readMachineSecret(path string)(string,error){
 if path==""{return "",errors.New("protected engineering client secret required")}
 f,err:=os.OpenFile(path,os.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_CLOEXEC,0)
 if err!=nil{return "",errors.New("engineering client secret unavailable")}
 defer f.Close()
 stat,err:=f.Stat()
 if err!=nil||!stat.Mode().IsRegular()||stat.Size()<32||stat.Size()>4096{
  return "",errors.New("unsafe engineering client secret")
 }
 inode,ok:=stat.Sys().(*syscall.Stat_t)
 if !ok||inode.Nlink!=1{return "",errors.New("unsafe engineering client secret links")}
 if !security.PrivateCredentialFileAllowed(path,stat.Mode(),inode.Uid){
  return "",errors.New("unsafe engineering client secret")
 }
 raw,err:=io.ReadAll(io.LimitReader(f,4097))
 if err!=nil||len(raw)>4096{return "",errors.New("engineering client secret exceeds limit")}
 secret:=strings.TrimSpace(string(raw))
 if len(secret)<32||strings.ContainsAny(secret,"\x00\r\n\t "){
  return "",errors.New("invalid engineering client secret")
 }
 return secret,nil
}

func(s *machineTokenSource)get()(string,error){
 s.mu.Lock()
 defer s.mu.Unlock()
 now:=s.now()
 if s.token!=""&&now.Before(s.expires){return s.token,nil}
 secret,err:=readMachineSecret(s.secretFile)
 if err!=nil{return "",err}
 form:=url.Values{"grant_type":{"client_credentials"}}
 if s.scope!=""{form.Set("scope",s.scope)}
 if s.resource!=""{form.Set("resource",s.resource)}
 ctx,cancel:=context.WithTimeout(context.Background(),10*time.Second)
 defer cancel()
 req,err:=http.NewRequestWithContext(ctx,http.MethodPost,s.endpoint,strings.NewReader(form.Encode()))
 if err!=nil{return "",errors.New("invalid engineering machine token request")}
 req.SetBasicAuth(s.clientID,secret)
 req.Header.Set("Content-Type","application/x-www-form-urlencoded")
 req.Header.Set("Accept","application/json")
 reply,err:=s.client.Do(req)
 if err!=nil{return "",errors.New("engineering OAuth authority unavailable")}
 defer reply.Body.Close()
 if reply.StatusCode!=http.StatusOK||!strings.HasPrefix(reply.Header.Get("Content-Type"),"application/json"){
  return "",errors.New("engineering OAuth authority refused machine credential")
 }
 raw,err:=io.ReadAll(io.LimitReader(reply.Body,16385))
 if err!=nil||len(raw)>16384{return "",errors.New("invalid engineering token response size")}
 var response struct{
  AccessToken string `json:"access_token"`
  TokenType string `json:"token_type"`
  ExpiresIn int `json:"expires_in"`
 }
 if err:=json.Unmarshal(raw,&response);err!=nil||
  !strings.EqualFold(response.TokenType,"Bearer")||response.ExpiresIn<90||
  response.ExpiresIn>86400||len(response.AccessToken)<32||
  len(response.AccessToken)>16384||strings.Count(response.AccessToken,".")!=2||
  strings.ContainsAny(response.AccessToken,"\x00\r\n\t ") {
  return "",errors.New("engineering OAuth authority returned an invalid signed token")
 }
 s.token=response.AccessToken
 lifetime:=time.Duration(response.ExpiresIn)*time.Second
 refresh:=lifetime-time.Minute
 if refresh<=0{refresh=lifetime/2}
 s.expires=now.Add(refresh)
 return s.token,nil
}
