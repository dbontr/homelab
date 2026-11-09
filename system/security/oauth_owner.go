package security

import (
 "context"
 "encoding/json"
 "errors"
 "log"
 "net"
 "net/http"
 "net/url"
 "sort"
 "strings"
 "time"
)

// SetOAuthBroker binds an allowlisted public-client PKCE registry to the
// independent vault. Neither provider codes nor tokens reach the MCP gateway.
func(a *AdminSessions)SetOAuthBroker(b *OAuthBroker,client *http.Client)error{
 if b==nil{return errors.New("OAuth provider registry is required")}
 callback,err:=b.CallbackListenAddress()
 if err!=nil||callback==""{return errors.New("OAuth callback must use one exact loopback listener")}
 a.mu.Lock()
 defer a.mu.Unlock()
 if a.vault==nil||a.oauth!=nil{return errors.New("OAuth requires an unused owner-only credential vault")}
 a.oauth=b
 a.oauthClient=client
 return nil
}

// CallbackListenAddress identifies the exact, local-only redirect endpoint.
// Every provider must share one listener; no public ingress is permitted.
func(b *OAuthBroker)CallbackListenAddress()(string,error){
 if b==nil{return "",errors.New("OAuth broker unavailable")}
 b.mu.Lock()
 defer b.mu.Unlock()
 if len(b.providers)==0{return "",errors.New("OAuth provider list is empty")}
 var address string
 for _,p:=range b.providers{
  target,err:=url.Parse(p.RedirectURL)
  if err!=nil||target.Scheme!="http"||target.Hostname()!="127.0.0.1"||
   target.Port()==""||target.Path!="/oauth/callback"||
   target.RawQuery!=""||target.Fragment!=""||
   target.User!=nil {
   return "",errors.New("OAuth redirect must be a fixed 127.0.0.1 callback")
  }
  if address==""{address=target.Host}
  if address!=target.Host{return "",errors.New("OAuth providers require one shared local callback")}
 }
 return address,nil
}

func(b *OAuthBroker)ConfiguredProviders()[]string{
 if b==nil{return nil}
 b.mu.Lock()
 defer b.mu.Unlock()
 names:=make([]string,0,len(b.providers))
 for name:=range b.providers{names=append(names,name)}
 sort.Strings(names)
 return names
}

// registerOAuthOwner binds only to the privileged local owner socket.
// Beginning an authorization requires fresh password and one-use TOTP.
func(a *AdminSessions)registerOAuthOwner(mux *http.ServeMux){
 mux.HandleFunc("GET /v1/oauth/providers",func(w http.ResponseWriter,_ *http.Request){
  if a.oauth==nil{http.Error(w,"OAuth providers are unavailable",503);return}
  respondJSON(w,200,map[string]any{"providers":a.oauth.ConfiguredProviders()})
 })
 mux.HandleFunc("POST /v1/oauth/{provider}/begin",func(w http.ResponseWriter,r *http.Request){
  if a.oauth==nil||a.vault==nil{http.Error(w,"OAuth provider integration unavailable",503);return}
  var input struct{
   Password string `json:"password"`
   TOTP string `json:"totp"`
  }
  if err:=readJSON(r,&input);err!=nil{http.Error(w,"invalid authorization request",400);return}
  if err:=a.VerifyOwnerFactors(input.Password,input.TOTP);err!=nil{
   log.Print("security audit oauth_enrollment denied")
   http.Error(w,"owner authentication denied",403);return
  }
  authorization,err:=a.oauth.Begin(r.PathValue("provider"))
  if err!=nil{http.Error(w,"OAuth provider unavailable",400);return}
  log.Print("security audit oauth_enrollment initiated")
  respondJSON(w,200,authorization)
 })
}

// OAuthCallbackHandler serves the exact provider redirect only over an
// explicitly loopback-bound listener, not over the browser or client sockets.
// The five-minute, single-use PKCE state is the CSRF and replay boundary.
func(a *AdminSessions)OAuthCallbackHandler()http.Handler{
 return privateHeaders(http.HandlerFunc(func(w http.ResponseWriter,r *http.Request){
  w.Header().Set("Content-Security-Policy","default-src 'none'; frame-ancestors 'none'")
  w.Header().Set("Content-Type","text/plain; charset=utf-8")
  if r.Method!=http.MethodGet||r.URL.Path!="/oauth/callback"||a.oauth==nil||a.vault==nil{
   http.Error(w,"OAuth callback unavailable",404);return
  }
  address,err:=a.oauth.CallbackListenAddress()
  if err!=nil||r.Host!=address{
   http.Error(w,"invalid callback destination",403);return
  }
  query:=r.URL.Query()
  if len(query)!=2||len(query["code"])!=1||len(query["state"])!=1{
   http.Error(w,"invalid callback parameters",400);return
  }
  code,state:=query.Get("code"),query.Get("state")
  if len(state)<32||len(state)>128||len(code)<1||len(code)>4096||
   strings.ContainsAny(state,"\r\n\x00"){
   http.Error(w,"invalid callback credentials",400);return
  }
  ctx,cancel:=context.WithTimeout(r.Context(),30*time.Second)
  defer cancel()
  handle,err:=a.oauth.CompleteAndStore(ctx,state,code,a.oauthClient,a.vault)
  if err!=nil{
   log.Print("security audit oauth_enrollment failed")
   http.Error(w,"OAuth enrollment failed; restart authorization",400);return
  }
  if handle.ID==""{
   http.Error(w,"OAuth credential storage incomplete",500);return
  }
  log.Print("security audit oauth_credential stored")
  respondJSON(w,200,map[string]any{
   "connected":true,
   "message":"Provider authorization completed in the independent credential service.",
  })
 }))
}

// ServeOAuthCallback binds only to the fixed IPv4 loopback address declared
// by the allowlisted OAuth providers. It never exposes the callback through
// the gateway, browser ingress or a wildcard network listener.
func(a *AdminSessions)ServeOAuthCallback(ctx context.Context)error{
 if a.oauth==nil||a.vault==nil{return errors.New("OAuth callback is not configured")}
 address,err:=a.oauth.CallbackListenAddress()
 if err!=nil{return err}
 listener,err:=net.Listen("tcp4",address)
 if err!=nil{return errors.New("OAuth loopback callback unavailable")}
 defer listener.Close()
 server:=&http.Server{
  Handler:a.OAuthCallbackHandler(),
  ReadHeaderTimeout:5*time.Second,
  ReadTimeout:10*time.Second,
  WriteTimeout:35*time.Second,
  IdleTimeout:10*time.Second,
 }
 finished:=make(chan struct{})
 go func(){
  select{
  case <-ctx.Done(): _=server.Close()
  case <-finished:
  }
 }()
 defer close(finished)
 err=server.Serve(listener)
 if errors.Is(err,http.ErrServerClosed){return nil}
 return err
}
