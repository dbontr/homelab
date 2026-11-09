package security

import (
 "bytes"
 "context"
 "encoding/json"
 "errors"
 "io"
 "net/http"
 "net/url"
 "strings"
 "time"
)

// OAuthDeviceAuthorization contains only the public verification instructions
// and an opaque owner-socket request ID. The provider's device_code stays in
// the independent security process and is never returned to an MCP client.
type OAuthDeviceAuthorization struct {
 ID string `json:"id"`
 VerificationURL string `json:"verification_url"`
 VerificationURLComplete string `json:"verification_url_complete,omitempty"`
 UserCode string `json:"user_code"`
 ExpiresAt time.Time `json:"expires_at"`
 PollAfterSeconds int `json:"poll_after_seconds"`
}

type OAuthDeviceStatus struct {
 State string `json:"state"`
 RetryAfterSeconds int `json:"retry_after_seconds,omitempty"`
 Credential *CredentialHandle `json:"credential,omitempty"`
}

type oauthDeviceSession struct {
 Provider Provider
 DeviceCode string
 ExpiresAt time.Time
 Interval time.Duration
 NextPoll time.Time
 Polling bool
}

func oauthDeviceClient(client *http.Client)*http.Client{
 if client==nil{
  client=&http.Client{Timeout:15*time.Second,
   Transport:&http.Transport{Proxy:nil,TLSHandshakeTimeout:5*time.Second}}
 }
 safe:=*client
 safe.CheckRedirect=func(*http.Request,[]*http.Request)error{
  return errors.New("OAuth provider redirects are prohibited")
 }
 return &safe
}

func oauthDevicePost(ctx context.Context,client *http.Client,endpoint string,form url.Values)(int,[]byte,error){
 target,err:=url.Parse(endpoint)
 if err!=nil||target.Scheme!="https"||target.Host==""||
  target.User!=nil||target.RawQuery!=""||target.Fragment!=""||target.Opaque!=""{
  return 0,nil,errors.New("invalid OAuth device endpoint")
 }
 req,err:=http.NewRequestWithContext(ctx,http.MethodPost,target.String(),strings.NewReader(form.Encode()))
 if err!=nil{return 0,nil,errors.New("invalid OAuth device request")}
 req.Header.Set("Content-Type","application/x-www-form-urlencoded")
 req.Header.Set("Accept","application/json")
 response,err:=oauthDeviceClient(client).Do(req)
 if err!=nil{return 0,nil,errors.New("OAuth device provider unavailable")}
 defer response.Body.Close()
 if !strings.HasPrefix(response.Header.Get("Content-Type"),"application/json"){
  return 0,nil,errors.New("invalid OAuth device response type")
 }
 body,err:=io.ReadAll(io.LimitReader(response.Body,32769))
 if err!=nil||len(body)>32768{return 0,nil,errors.New("invalid OAuth device response size")}
 return response.StatusCode,body,nil
}

func trustedVerificationURL(raw,host string)bool{
 if raw==""||len(raw)>2048{return false}
 target,err:=url.Parse(raw)
 return err==nil&&target.Scheme=="https"&&target.Host!=""&&
  strings.EqualFold(target.Hostname(),host)&&target.User==nil&&
  target.Fragment==""&&target.Opaque==""
}

// BeginDevice requests a short-lived provider device code after the owner
// has authenticated to the private socket. A code never leaves this broker.
func(b *OAuthBroker)BeginDevice(ctx context.Context,providerName string,client *http.Client)(OAuthDeviceAuthorization,error){
 if b==nil{return OAuthDeviceAuthorization{},errors.New("OAuth broker unavailable")}
 b.mu.Lock()
 provider,ok:=b.providers[providerName]
 b.mu.Unlock()
 if !ok||provider.DeviceAuthorizationURL==""{
  return OAuthDeviceAuthorization{},errors.New("provider does not support device authorization")
 }
 form:=url.Values{"client_id":{provider.ClientID}}
 if len(provider.Scopes)>0{form.Set("scope",strings.Join(provider.Scopes," "))}
 status,body,err:=oauthDevicePost(ctx,client,provider.DeviceAuthorizationURL,form)
 if err!=nil||status!=http.StatusOK{
  return OAuthDeviceAuthorization{},errors.New("OAuth device authorization request refused")
 }
 var result struct {
  DeviceCode string `json:"device_code"`
  UserCode string `json:"user_code"`
  VerificationURI string `json:"verification_uri"`
  VerificationURIComplete string `json:"verification_uri_complete"`
  ExpiresIn int `json:"expires_in"`
  Interval int `json:"interval"`
 }
 if err:=json.Unmarshal(body,&result);err!=nil||
  len(result.DeviceCode)<8||len(result.DeviceCode)>4096||
  result.UserCode==""||len(result.UserCode)>128||
  strings.ContainsAny(result.DeviceCode,"\r\n\x00")||
  strings.ContainsAny(result.UserCode,"\r\n\x00")||
  result.ExpiresIn<30||result.ExpiresIn>900||
  !trustedVerificationURL(result.VerificationURI,provider.DeviceVerificationHost)||
  result.VerificationURIComplete!=""&&
   !trustedVerificationURL(result.VerificationURIComplete,provider.DeviceVerificationHost){
  return OAuthDeviceAuthorization{},errors.New("invalid OAuth device authorization instructions")
 }
 interval:=result.Interval
 if interval==0{interval=5}
 if interval<5||interval>60{
  return OAuthDeviceAuthorization{},errors.New("unsupported OAuth device polling interval")
 }
 id,err:=randomURLToken(24)
 if err!=nil{return OAuthDeviceAuthorization{},err}
 b.mu.Lock()
 defer b.mu.Unlock()
 now:=b.now()
 for key,session:=range b.deviceSessions{
  if !now.Before(session.ExpiresAt)&&!session.Polling{delete(b.deviceSessions,key)}
 }
 if len(b.deviceSessions)>=64{
  return OAuthDeviceAuthorization{},errors.New("too many pending OAuth device sessions")
 }
 expires:=now.Add(time.Duration(result.ExpiresIn)*time.Second)
 b.deviceSessions[id]=oauthDeviceSession{
  Provider:provider,DeviceCode:result.DeviceCode,
  ExpiresAt:expires,Interval:time.Duration(interval)*time.Second,
  NextPoll:now.Add(time.Duration(interval)*time.Second),
 }
 return OAuthDeviceAuthorization{
  ID:id,VerificationURL:result.VerificationURI,
  VerificationURLComplete:result.VerificationURIComplete,
  UserCode:result.UserCode,ExpiresAt:expires,PollAfterSeconds:interval,
 },nil
}

// PollDevice is available only on the owner socket, and enforces provider
// polling intervals even when the trusted CLI is invoked repeatedly.
func(b *OAuthBroker)PollDevice(ctx context.Context,providerName,id string,
 client *http.Client,vault *Vault)(OAuthDeviceStatus,error){
 if b==nil||vault==nil||len(id)!=32{
  return OAuthDeviceStatus{},errors.New("invalid OAuth device poll")
 }
 b.mu.Lock()
 session,ok:=b.deviceSessions[id]
 if !ok||session.Provider.Name!=providerName{
  b.mu.Unlock()
  return OAuthDeviceStatus{},errors.New("unknown OAuth device authorization")
 }
 now:=b.now()
 if !now.Before(session.ExpiresAt){
  delete(b.deviceSessions,id)
  b.mu.Unlock()
  return OAuthDeviceStatus{},errors.New("OAuth device authorization expired")
 }
 if session.Polling{
  b.mu.Unlock()
  return OAuthDeviceStatus{},errors.New("OAuth device authorization is already polling")
 }
 if now.Before(session.NextPoll){
  delay:=int(session.NextPoll.Sub(now)/time.Second)
  if delay<1{delay=1}
  b.mu.Unlock()
  return OAuthDeviceStatus{State:"pending",RetryAfterSeconds:delay},nil
 }
 session.Polling=true
 session.NextPoll=now.Add(session.Interval)
 b.deviceSessions[id]=session
 b.mu.Unlock()
 form:=url.Values{
  "grant_type":{"urn:ietf:params:oauth:grant-type:device_code"},
  "device_code":{session.DeviceCode},
  "client_id":{session.Provider.ClientID},
 }
 status,body,requestErr:=oauthDevicePost(ctx,client,session.Provider.TokenURL,form)
 result:=OAuthDeviceStatus{State:"pending",RetryAfterSeconds:int(session.Interval/time.Second)}
 var handle CredentialHandle
 terminal:=false
 slowDown:=false
 if requestErr==nil{
  switch status{
  case http.StatusOK:
   var token struct{
    AccessToken string `json:"access_token"`
    TokenType string `json:"token_type"`
   }
   if json.Unmarshal(body,&token)==nil&&token.AccessToken!=""&&
    strings.EqualFold(token.TokenType,"Bearer"){
    handle,requestErr=vault.Store(providerName,bytes.Clone(body))
    if requestErr==nil{
     result=OAuthDeviceStatus{State:"authorized",Credential:&handle}
    }
   }else{
    requestErr=errors.New("invalid OAuth device token response")
   }
   terminal=true
  case http.StatusBadRequest:
   var denied struct{Error string `json:"error"`}
   if json.Unmarshal(body,&denied)!=nil{
    requestErr=errors.New("invalid OAuth device error response")
    terminal=true
   }else{
    switch denied.Error{
    case "authorization_pending":
    case "slow_down":slowDown=true
    case "access_denied","expired_token":
     requestErr=errors.New("OAuth device authorization denied or expired")
     terminal=true
    default:
     requestErr=errors.New("OAuth device provider rejected token exchange")
     terminal=true
    }
   }
  default:
   requestErr=errors.New("OAuth device provider unavailable")
  }
 }
 b.mu.Lock()
 if current,exists:=b.deviceSessions[id];exists&&current.Polling{
  if terminal{
   delete(b.deviceSessions,id)
  }else{
   current.Polling=false
   if slowDown{
    current.Interval+=5*time.Second
    if current.Interval>60*time.Second{current.Interval=60*time.Second}
    current.NextPoll=b.now().Add(current.Interval)
   }
   b.deviceSessions[id]=current
   result.RetryAfterSeconds=int(current.Interval/time.Second)
  }
 }
 b.mu.Unlock()
 if requestErr!=nil{return OAuthDeviceStatus{},requestErr}
 return result,nil
}
