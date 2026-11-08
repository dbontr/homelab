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
  cfg.ServerName==""||cfg.TrustRoots==nil||len(cfg.Certificate.Certificate)==0||cfg.Certificate.PrivateKey==nil{
  return nil,errors.New("invalid remote authorizer configuration")
 }
 transport:=&http.Transport{Proxy:nil,DisableKeepAlives:true,
  TLSClientConfig:&tls.Config{MinVersion:tls.VersionTLS13,RootCAs:cfg.TrustRoots,ServerName:cfg.ServerName,Certificates:[]tls.Certificate{cfg.Certificate}},
  ResponseHeaderTimeout:5*time.Second}
 client:=&http.Client{Transport:transport,Timeout:10*time.Second,CheckRedirect:func(*http.Request,[]*http.Request)error{return errors.New("authorization redirect denied")}}
 return &RemoteAuthorizationClient{client:client,endpoint:strings.TrimSuffix(u.String(),"/")},nil
}

// Authorize consults the independent owner's current revocable session.
// Network errors and malformed decisions always deny authorization.
func(c *RemoteAuthorizationClient)Authorize(ctx context.Context,operation,target string,arguments json.RawMessage)(Authorization,error){
 assertion:=AccessAssertion(ctx)
 if c==nil||assertion==""{return Authorization{},errors.New("signed owner assertion required")}
 payload,err:=json.Marshal(struct{
  Operation string `json:"operation"`
  Target string `json:"target"`
  Arguments json.RawMessage `json:"arguments"`
 }{operation,target,arguments})
 if err!=nil{return Authorization{},err}
 request,err:=http.NewRequestWithContext(ctx,http.MethodPost,c.endpoint+"/v1/authorize",bytes.NewReader(payload))
 if err!=nil{return Authorization{},err}
 request.Header.Set("Content-Type","application/json")
 request.Header.Set("Cf-Access-Jwt-Assertion",assertion)
 response,err:=c.client.Do(request)
 if err!=nil{return Authorization{},fmt.Errorf("remote security service unavailable: %w",err)}
 defer response.Body.Close()
 if response.StatusCode!=http.StatusOK{return Authorization{},errors.New("remote security service refused authorization")}
 var result Authorization
 decoder:=json.NewDecoder(io.LimitReader(response.Body,8193));decoder.DisallowUnknownFields()
 if err:=decoder.Decode(&result);err!=nil{return Authorization{},errors.New("invalid remote authorization result")}
 var extra any
 if err:=decoder.Decode(&extra);err!=io.EOF{return Authorization{},errors.New("invalid remote authorization result")}
 return result,nil
}

// ServeAuthorizationTLS exposes only operation decisions to identified host
// executors; session login, revocation and credential custody stay local.
func(a *AdminSessions)ServeAuthorizationTLS(ctx context.Context,address string,serverCert tls.Certificate,clientRoots *x509.CertPool,allowedClients []string)error{
 if a==nil||clientRoots==nil||len(serverCert.Certificate)==0||serverCert.PrivateKey==nil||len(allowedClients)==0||len(allowedClients)>16{
  return errors.New("remote authorization requires a certificate and exact client allowlist")
 }
 host,_,err:=net.SplitHostPort(address)
 if err!=nil{return errors.New("invalid remote authorization listener")}
 ip:=net.ParseIP(host)
 if ip==nil||!(ip.IsPrivate()||ip.IsLoopback()){return errors.New("authorization must bind to an explicit private IP")}
 for _,client:=range allowedClients{if client==""||strings.ContainsAny(client," /\\:;"){return errors.New("invalid authorized client identity")}}
 listen,err:=net.Listen("tcp",address)
 if err!=nil{return err}
 defer listen.Close()
 config:=&tls.Config{
  MinVersion:tls.VersionTLS13,ClientAuth:tls.RequireAndVerifyClientCert,ClientCAs:clientRoots,
  Certificates:[]tls.Certificate{serverCert},
  VerifyConnection:func(state tls.ConnectionState)error{
   if len(state.VerifiedChains)==0||len(state.PeerCertificates)==0{return errors.New("unverified executor identity")}
   for _,dns:=range allowedClients{if state.PeerCertificates[0].VerifyHostname(dns)==nil{return nil}}
   return errors.New("unapproved executor identity")
  },
 }
 mux:=http.NewServeMux()
 mux.Handle("POST /v1/authorize",a.ClientHandler())
 server:=&http.Server{Handler:mux,ReadHeaderTimeout:5*time.Second,ReadTimeout:10*time.Second,WriteTimeout:10*time.Second}
 stopped:=make(chan struct{})
 go func(){select{case <-ctx.Done():_=server.Close();case <-stopped:}}()
 err=server.Serve(tls.NewListener(listen,config));close(stopped)
 if errors.Is(err,http.ErrServerClosed){return nil}
 return err
}
