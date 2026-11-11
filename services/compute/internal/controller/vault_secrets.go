package controller

import (
 "bytes"
 "context"
 "encoding/json"
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

// VaultCredentialTicket is issued only by the owner security authority
// following an exact independently authorized operation.
type VaultCredentialTicket struct {
 Alias string
 Ticket string
}

var vaultTicketID=regexp.MustCompile(`^[0-9a-f]{48}package controller

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

)

// CredentialForOperation redeems a one-use ticket on the same mTLS identity
// that the owner authority bound to its operation and resource.
func (c *VaultSecretClient) CredentialForOperation(
 ctx context.Context,capability VaultCredentialTicket,
 operation,target string,arguments json.RawMessage,
)(string,error) {
 if c==nil||!vaultSecretAlias.MatchString(capability.Alias)||
  !vaultTicketID.MatchString(capability.Ticket)||operation==""||len(operation)>128||
  target==""||len(target)>128||len(arguments)==0||len(arguments)>16<<10{
  return "",errors.New("invalid scoped credential capability")
 }
 var object map[string]json.RawMessage
 if err:=json.Unmarshal(arguments,&object);err!=nil||object==nil{
  return "",errors.New("invalid scoped credential arguments")
 }
 requestBody,err:=json.Marshal(struct{
  Ticket string `json:"ticket"`
  Operation string `json:"operation"`
  Target string `json:"target"`
  Arguments json.RawMessage `json:"arguments"`
 }{capability.Ticket,operation,target,arguments})
 if err!=nil{return "",errors.New("invalid credential request")}
 request,err:=http.NewRequestWithContext(ctx,http.MethodPost,
  c.endpoint+"/v1/service-credentials/"+capability.Alias,bytes.NewReader(requestBody))
 if err!=nil{return "",errors.New("invalid credential request")}
 request.Header.Set("Accept","application/octet-stream")
 request.Header.Set("Content-Type","application/json")
 response,err:=c.client.Do(request)
 if err!=nil{return "",errors.New("owner vault is unavailable")}
 defer response.Body.Close()
 if response.StatusCode!=http.StatusOK||
  response.Header.Get("Content-Type")!="application/octet-stream"{
  return "",errors.New("owner vault denied credential access")
 }
 body,err:=io.ReadAll(io.LimitReader(response.Body,8193))
 defer func(){for i:=range body{body[i]=0}}()
 if err!=nil||len(body)>8192||len(body)<20{
  return "",errors.New("invalid scoped provider credential")
 }
 token:=strings.TrimSuffix(string(body),"\n")
 if len(token)<20||strings.ContainsAny(token,"\x00\r\n\t "){
  return "",errors.New("invalid scoped provider token")
 }
 return token,nil
}

// Ambient provider callbacks have no independently authorized request or
// one-use ticket. They must not retrieve protected vault credentials.
func (c *VaultSecretClient) Credential(alias string)(string,error){
 return "",errors.New("credential requires an owner-authorized operation ticket")
}
