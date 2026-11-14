package controller

import (
 "bytes"
 "context"
 "encoding/json"
 "crypto/tls"
 "crypto/x509"
 "errors"
 "io"
 "net"
 "net/http"
 "net/url"
 "os"
 "path/filepath"
 "syscall"
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

// A vault TLS name is an exact certificate DNS identity, not an IP literal,
// wildcard, URL or hostname with ambiguous case.
func validVaultServerDNS(name string)bool{
 if name==""||len(name)>253||net.ParseIP(name)!=nil{return false}
 for _,label:=range strings.Split(name,"."){
  if len(label)==0||len(label)>63{return false}
  for i:=0;i<len(label);i++{
   c:=label[i]
   ordinary:=(c>='a'&&c<='z')||(c>='0'&&c<='9')
   if !ordinary&&(c!='-'||i==0||i==len(label)-1){return false}
  }
 }
 return true
}

// Read only the opened, protected inode; a path replacement or symbolic
// link cannot supply different trust material or the client private key.
func protectedVaultTLSFile(path string,private bool)([]byte,error){
 if !filepath.IsAbs(path)||filepath.Clean(path)!=path{return nil,errors.New("absolute TLS path required")}
 file,err:=os.OpenFile(path,os.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_CLOEXEC,0)
 if err!=nil{return nil,errors.New("vault TLS file unavailable")}
 defer file.Close()
 info,err:=file.Stat()
 if err!=nil||!info.Mode().IsRegular()||info.Size()<1||info.Size()>1<<20{
  return nil,errors.New("vault TLS file has invalid custody")
 }
 forbidden:=os.FileMode(0o022)
 if private{forbidden=0o077}
 if info.Mode().Perm()&forbidden!=0{return nil,errors.New("vault TLS file has unsafe permissions")}
 if private{
  inode,ok:=info.Sys().(*syscall.Stat_t)
  if !ok||inode.Uid!=uint32(os.Geteuid())||inode.Nlink!=1{
   return nil,errors.New("vault private key has unsafe ownership")
  }
 }
 data,err:=io.ReadAll(io.LimitReader(file,1<<20+1))
 if err!=nil||len(data)>1<<20{return nil,errors.New("vault TLS file exceeds size limit")}
 return data,nil
}

func NewVaultSecretClient(endpoint,serverName,caFile,certFile,keyFile string)(*VaultSecretClient,error){
 parsed,err:=url.Parse(endpoint)
 if err!=nil||parsed.Scheme!="https"||parsed.Host==""||parsed.User!=nil||
  parsed.Opaque!=""||parsed.RawQuery!=""||parsed.Fragment!=""||
  (parsed.Path!=""&&parsed.Path!="/")||!validVaultServerDNS(serverName)||
  caFile==""||certFile==""||keyFile==""{
  return nil,errors.New("invalid owner vault service endpoint")
 }
 rootsRaw,err:=protectedVaultTLSFile(caFile,false)
 if err!=nil{return nil,errors.New("vault trust roots unavailable")}
 roots:=x509.NewCertPool()
 if !roots.AppendCertsFromPEM(rootsRaw){return nil,errors.New("invalid vault trust roots")}
 certRaw,err:=protectedVaultTLSFile(certFile,false)
 if err!=nil{return nil,errors.New("vault client certificate unavailable")}
 keyRaw,err:=protectedVaultTLSFile(keyFile,true)
 if err!=nil{return nil,errors.New("vault private key unavailable")}
 defer func(){for i:=range keyRaw{keyRaw[i]=0}}()
 pair,err:=tls.X509KeyPair(certRaw,keyRaw)
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

var vaultTicketID=regexp.MustCompile(`^[0-9a-f]{48}$`)

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

// AuthorizeOperation obtains a bounded set of one-use credential tickets from
// the owner authority. The signed owner assertion is never synthesized from
// controller-local OAuth or bearer credentials. The authority independently
// checks the owner, operation, target, arguments and mTLS service identity.
func(c *VaultSecretClient)AuthorizeOperation(
 ctx context.Context,assertion,operation,target string,arguments json.RawMessage,
)(map[string]VaultCredentialTicket,error){
 if c==nil||assertion==""||len(assertion)>16<<10||
  operation==""||len(operation)>128||target==""||len(target)>128||
  len(arguments)==0||len(arguments)>16<<10{
  return nil,errors.New("signed owner operation authorization required")
 }
 var value map[string]json.RawMessage
 if err:=json.Unmarshal(arguments,&value);err!=nil||value==nil{
  return nil,errors.New("invalid owner authorization arguments")
 }
 body,err:=json.Marshal(struct{
  Operation string `json:"operation"`
  Target string `json:"target"`
  Arguments json.RawMessage `json:"arguments"`
 }{operation,target,arguments})
 if err!=nil{return nil,errors.New("invalid owner authorization request")}
 request,err:=http.NewRequestWithContext(ctx,http.MethodPost,
  c.endpoint+"/v1/authorize",bytes.NewReader(body))
 if err!=nil{return nil,errors.New("invalid owner authorization request")}
 request.Header.Set("Content-Type","application/json")
 request.Header.Set("Cf-Access-Jwt-Assertion",assertion)
 response,err:=c.client.Do(request)
 if err!=nil{return nil,errors.New("independent owner authorization unavailable")}
 defer response.Body.Close()
 if response.StatusCode!=http.StatusOK ||
  !strings.HasPrefix(response.Header.Get("Content-Type"),"application/json"){
  return nil,errors.New("independent owner authorization refused")
 }
 raw,err:=io.ReadAll(io.LimitReader(response.Body,8193))
 if err!=nil||len(raw)>8192{
  return nil,errors.New("owner authorization response exceeds limit")
 }
 var decision struct{
  Allowed bool `json:"allowed"`
  Reason string `json:"reason"`
  ConfirmationID string `json:"confirmation_id"`
  ConfirmationExpiresAt time.Time `json:"confirmation_expires_at"`
  CredentialTickets []struct{
   Alias string `json:"alias"`
   Ticket string `json:"ticket"`
  } `json:"credential_tickets"`
 }
 decoder:=json.NewDecoder(bytes.NewReader(raw))
 decoder.DisallowUnknownFields()
 if err:=decoder.Decode(&decision);err!=nil{
  return nil,errors.New("invalid owner authorization decision")
 }
 var extra any
 if err:=decoder.Decode(&extra);err!=io.EOF{
  return nil,errors.New("owner authorization response has trailing data")
 }
 if !decision.Allowed{
  if len(decision.CredentialTickets)!=0{
   return nil,errors.New("denied operation returned credential capabilities")
  }
  if decision.ConfirmationID!=""{
   return nil,errors.New("owner confirmation required for this provider operation")
  }
  return nil,errors.New("independent owner operation authorization denied")
 }
 if decision.Reason!=""||decision.ConfirmationID!=""||
  !decision.ConfirmationExpiresAt.IsZero()||len(decision.CredentialTickets)>8{
  return nil,errors.New("contradictory owner authorization decision")
 }
 tickets:=make(map[string]VaultCredentialTicket,len(decision.CredentialTickets))
 for _,candidate:=range decision.CredentialTickets{
  if !vaultSecretAlias.MatchString(candidate.Alias)||
   !vaultTicketID.MatchString(candidate.Ticket){
   return nil,errors.New("invalid owner credential capability")
  }
  if _,exists:=tickets[candidate.Alias];exists{
   return nil,errors.New("duplicate owner credential capability")
  }
  tickets[candidate.Alias]=VaultCredentialTicket{
   Alias:candidate.Alias,Ticket:candidate.Ticket,
  }
 }
 return tickets,nil
}

// Ambient provider callbacks have no independently authorized request or
// one-use ticket. They must not retrieve protected vault credentials.
func (c *VaultSecretClient) Credential(alias string)(string,error){
 return "",errors.New("credential requires an owner-authorized operation ticket")
}
