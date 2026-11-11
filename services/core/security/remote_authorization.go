package security

import (
 "bytes"
 "context"
 "crypto/tls"
 "crypto/x509"
 "encoding/json"
 "errors"
 "fmt"
 "io"
 "net"
 "net/http"
 "net/url"
 "strings"
 "time"
)

// RemoteAuthorizationConfig is provisioned by the trusted host administrator.
// TLS identities and signed owner assertions never come from MCP arguments.
type RemoteAuthorizationConfig struct {
 Endpoint string
 ServerName string
 TrustRoots *x509.CertPool
 Certificate tls.Certificate
}
type RemoteAuthorizationClient struct{client *http.Client;endpoint string}

func NewRemoteAuthorizationClient(cfg RemoteAuthorizationConfig)(*RemoteAuthorizationClient,error){
 u,err:=url.Parse(cfg.Endpoint)
 if err!=nil||u.Scheme!="https"||u.Host==""||u.User!=nil||u.Opaque!=""||u.Fragment!=""||u.RawQuery!=""||(u.Path!=""&&u.Path!="/")||
  !ValidServiceDNS(cfg.ServerName)||cfg.TrustRoots==nil||len(cfg.Certificate.Certificate)==0||cfg.Certificate.PrivateKey==nil{
  return nil,errors.New("invalid remote authorizer configuration")
 }
 transport:=&http.Transport{Proxy:nil,DisableKeepAlives:true,
  DialContext:(&net.Dialer{Timeout:3*time.Second}).DialContext,
  TLSClientConfig:&tls.Config{MinVersion:tls.VersionTLS13,RootCAs:cfg.TrustRoots,ServerName:cfg.ServerName,Certificates:[]tls.Certificate{cfg.Certificate}},
  TLSHandshakeTimeout:5*time.Second,
  MaxResponseHeaderBytes:16<<10,
  ResponseHeaderTimeout:5*time.Second}
 client:=&http.Client{Transport:transport,Timeout:10*time.Second,CheckRedirect:func(*http.Request,[]*http.Request)error{return errors.New("authorization redirect denied")}}
 return &RemoteAuthorizationClient{client:client,endpoint:strings.TrimSuffix(u.String(),"/")},nil
}

// Authorize consults the independent owner's current revocable session.
// Network errors and malformed decisions always deny authorization.
func(c *RemoteAuthorizationClient)Authorize(ctx context.Context,operation,target string,arguments json.RawMessage)(Authorization,error){
 return c.requestDecision(ctx,"/v1/authorize",operation,target,arguments)
}

// Preview consults the same independent policy without consuming approval.
func(c *RemoteAuthorizationClient)Preview(ctx context.Context,operation,target string,arguments json.RawMessage)(Authorization,error){
 return c.requestDecision(ctx,"/v1/preview",operation,target,arguments)
}

func(c *RemoteAuthorizationClient)requestDecision(ctx context.Context,path,operation,target string,arguments json.RawMessage)(Authorization,error){
 assertion:=AccessAssertion(ctx)
 if c==nil||assertion==""{return Authorization{},errors.New("signed owner assertion required")}
 payload,err:=json.Marshal(struct{
  Operation string `json:"operation"`
  Target string `json:"target"`
  Arguments json.RawMessage `json:"arguments"`
 }{operation,target,arguments})
 if err!=nil{return Authorization{},err}
 request,err:=http.NewRequestWithContext(ctx,http.MethodPost,c.endpoint+path,bytes.NewReader(payload))
 if err!=nil{return Authorization{},err}
 request.Header.Set("Content-Type","application/json")
 request.Header.Set("Cf-Access-Jwt-Assertion",assertion)
 response,err:=c.client.Do(request)
 if err!=nil{return Authorization{},fmt.Errorf("remote security service unavailable: %w",err)}
 defer response.Body.Close()
 if response.StatusCode!=http.StatusOK||
  !strings.HasPrefix(response.Header.Get("Content-Type"),"application/json"){
  return Authorization{},errors.New("remote security service refused authorization")
 }
 const maxAuthorizationResponse=8192
 payload,err:=io.ReadAll(io.LimitReader(response.Body,maxAuthorizationResponse+1))
 if err!=nil||len(payload)>maxAuthorizationResponse{
  return Authorization{},errors.New("remote authorization response exceeds size limit")
 }
 var result Authorization
 decoder:=json.NewDecoder(bytes.NewReader(payload))
 decoder.DisallowUnknownFields()
 if err:=decoder.Decode(&result);err!=nil{return Authorization{},errors.New("invalid remote authorization result")}
 var extra any
 if err:=decoder.Decode(&extra);err!=io.EOF{return Authorization{},errors.New("invalid remote authorization result")}
 if result.Allowed&&(result.Reason!=""||result.ConfirmationID!=""||
  !result.ConfirmationExpiresAt.IsZero()){
  return Authorization{},errors.New("contradictory authorization approval")
 }
 if (!result.Allowed||path!="/v1/authorize")&&len(result.CredentialTickets)!=0{
  return Authorization{},errors.New("unexpected credential capabilities")
 }
 if len(result.CredentialTickets)>maxCredentialTicketsPerOperation{
  return Authorization{},errors.New("credential capability limit exceeded")
 }
 seen:=map[string]bool{}
 for _,ticket:=range result.CredentialTickets{
  if !serviceCredentialName.MatchString(ticket.Alias)||
   !serviceCredentialTicket.MatchString(ticket.Ticket)||seen[ticket.Alias]{
   return Authorization{},errors.New("invalid or duplicate credential capability")
  }
  seen[ticket.Alias]=true
 }
 return result,nil
}

// ServeAuthorizationTLS exposes only operation decisions to identified host
// executors; session login, revocation and credential custody stay local.
// remotePeerRoles separates operation authorization identities from
// credential-only peers. Every accepted TLS peer has exactly one role
// classification, and credential-only peers cannot invoke operation decisions.
func remotePeerRoles(authorizationClients,credentialClients []string)(map[string]struct{},map[string]struct{},error){
 if len(authorizationClients)==0||len(authorizationClients)+len(credentialClients)>16{
  return nil,nil,errors.New("remote TLS peers require an authorizer and a bounded identity list")
 }
 allowed:=make(map[string]struct{},len(authorizationClients)+len(credentialClients))
 authorizers:=make(map[string]struct{},len(authorizationClients))
 for _,client:=range authorizationClients{
  if !ValidServiceDNS(client){return nil,nil,errors.New("invalid authorization client identity")}
  if _,exists:=allowed[client];exists{return nil,nil,errors.New("duplicate remote TLS identity")}
  allowed[client]=struct{}{}
  authorizers[client]=struct{}{}
 }
 for _,client:=range credentialClients{
  if !ValidServiceDNS(client){return nil,nil,errors.New("invalid credential client identity")}
  if _,exists:=allowed[client];exists{return nil,nil,errors.New("remote TLS identities must have disjoint roles")}
  allowed[client]=struct{}{}
 }
 return allowed,authorizers,nil
}

// ServeAuthorizationTLS authenticates every peer and independently checks
// whether its pinned identity may invoke authorization or credential reads.
func(a *AdminSessions)ServeAuthorizationTLS(ctx context.Context,address string,serverCert tls.Certificate,clientRoots *x509.CertPool,authorizationClients,credentialClients []string)error{
 if a==nil||clientRoots==nil||len(serverCert.Certificate)==0||serverCert.PrivateKey==nil{
  return errors.New("remote authorization requires a certificate and exact client allowlist")
 }
 allowed,authorizers,err:=remotePeerRoles(authorizationClients,credentialClients)
 if err!=nil{return err}
 host,_,err:=net.SplitHostPort(address)
 if err!=nil{return errors.New("invalid remote authorization listener")}
 ip:=net.ParseIP(host)
 if ip==nil||!(ip.IsPrivate()||ip.IsLoopback()){return errors.New("authorization must bind to an explicit private IP")}
 listen,err:=net.Listen("tcp",address)
 if err!=nil{return err}
 defer listen.Close()
 config:=&tls.Config{
  MinVersion:tls.VersionTLS13,ClientAuth:tls.RequireAndVerifyClientCert,ClientCAs:clientRoots,
  Certificates:[]tls.Certificate{serverCert},
  VerifyConnection:func(state tls.ConnectionState)error{
   dns,verified:=verifiedServiceDNS(&state)
   if !verified{return errors.New("unverified or ambiguous service identity")}
   if _,ok:=allowed[dns];ok{return nil}
   return errors.New("unapproved remote service identity")
  },
 }
 authorization:=a.ClientHandler()
 authorizedOperation:=http.HandlerFunc(func(w http.ResponseWriter,r *http.Request){
  identity,verified:=verifiedServicePeer(r)
  if !verified{
   http.Error(w,"remote service identity not verified",http.StatusForbidden)
   return
  }
  if _,permitted:=authorizers[identity];!permitted{
   http.Error(w,"remote operation authorization denied",http.StatusForbidden)
   return
  }
  authorization.ServeHTTP(w,r.WithContext(context.WithValue(
   r.Context(),verifiedCredentialPeerContextKey{},identity)))
 })
 mux:=http.NewServeMux()
 mux.Handle("POST /v1/authorize",authorizedOperation)
 mux.Handle("POST /v1/preview",authorizedOperation)
 mux.Handle("POST /v1/service-credentials/{alias}",a.ServiceCredentialHandler())
 server:=&http.Server{Handler:mux,ReadHeaderTimeout:5*time.Second,ReadTimeout:10*time.Second,WriteTimeout:10*time.Second}
 stopped:=make(chan struct{})
 go func(){select{case <-ctx.Done():_=server.Close();case <-stopped:}}()
 err=server.Serve(tls.NewListener(listen,config));close(stopped)
 if errors.Is(err,http.ErrServerClosed){return nil}
 return err
}
