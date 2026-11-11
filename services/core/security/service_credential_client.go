package security

import (
 "bytes"
 "context"
 "encoding/json"
 "errors"
 "io"
 "net/http"
 "os"
 "regexp"
 "strings"
 "time"

 "github.com/dbontr/homelab-new/system/canonicaljson"
)

// Service credentials are retrieved only through the independent private
// mutual-TLS listener. The alias is an operator-approved service binding.
var serviceAlias=regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_-]{0,63}$`)

// NewVaultClientFromFiles uses protected TLS identity and trust files. It does
// not accept certificates or an endpoint from MCP calls.
func NewVaultClientFromFiles(endpoint,serverName,caFile,certFile,keyFile string)(*RemoteAuthorizationClient,error){
 if endpoint==""||serverName==""||caFile==""||certFile==""||keyFile==""{
  return nil,errors.New("incomplete trusted vault client configuration")
 }
 roots,err:=LoadTLSRoots(caFile)
 if err!=nil{return nil,errors.New("vault trust roots unavailable")}
 certificate,err:=LoadTLSIdentity(certFile,keyFile)
 if err!=nil{return nil,errors.New("vault client identity unavailable")}
 return NewRemoteAuthorizationClient(RemoteAuthorizationConfig{
  Endpoint:endpoint,ServerName:serverName,TrustRoots:roots,Certificate:certificate,
 })
}

// ServiceCredential redeems one non-exportable executor decision with the
// exact original operation, resource and canonical argument object. A bare
// alias cannot retrieve a protected credential.
func(c *RemoteAuthorizationClient)ServiceCredential(
 ctx context.Context,alias string,capability CredentialTicket,
 operation,target string,arguments json.RawMessage,
)(string,error){
 if c==nil||!serviceAlias.MatchString(alias)||capability.Alias!=alias||
  !serviceCredentialTicket.MatchString(capability.Ticket){
  return "",errors.New("invalid service credential capability")
 }
 canonical,_,err:=canonicaljson.Object(arguments)
 if err!=nil{return "",errors.New("invalid credential authorization arguments")}
 payload,err:=json.Marshal(struct{
  Ticket string `json:"ticket"`
  Operation string `json:"operation"`
  Target string `json:"target"`
  Arguments json.RawMessage `json:"arguments"`
 }{capability.Ticket,operation,target,canonical})
 if err!=nil{return "",errors.New("invalid credential request")}
 request,err:=http.NewRequestWithContext(ctx,http.MethodPost,
  c.endpoint+"/v1/service-credentials/"+alias,bytes.NewReader(payload))
 if err!=nil{return "",errors.New("invalid service credential request")}
 request.Header.Set("Accept","application/octet-stream")
 request.Header.Set("Content-Type","application/json")
 response,err:=c.client.Do(request)
 if err!=nil{return "",errors.New("owner vault unavailable")}
 defer response.Body.Close()
 if response.StatusCode!=http.StatusOK||
  response.Header.Get("Content-Type")!="application/octet-stream"{
  return "",errors.New("owner vault refused credential")
 }
 raw,err:=io.ReadAll(io.LimitReader(response.Body,8193))
 if err!=nil||len(raw)==0||len(raw)>8192{
  return "",errors.New("invalid service credential payload")
 }
 secret:=strings.TrimSuffix(string(raw),"\n")
 if len(secret)<20||strings.ContainsAny(secret,"\x00\r\n\t "){
  return "",errors.New("invalid service credential encoding")
 }
 return secret,nil
}

// ScopedTokenSource is constructed only by a trusted executor. It never
// manufactures approval or queries the vault without a one-use ticket.
func(c *RemoteAuthorizationClient)ScopedTokenSource(alias string)(
 func(context.Context,CredentialTicket,string,string,json.RawMessage)(string,error),error,
){
 if c==nil||!serviceAlias.MatchString(alias){
  return nil,errors.New("vault client and valid alias required")
 }
 return func(ctx context.Context,capability CredentialTicket,operation,target string,args json.RawMessage)(string,error){
  return c.ServiceCredential(ctx,alias,capability,operation,target,args)
 },nil
}

// TokenSource is retained as an API compatibility boundary but refuses
// ambient credential reads. Callers must use ScopedTokenSource with the
// independently authorized operation ticket.
func(c *RemoteAuthorizationClient)TokenSource(alias string)(func()(string,error),error){
 return nil,errors.New("ambient vault credential access is not authorized")
}

// NewVaultClientFromEnv reads only trusted process configuration. The caller
// decides whether a provider credential alias is required for its operation.
func NewVaultClientFromEnv(prefix string)(*RemoteAuthorizationClient,error){
 if !serviceAlias.MatchString(prefix)||strings.ToUpper(prefix)!=prefix{
  return nil,errors.New("invalid vault environment prefix")
 }
 variable:=func(suffix string)string{return os.Getenv(prefix+"_VAULT_"+suffix)}
 endpoint,server:=variable("URL"),variable("SERVER_NAME")
 ca,cert,key:=variable("CA_FILE"),variable("CLIENT_CERT_FILE"),variable("CLIENT_KEY_FILE")
 if endpoint==""&&server==""&&ca==""&&cert==""&&key==""{
  return nil,nil
 }
 return NewVaultClientFromFiles(endpoint,server,ca,cert,key)
}
