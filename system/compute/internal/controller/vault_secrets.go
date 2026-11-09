package controller

import (
 "context"
 "crypto/tls"
 "crypto/x509"
 "errors"
 "io"
 "net/http"
 "net/url"
 "os"
 "regexp"
 "strings"
 "time"
)

var vaultSecretAlias=regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_-]{0,63}$`)

// VaultSecretClient obtains one service-scoped credential over mutually
// authenticated TLS. It never accepts request-specific target URLs or secrets
// from MCP inputs. Every read rechecks the source vault after rotation.
type VaultSecretClient struct {
 client *http.Client
 endpoint string
}

func NewVaultSecretClient(endpoint,serverName,caFile,certFile,keyFile string)(*VaultSecretClient,error){
 parsed,err:=url.Parse(endpoint)
 if err!=nil||parsed.Scheme!="https"||parsed.Host==""||parsed.User!=nil||
  parsed.Opaque!=""||parsed.RawQuery!=""||parsed.Fragment!=""||
  (parsed.Path!=""&&parsed.Path!="/")||serverName==""||
  caFile==""||certFile==""||keyFile==""{
  return nil,errors.New("invalid owner vault service endpoint")
 }
 rootsRaw,err:=os.ReadFile(caFile)
 if err!=nil||len(rootsRaw)>128<<10{return nil,errors.New("vault trust roots unavailable")}
 roots:=x509.NewCertPool()
 if !roots.AppendCertsFromPEM(rootsRaw){return nil,errors.New("invalid vault trust roots")}
 pair,err:=tls.LoadX509KeyPair(certFile,keyFile)
 if err!=nil{return nil,errors.New("vault client certificate unavailable")}
 transport:=&http.Transport{
  Proxy:nil,DisableKeepAlives:true,
  TLSClientConfig:&tls.Config{MinVersion:tls.VersionTLS13,
   ServerName:serverName,RootCAs:roots,
   Certificates:[]tls.Certificate{pair}},
  ResponseHeaderTimeout:5*time.Second,
 }
 client:=&http.Client{Transport:transport,Timeout:8*time.Second,
  CheckRedirect:func(*http.Request,[]*http.Request)error{
   return errors.New("vault redirects are forbidden")
  }}
 return &VaultSecretClient{client:client,endpoint:strings.TrimSuffix(parsed.String(),"/")},nil
}

func (c *VaultSecretClient)Credential(alias string)(string,error){
 if c==nil||!vaultSecretAlias.MatchString(alias){
  return "",errors.New("invalid vault credential alias")
 }
 ctx,cancel:=context.WithTimeout(context.Background(),8*time.Second)
 defer cancel()
 req,err:=http.NewRequestWithContext(ctx,http.MethodGet,c.endpoint+"/v1/service-credentials/"+alias,nil)
 if err!=nil{return "",errors.New("invalid vault credential request")}
 req.Header.Set("Accept","application/octet-stream")
 response,err:=c.client.Do(req)
 if err!=nil{return "",errors.New("owner vault is unavailable")}
 defer response.Body.Close()
 if response.StatusCode!=http.StatusOK||
  response.Header.Get("Content-Type")!="application/octet-stream"{
  return "",errors.New("owner vault denied credential access")
 }
 body,err:=io.ReadAll(io.LimitReader(response.Body,4097))
 if err!=nil||len(body)>4096||len(body)<20{
  return "",errors.New("invalid scoped provider credential")
 }
 token:=strings.TrimSuffix(string(body),"\n")
 if len(token)<20||strings.ContainsAny(token,"\x00\r\n\t "){
  return "",errors.New("invalid scoped provider token")
 }
 return token,nil
}
