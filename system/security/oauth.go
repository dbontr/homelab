package security

import (
 "context"
 "crypto/rand"
 "crypto/sha256"
 "encoding/base64"
 "errors"
 "net/url"
 "strings"
 "sync"
 "time"
)

type Provider struct {
 Name string
 AuthorizeURL string
 TokenURL string
 ClientID string
 RedirectURL string
 Scopes []string
}

type Authorization struct {
 URL string `json:"url"`
 ExpiresAt time.Time `json:"expires_at"`
}

type oauthSession struct {
 provider Provider
 verifier string
 expires time.Time
}

// OAuthBroker keeps authorization codes and PKCE verifiers in a trusted local process.
type OAuthBroker struct {
 mu sync.Mutex
 providers map[string]Provider
 sessions map[string]oauthSession
 now func() time.Time
}

func NewOAuthBroker(providers []Provider)(*OAuthBroker,error){
 b:=&OAuthBroker{providers:map[string]Provider{},sessions:map[string]oauthSession{},now:time.Now}
 for _,p:=range providers{
  auth,err1:=url.Parse(p.AuthorizeURL)
  redirect,err2:=url.Parse(p.RedirectURL)
  if p.Name==""||len(p.Name)>64||b.providers[p.Name].Name!=""||err1!=nil||err2!=nil||
    auth.Scheme!="https"||auth.Host==""||auth.User!=nil||auth.Fragment!=""||
    redirect.Scheme!="http"||(redirect.Hostname()!="127.0.0.1"&&redirect.Hostname()!="localhost"&&redirect.Hostname()!="::1")||
    p.ClientID==""||redirect.Fragment!=""||redirect.User!=nil{
     return nil,errors.New("invalid OAuth provider configuration")
  }
  b.providers[p.Name]=p
 }
 return b,nil
}

func randomURLToken(size int)(string,error){
 b:=make([]byte,size)
 if _,err:=rand.Read(b);err!=nil{return "",err}
 return base64.RawURLEncoding.EncodeToString(b),nil
}

func(b *OAuthBroker)Begin(provider string)(Authorization,error){
 b.mu.Lock();defer b.mu.Unlock()
 p,ok:=b.providers[provider];if !ok{return Authorization{},errors.New("unknown OAuth provider")}
 state,err:=randomURLToken(32);if err!=nil{return Authorization{},err}
 verifier,err:=randomURLToken(32);if err!=nil{return Authorization{},err}
 sum:=sha256.Sum256([]byte(verifier))
 challenge:=base64.RawURLEncoding.EncodeToString(sum[:])
 u,err:=url.Parse(p.AuthorizeURL);if err!=nil{return Authorization{},err}
 q:=u.Query()
 q.Set("response_type","code")
 q.Set("client_id",p.ClientID)
 q.Set("redirect_uri",p.RedirectURL)
 q.Set("scope",strings.Join(p.Scopes," "))
 q.Set("state",state)
 q.Set("code_challenge",challenge)
 q.Set("code_challenge_method","S256")
 u.RawQuery=q.Encode()
 expires:=b.now().Add(5*time.Minute)
 for key,session:=range b.sessions{
  if !b.now().Before(session.expires){delete(b.sessions,key)}
 }
 if len(b.sessions)>=64{return Authorization{},errors.New("too many pending OAuth sessions")}
 b.sessions[state]=oauthSession{provider:p,verifier:verifier,expires:expires}
 return Authorization{URL:u.String(),ExpiresAt:expires},nil
}

// Finish is called by the trusted loopback callback. Exchange stores tokens
// in the protected broker and never returns them through MCP or chat.
func(b *OAuthBroker)Finish(ctx context.Context,state,code string,exchange func(context.Context,Provider,string,string)error)error{
 if code==""||len(code)>4096||exchange==nil{return errors.New("invalid OAuth callback")}
 b.mu.Lock()
 session,ok:=b.sessions[state]
 delete(b.sessions,state)
 b.mu.Unlock()
 if !ok||!b.now().Before(session.expires){return errors.New("unknown or expired OAuth state")}
 return exchange(ctx,session.provider,code,session.verifier)
}
