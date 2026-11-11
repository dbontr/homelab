package release

import (
 "bytes"
 "context"
 "crypto/rand"
 "crypto/tls"
 "crypto/x509"
 "encoding/json"
 "encoding/hex"
 "errors"
 "fmt"
 "io"
 "net/http"
 "net/url"
 "strings"
 "time"
)

// RemoteAgentConfig is configured by the trusted release coordinator.
// Client certificates and owner assertions are never serialized in requests.
type RemoteAgentConfig struct {
 Host string
 Endpoint string
 ServerName string
 TrustRoots *x509.CertPool
 Certificate tls.Certificate
 AccessAssertion func(context.Context)string
}

type RemoteReleaseAgent struct{
 host string
 endpoint string
 assertion func(context.Context)string
 client *http.Client
}

func NewRemoteReleaseAgent(cfg RemoteAgentConfig)(*RemoteReleaseAgent,error){
 u,err:=url.Parse(cfg.Endpoint)
 if err!=nil||u.Scheme!="https"||u.Host==""||u.User!=nil||(u.Path!=""&&u.Path!="/")||u.RawQuery!=""||u.Fragment!=""||u.Opaque!=""||
 !fleetHostPattern.MatchString(cfg.Host)||cfg.ServerName==""||cfg.TrustRoots==nil||len(cfg.Certificate.Certificate)==0||cfg.Certificate.PrivateKey==nil||cfg.AccessAssertion==nil{
  return nil,errors.New("invalid remote release transport configuration")
 }
 transport:=&http.Transport{
  Proxy:nil,DisableKeepAlives:true,
  TLSClientConfig:&tls.Config{
   MinVersion:tls.VersionTLS13,ServerName:cfg.ServerName,RootCAs:cfg.TrustRoots,
   Certificates:[]tls.Certificate{cfg.Certificate},
  },
  ResponseHeaderTimeout:30*time.Second,
 }
 return &RemoteReleaseAgent{host:cfg.Host,endpoint:strings.TrimSuffix(u.String(),"/"),assertion:cfg.AccessAssertion,
  client:&http.Client{Transport:transport,Timeout:2*time.Hour,CheckRedirect:func(*http.Request,[]*http.Request)error{return errors.New("release agent redirect denied")}}},nil
}

func(a *RemoteReleaseAgent)call(ctx context.Context,method,path string,input,output any)error{
 assertion:=a.assertion(ctx)
 if assertion==""{return errors.New("verified owner identity required")}
 var body io.Reader
 if input!=nil{
  payload,err:=json.Marshal(input);if err!=nil{return err}
  body=bytes.NewReader(payload)
 }
 request,err:=http.NewRequestWithContext(ctx,method,a.endpoint+path,body)
 if err!=nil{return err}
 request.Header.Set("Cf-Access-Jwt-Assertion",assertion)
 if input!=nil{request.Header.Set("Content-Type","application/json")}
 response,err:=a.client.Do(request)
 if err!=nil{return fmt.Errorf("authenticated release transport unavailable: %w",err)}
 defer response.Body.Close()
 if response.StatusCode!=http.StatusOK{return fmt.Errorf("release agent rejected request: HTTP %d",response.StatusCode)}
 decoder:=json.NewDecoder(io.LimitReader(response.Body,8193))
 if err:=decoder.Decode(output);err!=nil{return errors.New("invalid release agent response")}
 var extra any
 if err:=decoder.Decode(&extra);err!=io.EOF{return errors.New("invalid release agent response")}
 return nil
}

// Explicit release execution IDs are stable across transport retries.
func validRemoteExecutionID(id string)bool{
 if len(id)!=32{return false}
 for _,c:=range id{
  if (c<'0'||c>'9')&&(c<'a'||c>'f'){return false}
 }
 return true
}

func(a *RemoteReleaseAgent)execute(ctx context.Context,operation,id string)error{
 var entropy [16]byte
 if _,err:=rand.Read(entropy[:]);err!=nil{return err}
 return a.executeWithID(ctx,hex.EncodeToString(entropy[:]),operation,id)
}

func(a *RemoteReleaseAgent)executeWithID(ctx context.Context,requestID,operation,id string)error{
 if !idPattern.MatchString(id)||!validRemoteExecutionID(requestID){
  return errors.New("invalid staged release or execution request identity")
 }
 arguments:=struct{
  Host string `json:"host"`
  ReleaseID string `json:"release_id"`
 }{a.host,id}
 request:=struct{
  RequestID string `json:"request_id"`
  Operation string `json:"operation"`
  Target string `json:"target"`
  Arguments any `json:"arguments"`
 }{requestID,operation,a.host,arguments}
 var response struct{
  Allowed bool `json:"allowed"`
  Reason string `json:"reason"`
  ConfirmationID string `json:"confirmation_id"`
  AuditID string `json:"audit_id"`
  State string `json:"state"`
  Replayed bool `json:"replayed"`
 }
 if err:=a.call(ctx,http.MethodPost,"/v1/execute",request,&response);err!=nil{return err}
 if !response.Allowed{
  if response.ConfirmationID!=""{return fmt.Errorf("owner confirmation required: %s",response.ConfirmationID)}
  return errors.New("independent authorization denied release operation")
 }
 if response.AuditID!=requestID||response.State!="completed"{
  return errors.New("release executor returned an unrelated or unverified execution receipt")
 }
 return nil
}

func(a *RemoteReleaseAgent)Activate(ctx context.Context,id string)error{return a.execute(ctx,"infrastructure__release_activate",id)}
func(a *RemoteReleaseAgent)Revert(ctx context.Context,id string)error{return a.execute(ctx,"infrastructure__release_revert",id)}

// A coordinator may retry these methods with the same request ID if the
// transport fails. The executor will return its recorded disposition without
// repeating a release activation or reversion.
func(a *RemoteReleaseAgent)ActivateWithID(ctx context.Context,requestID,id string)error{
 return a.executeWithID(ctx,requestID,"infrastructure__release_activate",id)
}
func(a *RemoteReleaseAgent)RevertWithID(ctx context.Context,requestID,id string)error{
 return a.executeWithID(ctx,requestID,"infrastructure__release_revert",id)
}

func(a *RemoteReleaseAgent)Observe(ctx context.Context)(ReleaseObservation,error){
 var result ReleaseObservation
 if err:=a.call(ctx,http.MethodGet,"/v1/release/status",nil,&result);err!=nil{return result,err}
 switch result.Phase{
 case "active","rolled-back","activating","reverting","rollback-failed":
 default:return ReleaseObservation{},errors.New("invalid remote release journal")
 }
 if !idPattern.MatchString(result.ID){return ReleaseObservation{},errors.New("invalid remote release journal")}
 return result,nil
}
