package security

import (
 "context"
 "encoding/json"
 "errors"
 "io"
 "net/http"
 "net/url"
 "strings"
 "time"
)

// RefreshOAuthCredential refreshes an existing public-client OAuth credential
// inside the trusted broker. The refresh token and replacement access token
// never leave the encrypted vault or the outbound provider request. The
// credential handle is stable, but all old scoped leases are invalidated.
func RefreshOAuthCredential(ctx context.Context,provider Provider,handle string,client *http.Client,vault *Vault)error{
 if vault==nil||handle==""||provider.Name==""||provider.ClientID==""{
  return errors.New("provider refresh is not configured")
 }
 endpoint,err:=url.Parse(provider.TokenURL)
 if err!=nil||endpoint.Scheme!="https"||endpoint.Host==""||endpoint.User!=nil||
  endpoint.Fragment!=""||endpoint.Opaque!=""||endpoint.RawQuery!=""{
  return errors.New("invalid OAuth refresh endpoint")
 }
 if client==nil{
  client=&http.Client{Timeout:15*time.Second,
   Transport:&http.Transport{Proxy:nil,TLSHandshakeTimeout:5*time.Second}}
 }
 safeClient:=*client
 safeClient.CheckRedirect=func(*http.Request,[]*http.Request)error{return errors.New("OAuth token redirects are forbidden")}
 return vault.UpdateCredential(handle,provider.Name,func(current []byte)([]byte,error){
  var stored map[string]json.RawMessage
  if err:=json.Unmarshal(current,&stored);err!=nil||stored==nil{return nil,errors.New("invalid stored OAuth credential")}
  var oldRefresh string
  if err:=json.Unmarshal(stored["refresh_token"],&oldRefresh);err!=nil||oldRefresh==""{
   return nil,errors.New("OAuth credential has no refresh grant")
  }
  form:=url.Values{
   "grant_type":{"refresh_token"},
   "refresh_token":{oldRefresh},
   "client_id":{provider.ClientID},
  }
  request,err:=http.NewRequestWithContext(ctx,http.MethodPost,endpoint.String(),strings.NewReader(form.Encode()))
  if err!=nil{return nil,err}
  request.Header.Set("Content-Type","application/x-www-form-urlencoded")
  request.Header.Set("Accept","application/json")
  response,err:=safeClient.Do(request)
  if err!=nil{return nil,errors.New("OAuth token refresh transport unavailable")}
  defer response.Body.Close()
  if response.StatusCode!=http.StatusOK||!strings.HasPrefix(response.Header.Get("Content-Type"),"application/json"){
   return nil,errors.New("OAuth provider rejected refresh request")
  }
  body,err:=io.ReadAll(io.LimitReader(response.Body,32769))
  if err!=nil||len(body)>32768{return nil,errors.New("OAuth refresh response exceeds limit")}
  var refreshed struct{
   AccessToken string `json:"access_token"`
   TokenType string `json:"token_type"`
   RefreshToken string `json:"refresh_token"`
  }
  if err:=json.Unmarshal(body,&refreshed);err!=nil||refreshed.AccessToken==""||
   !strings.EqualFold(refreshed.TokenType,"Bearer"){
   return nil,errors.New("invalid OAuth refresh response")
  }
  var updates map[string]json.RawMessage
  if err:=json.Unmarshal(body,&updates);err!=nil||updates==nil{return nil,errors.New("invalid OAuth refresh payload")}
  // RFC 6749 allows a refresh response to omit a replacement refresh token.
  // Retain the original only when a new nonempty token was not issued.
  if refreshed.RefreshToken==""{delete(updates,"refresh_token")}
  for key,value:=range updates{stored[key]=value}
  delete(stored,"error")
  return json.Marshal(stored)
 })
}
