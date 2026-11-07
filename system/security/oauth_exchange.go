package security

import (
 "bytes"
 "context"
 "encoding/json"
 "errors"
 "fmt"
 "io"
 "net/http"
 "net/url"
 "strings"
 "time"
)

// ExchangeOAuthCode runs only inside a trusted broker with access to its vault.
func ExchangeOAuthCode(ctx context.Context,provider Provider,code,verifier string,client *http.Client,vault *Vault)(CredentialHandle,error){
 if vault==nil||provider.TokenURL==""||code==""||verifier==""{
   return CredentialHandle{},errors.New("provider token exchange is not configured")
 }
 target,err:=url.Parse(provider.TokenURL)
 if err!=nil||target.Scheme!="https"||target.Host==""||target.User!=nil||target.Fragment!=""{
   return CredentialHandle{},errors.New("invalid token endpoint")
 }
 params:=url.Values{
   "grant_type":{"authorization_code"},
   "code":{code},
   "client_id":{provider.ClientID},
   "redirect_uri":{provider.RedirectURL},
   "code_verifier":{verifier},
 }
 req,err:=http.NewRequestWithContext(ctx,http.MethodPost,target.String(),strings.NewReader(params.Encode()))
 if err!=nil{return CredentialHandle{},err}
 req.Header.Set("Content-Type","application/x-www-form-urlencoded")
 req.Header.Set("Accept","application/json")
 if client==nil{client=&http.Client{Timeout:15*time.Second}}
 safeClient:=*client
 safeClient.CheckRedirect=func(*http.Request,[]*http.Request)error{return errors.New("token redirects are not permitted")}
 resp,err:=safeClient.Do(req)
 if err!=nil{return CredentialHandle{},fmt.Errorf("token exchange failed: %w",err)}
 defer resp.Body.Close()
 if resp.StatusCode!=http.StatusOK||!strings.HasPrefix(resp.Header.Get("Content-Type"),"application/json"){
   return CredentialHandle{},errors.New("provider refused token exchange")
 }
 body,err:=io.ReadAll(io.LimitReader(resp.Body,32769))
 if err!=nil||len(body)>32768{return CredentialHandle{},errors.New("invalid provider token response size")}
 var token struct {AccessToken string `json:"access_token"`; TokenType string `json:"token_type"`}
 if err:=json.Unmarshal(body,&token);err!=nil||token.AccessToken==""||!strings.EqualFold(token.TokenType,"Bearer"){
   return CredentialHandle{},errors.New("provider returned an invalid token response")
 }
 return vault.Store(provider.Name,bytes.Clone(body))
}

// CompleteAndStore consumes the local PKCE state and returns only an opaque handle.
func (b *OAuthBroker) CompleteAndStore(ctx context.Context,state,code string,client *http.Client,vault *Vault)(CredentialHandle,error){
 var handle CredentialHandle
 err:=b.Finish(ctx,state,code,func(ctx context.Context,p Provider,code,verifier string)error{
   var err error
   handle,err=ExchangeOAuthCode(ctx,p,code,verifier,client,vault)
   return err
 })
 return handle,err
}
