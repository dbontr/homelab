package security

import (
 "context"
 "crypto/rand"
 "encoding/hex"
 "errors"
 "html/template"
 "net"
 "net/http"
 "net/url"
 "mime"
 "os"
 "path/filepath"
 "strings"
 "sync"
 "time"
 "log"
)

// BrowserPortal serves password/TOTP forms on a separate Unix socket.
// The external HTTPS reverse proxy must authenticate the owner first.
type BrowserPortal struct {
 manager *AdminSessions
 origin string
 verify func(string)(string,error)
 mu sync.Mutex
 csrf map[string]time.Time
}

func NewBrowserPortal(manager *AdminSessions, origin string, verify func(string)(string,error)) (*BrowserPortal,error) {
 u,e:=url.Parse(origin)
 if manager==nil||verify==nil||e!=nil||u.Scheme!="https"||u.Host==""||u.User!=nil||u.RawQuery!=""||u.Fragment!=""||(u.Path!=""&&u.Path!="/"){
  return nil,errors.New("browser portal requires an exact HTTPS origin")
 }
 if u.Hostname()=="localhost"||net.ParseIP(u.Hostname())!=nil||strings.Contains(u.Host,"@"){
  return nil,errors.New("browser portal requires a named HTTPS host")
 }
 return &BrowserPortal{manager:manager,verify:verify,origin:"https://"+u.Host,csrf:map[string]time.Time{}},nil
}

var browserPage=template.Must(template.New("owner").Parse(`<!doctype html><html lang="en"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1"><title>Homelab administration</title><style>
:root{font-family:system-ui,sans-serif;color-scheme:dark;background:#101218;color:#edf0f4}body{margin:0 auto;max-width:42rem;padding:2rem 1rem}section{background:#1c2029;border:1px solid #373e49;border-radius:.75rem;padding:1.25rem;margin:1rem 0}p{line-height:1.5}label{display:block;margin:.8rem 0 .3rem}input{box-sizing:border-box;width:100%;padding:.7rem;background:#101218;border:1px solid #596171;border-radius:.35rem;color:white}button{background:#e8ecf3;color:#0b1019;border:0;border-radius:.4rem;padding:.7rem 1rem;margin-top:.9rem;font:inherit;cursor:pointer}small{color:#aeb6c4}code{overflow-wrap:anywhere}.error{color:#ffb4ae}.pending{border-top:1px solid #424855;margin-top:1rem;padding-top:1rem}
</style></head><body><main><h1>Homelab administration</h1><p>Enter your credentials on this trusted page. They are never sent through ChatGPT or an MCP tool.</p>
{{if .Message}}<p class="error" role="status">{{.Message}}</p>{{end}}
{{if .RequestID}}<section><h2>Request-scoped administration</h2><p>Reference code: <code>{{.ReferenceCode}}</code></p><p>This code identifies the request. It is not an access token or permission grant.</p>
{{if and .RequestCompleted .Session.Active}}<p role="status">Owner request approved. Return to your application; each operation remains independently authorized.</p>
{{else if .Session.Active}}<form method="post" action="/request/{{.RequestID}}/approve"><input type="hidden" name="csrf" value="{{.CSRF}}"><button type="submit">Approve this request</button></form>
{{else}}<form method="post" action="/request/{{.RequestID}}/login"><input type="hidden" name="csrf" value="{{.CSRF}}"><label for="request-password">Administrative password</label><input id="request-password" name="password" type="password" autocomplete="off" required><label for="request-totp">Authenticator code</label><input id="request-totp" name="totp" inputmode="numeric" pattern="[0-9]{6}" autocomplete="one-time-code" required><button type="submit">Authenticate and approve request</button></form>{{end}}</section>{{end}}
{{if .Session.Active}}
<section><h2>Administrative session active</h2><p>Idle expiry: {{.Session.IdleExpiresAt.Format "2006-01-02 15:04:05 MST"}}</p><p>Absolute expiry: {{.Session.AbsoluteExpiresAt.Format "2006-01-02 15:04:05 MST"}}</p>
<form method="post" action="/close"><input type="hidden" name="csrf" value="{{.CSRF}}"><button type="submit">Close administrative session</button></form></section>
<section><h2>Destructive operations</h2>{{if .Pending}}{{range .Pending}}<div class="pending"><strong>{{.Operation}}</strong> on <strong>{{.Target}}</strong><p><small>Redacted arguments:</small> <code>{{.Arguments}}</code></p><p><small>SHA-256: <code>{{.ArgumentsSHA256}}</code></small></p><p><small>Expires: {{.ExpiresAt.Format "2006-01-02 15:04:05 MST"}}</small></p>
{{if not .Confirmed}}<form method="post" action="/confirm/{{.ID}}"><input type="hidden" name="csrf" value="{{$.CSRF}}"><label for="pw-{{.ID}}">Password</label><input id="pw-{{.ID}}" name="password" type="password" autocomplete="off" required><label for="otp-{{.ID}}">New authenticator code</label><input id="otp-{{.ID}}" name="totp" inputmode="numeric" pattern="[0-9]{6}" autocomplete="one-time-code" required><button type="submit">Confirm this operation</button></form>{{else}}<p>Confirmed. Retry the unchanged operation in ChatGPT.</p>{{end}}</div>{{end}}{{else}}<p>No operations awaiting confirmation.</p>{{end}}</section>
{{else}}
<section><h2>Sign in</h2><p>Session lifetime: 120 minutes idle, 12 hours maximum.</p><form method="post" action="/login"><input type="hidden" name="csrf" value="{{.CSRF}}"><label for="password">Administrative password</label><input id="password" type="password" name="password" autocomplete="off" required><label for="totp">Six-digit authenticator code</label><input id="totp" name="totp" inputmode="numeric" pattern="[0-9]{6}" autocomplete="one-time-code" required><button type="submit">Unlock administration</button></form></section>
{{end}}<p><small>Use only on a trusted device. This page requires an independent owner-only Cloudflare Access policy.</small></p></main></body></html>`))

func(b *BrowserPortal) token()(string,error){
 var id [24]byte
 if _,err:=rand.Read(id[:]);err!=nil{return "",err}
 token:=hex.EncodeToString(id[:])
 b.mu.Lock();defer b.mu.Unlock()
 now:=time.Now()
 for id,expires:=range b.csrf{if !now.Before(expires){delete(b.csrf,id)}}
 if len(b.csrf)>=256{return "",errors.New("too many pending authentication forms")}
 b.csrf[token]=now.Add(10*time.Minute)
 return token,nil
}
func(b *BrowserPortal) take(token string)bool{
 b.mu.Lock();defer b.mu.Unlock()
 expires,ok:=b.csrf[token];delete(b.csrf,token)
 return ok&&time.Now().Before(expires)
}
func(b *BrowserPortal) render(w http.ResponseWriter,code int,message,principal string) {
 b.renderRequest(w,code,message,principal,"")
}

func(b *BrowserPortal) renderRequest(w http.ResponseWriter,code int,message,principal,requestID string){
 var completed bool
 if requestID!="" {
  var found bool
  completed,found=b.manager.OwnerRequestStatusFor(principal,requestID)
  if !found {http.Error(w,"owner request unavailable",http.StatusNotFound);return}
 }
 token,err:=b.token()
 if err!=nil{http.Error(w,"authentication temporarily unavailable",http.StatusServiceUnavailable);return}
 data:=struct{
  Session SessionStatus
  Pending []PendingConfirmation
  CSRF,Message,RequestID,ReferenceCode string
  RequestCompleted bool
 }{
  Session:b.manager.StatusFor(principal),Pending:b.manager.PendingFor(principal),
  CSRF:token,Message:message,RequestID:requestID,RequestCompleted:completed,
 }
 if requestID!=""{data.ReferenceCode=requestID[:12]}
 w.Header().Set("Content-Type","text/html; charset=utf-8")
 w.WriteHeader(code)
 if err:=browserPage.Execute(w,data);err!=nil{log.Print("security portal response error")}
}

// browserRequestRoute accepts only exact, lower-case request IDs.
func browserRequestRoute(path string)(id,action string,ok bool) {
 if !strings.HasPrefix(path,"/request/"){return "","",false}
 parts:=strings.Split(strings.TrimPrefix(path,"/request/"),"/")
 if len(parts)==0||len(parts)>2||!validOwnerRequestID(parts[0]){return "","",false}
 if len(parts)==1{return parts[0],"",true}
 if parts[1]!="login"&&parts[1]!="approve"{return "","",false}
 return parts[0],parts[1],true
}

// trustedBrowserForm accepts browser forms only when their source is the
// configured HTTPS origin. Fetch Metadata supplies a same-origin fallback for
// privacy-preserving browsers that omit Origin on a POST. The handler also
// requires a cryptographically verified owner and a one-use CSRF token.
func(b *BrowserPortal) trustedBrowserForm(r *http.Request)bool{
 mediaType,_,err:=mime.ParseMediaType(r.Header.Get("Content-Type"))
 if err!=nil||mediaType!="application/x-www-form-urlencoded"{return false}
 origin,site:=r.Header.Get("Origin"),r.Header.Get("Sec-Fetch-Site")
 // An exact HTTPS Origin takes precedence. Fetch Metadata may reflect a
 // cross-site identity-provider redirect; it is a fallback when Origin is absent.
 if origin!=""{return origin==b.origin}
 return site=="same-origin"
}

func(b *BrowserPortal) ServeHTTP(w http.ResponseWriter,r *http.Request){
 w.Header().Set("Cache-Control","no-store")
 w.Header().Set("Pragma","no-cache")
 w.Header().Set("X-Content-Type-Options","nosniff")
 w.Header().Set("Referrer-Policy","no-referrer")
 w.Header().Set("X-Frame-Options","DENY")
 w.Header().Set("Content-Security-Policy","default-src 'none'; style-src 'unsafe-inline'; form-action 'self'; frame-ancestors 'none'; base-uri 'none'")
 if r.Host!=strings.TrimPrefix(b.origin,"https://"){http.Error(w,"wrong host",http.StatusMisdirectedRequest);return}
 principal,err:=b.verify(r.Header.Get("Cf-Access-Jwt-Assertion"))
 if err!=nil||principal==""{http.Error(w,"verified owner identity required",http.StatusForbidden);return}
 requestID,requestAction,requestRoute:=browserRequestRoute(r.URL.Path)
 if r.Method==http.MethodGet {
  if r.URL.Path=="/"{b.render(w,http.StatusOK,"",principal);return}
  if requestRoute&&requestAction==""{b.renderRequest(w,http.StatusOK,"",principal,requestID);return}
  http.NotFound(w,r);return
 }
 if r.Method!=http.MethodPost|| !(r.URL.Path=="/login"||r.URL.Path=="/close"||strings.HasPrefix(r.URL.Path,"/confirm/")||
  (requestRoute&&(requestAction=="login"||requestAction=="approve"))) {
  http.NotFound(w,r);return
 }
 // Require same-origin form submission and a single-use CSRF value.
 if !b.trustedBrowserForm(r) {
  log.Print("security audit owner_form rejected")
  http.Error(w,"untrusted request",http.StatusForbidden);return
 }
 r.Body=http.MaxBytesReader(w,r.Body,4096)
 if err:=r.ParseForm();err!=nil||!b.take(r.PostForm.Get("csrf")) {
  http.Error(w,"invalid form",http.StatusForbidden);return
 }
 if requestRoute {
  if _,found:=b.manager.OwnerRequestStatusFor(principal,requestID);!found {
   http.Error(w,"owner request unavailable",http.StatusNotFound);return
  }
  switch requestAction {
  case "login":
   if _,err:=b.manager.AuthenticateFor(principal,r.PostForm.Get("password"),r.PostForm.Get("totp"));err!=nil{
    log.Print("security audit owner_request_login denied")
    b.renderRequest(w,http.StatusUnauthorized,"Authentication denied",principal,requestID);return
   }
  case "approve":
   if !b.manager.StatusFor(principal).Active {
    http.Error(w,"owner session unavailable",http.StatusForbidden);return
   }
  default:
   http.NotFound(w,r);return
  }
  if !b.manager.ApproveOwnerRequestFor(principal,requestID) {
   log.Print("security audit owner_request_approval denied")
   http.Error(w,"owner request unavailable",http.StatusForbidden);return
  }
  log.Print("security audit owner_request_approval accepted")
  http.Redirect(w,r,"/request/"+requestID,http.StatusSeeOther)
  return
 }
 switch {
 case r.URL.Path=="/login":
  if _,err:=b.manager.AuthenticateFor(principal,r.PostForm.Get("password"),r.PostForm.Get("totp"));err!=nil{
   log.Print("security audit web_authentication denied")
   b.render(w,http.StatusUnauthorized,"Authentication denied",principal);return
  }
  log.Print("security audit web_session opened")
 case r.URL.Path=="/close":
  b.manager.RevokeFor(principal)
  log.Print("security audit web_session revoked")
 default:
  id:=strings.TrimPrefix(r.URL.Path,"/confirm/")
  if !validOwnerRequestID(id){http.NotFound(w,r);return}
  if err:=b.manager.ConfirmFor(principal,id,r.PostForm.Get("password"),r.PostForm.Get("totp"));err!=nil{
   log.Print("security audit web_confirmation denied")
   b.render(w,http.StatusForbidden,"Confirmation denied",principal);return
  }
  log.Print("security audit web_confirmation accepted")
 }
 http.Redirect(w,r,"/",http.StatusSeeOther)
}

// ServeBrowserSocket exposes only the owner browser portal. It binds no TCP port.
func(b *BrowserPortal) ServeBrowserSocket(ctx context.Context,path string,groupID int)error{
 if groupID<=0||!filepath.IsAbs(path)||strings.ContainsRune(path,0){return errors.New("invalid browser socket")}
 dir:=filepath.Dir(path)
 info,err:=os.Lstat(dir)
 if err!=nil||!info.IsDir()||info.Mode().Perm()&0007!=0{return errors.New("browser socket directory is not private")}
 if err:=os.Chown(dir,-1,groupID);err!=nil{return err}
 if err:=os.Chmod(dir,0750);err!=nil{return err}
 if err:=reclaimStaleBrowserSocket(path,groupID);err!=nil{return err}
 listener,err:=net.Listen("unix",path);if err!=nil{return err}
 defer listener.Close();defer os.Remove(path)
 if err:=os.Chown(path,-1,groupID);err!=nil{return err}
 if err:=os.Chmod(path,0660);err!=nil{return err}
 server:=&http.Server{Handler:b,ReadHeaderTimeout:5*time.Second,ReadTimeout:15*time.Second,WriteTimeout:15*time.Second,IdleTimeout:30*time.Second}
 done:=make(chan struct{})
 go func(){select{case <-ctx.Done():_=server.Close();case <-done:}}()
 err=server.Serve(listener);close(done)
 if errors.Is(err,http.ErrServerClosed){return nil}
 return err
}
