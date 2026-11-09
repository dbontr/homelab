package security

import (
 "context"
 "crypto/tls"
 "crypto/x509"
 "errors"
 "io"
 "net/http"
 "os"
 "regexp"
 "strings"
 "time"
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
 rootPEM,err:=os.ReadFile(caFile)
 if err!=nil||len(rootPEM)>128<<10{return nil,errors.New("vault trust roots unavailable")}
 roots:=x509.NewCertPool()
 if !roots.AppendCertsFromPEM(rootPEM){return nil,errors.New("invalid vault trust roots")}
 certificate,err:=tls.LoadX509KeyPair(certFile,keyFile)
 if err!=nil{return nil,errors.New("vault client identity unavailable")}
 return NewRemoteAuthorizationClient(RemoteAuthorizationConfig{
  Endpoint:endpoint,ServerName:serverName,TrustRoots:roots,Certificate:certificate,
 })
}

// ServiceCredential fetches only the alias granted to the service's unique
// client certificate. Revoked and rotated vault records are checked afresh.
func(c *RemoteAuthorizationClient)ServiceCredential(ctx context.Context,alias string)(string,error){
 if c==nil||!serviceAlias.MatchString(alias){
  return "",errors.New("invalid service credential alias")
 }
 request,err:=http.NewRequestWithContext(ctx,http.MethodGet,
  c.endpoint+"/v1/service-credentials/"+alias,nil)
 if err!=nil{return "",errors.New("invalid service credential request")}
 request.Header.Set("Accept","application/octet-stream")
 response,err:=c.client.Do(request)
 if err!=nil{return "",errors.New("owner vault unavailable")}
 defer response.Body.Close()
 if response.StatusCode!=http.StatusOK||
  response.Header.Get("Content-Type")!="application/octet-stream"{
  return "",errors.New("owner vault refused credential")
 }
 raw,err:=io.ReadAll(io.LimitReader(response.Body,4097))
 if err!=nil||len(raw)<20||len(raw)>4096{
  return "",errors.New("invalid service credential payload")
 }
 secret:=strings.TrimSuffix(string(raw),"\n")
 if len(secret)<20||strings.ContainsAny(secret,"\x00\r\n\t "){
  return "",errors.New("invalid service credential encoding")
 }
 return secret,nil
}

// TokenSource is a single-alias callback for trusted credential consumers.
// It never falls back to a second authority after a vault failure.
func(c *RemoteAuthorizationClient)TokenSource(alias string)(func()(string,error),error){
 if c==nil||!serviceAlias.MatchString(alias){
  return nil,errors.New("vault client and valid alias required")
 }
 return func()(string,error){
  ctx,cancel:=context.WithTimeout(context.Background(),8*time.Second)
  defer cancel()
  return c.ServiceCredential(ctx,alias)
 },nil
}
