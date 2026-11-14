// Package bridge provides narrowly scoped HTTP transports for optional
// interfaces. Domain services remain the source of truth for every operation.
package bridge

import (
 "crypto/subtle"
 "errors"
 "fmt"
 "io"
 "net"
 "net/http"
 "net/http/httputil"
 "net/url"
 "os"
 "path/filepath"
 "strings"
 "syscall"
 "time"
)

type Surface string

const (
 MCP Surface = "mcp"
 API Surface = "api"
)

const maxRequestBytes = 16 << 20

type Config struct {
 Surface Surface
 BackendURL string
 ClientToken string
 ServiceToken string
 Origins []string
 WritePaths []string
}

// New creates an adapter with one fixed private service origin. An interface
// cannot use arbitrary request headers, paths or query parameters to select a
// backend, acquire a service credential or bypass its own admission policy.
func New(cfg Config)(http.Handler,error){
 if cfg.Surface!=MCP&&cfg.Surface!=API{
  return nil,errors.New("unsupported interface surface")
 }
 backend,err:=url.Parse(cfg.BackendURL)
 if err!=nil||backend.Scheme!="http"||backend.Host==""||
  backend.User!=nil||backend.Opaque!=""||backend.Fragment!=""||
  backend.RawQuery!=""||(backend.Path!=""&&backend.Path!="/"){
  return nil,errors.New("service origin must be exact loopback HTTP")
 }
 ip:=net.ParseIP(backend.Hostname())
 if ip==nil||!ip.IsLoopback()||backend.Port()==""{
  return nil,errors.New("service origin must use a literal loopback IP and port")
 }
 if !validToken(cfg.ClientToken)||!validToken(cfg.ServiceToken)||
  subtle.ConstantTimeCompare([]byte(cfg.ClientToken),[]byte(cfg.ServiceToken))==1{
  return nil,errors.New("interface and service require distinct protected credentials")
 }
 origins:=make(map[string]bool,len(cfg.Origins))
 for _,raw:=range cfg.Origins{
  parsed,err:=url.Parse(raw)
  if err!=nil||parsed.Host==""||parsed.User!=nil||parsed.Opaque!=""||
   parsed.RawQuery!=""||parsed.Fragment!=""||(parsed.Path!=""&&parsed.Path!="/")||
   (parsed.Scheme!="https"&&!(parsed.Scheme=="http"&&loopbackHost(parsed.Hostname()))){
   return nil,errors.New("invalid interface Origin allowlist")
  }
  origins[parsed.Scheme+"://"+parsed.Host]=true
 }
 if len(cfg.WritePaths)>32{return nil,errors.New("API write allowlist exceeds limit")}
 writePaths:=map[string]bool{}
 for _,p:=range cfg.WritePaths{
  if cfg.Surface!=API||!validWritePath(p)||writePaths[p]{
   return nil,errors.New("invalid API write allowlist")
  }
  writePaths[p]=true
 }
 target:=*backend
 target.Path=""
 target.RawPath=""
 proxy:=&httputil.ReverseProxy{
  Rewrite:func(pr *httputil.ProxyRequest){
   pr.SetURL(&target)
   // The upstream token is the identity of this interface, not the caller.
   // Caller-controlled forwarding headers must not acquire service authority.
   out:=pr.Out.Header
   out.Del("Authorization")
   out.Del("Cookie")
   // Origin is verified at the interface boundary, not delegated to the
   // private service's browser-origin policy.
   out.Del("Origin")
   out.Del("Proxy-Authorization")
   out.Del("Forwarded")
   for name:=range out{
    if strings.HasPrefix(strings.ToLower(name),"x-forwarded-")||
     strings.EqualFold(name,"X-Original-URL")||
     strings.EqualFold(name,"X-Rewrite-URL"){
     out.Del(name)
    }
   }
   out.Set("Authorization","Bearer "+cfg.ServiceToken)
   pr.Out.Host=target.Host
  },
  Transport:&http.Transport{
   Proxy:nil,
   DialContext:(&net.Dialer{Timeout:5*time.Second,KeepAlive:30*time.Second}).DialContext,
   MaxIdleConnsPerHost:8,
   MaxResponseHeaderBytes:64<<10,
   ResponseHeaderTimeout:120*time.Second,
  },
  // Internal services cannot grant browser state or relax the interface's
  // cache and framing policy through response headers.
  ModifyResponse:func(resp *http.Response)error{
   resp.Header.Del("Set-Cookie")
   resp.Header.Set("Cache-Control","no-store")
   resp.Header.Set("X-Content-Type-Options","nosniff")
   resp.Header.Set("Referrer-Policy","no-referrer")
   resp.Header.Set("X-Frame-Options","DENY")
   return nil
  },
  FlushInterval:-1,
  ErrorHandler:func(w http.ResponseWriter,_ *http.Request,_ error){
   http.Error(w,"service unavailable",http.StatusServiceUnavailable)
  },
 }
 return http.HandlerFunc(func(w http.ResponseWriter,r *http.Request){
  w.Header().Set("Cache-Control","no-store")
  w.Header().Set("X-Content-Type-Options","nosniff")
  w.Header().Set("Referrer-Policy","no-referrer")
  w.Header().Set("X-Frame-Options","DENY")
  if r.Method==http.MethodGet&&r.URL.Path=="/healthz"{
   w.Header().Set("Content-Type","application/json")
   _,_=io.WriteString(w,`{"status":"ok"}`)
   return
  }
  if r.URL.IsAbs()||r.URL.Host!=""||!validRequestPath(r.URL.Path,r.URL.EscapedPath()){
   http.Error(w,"invalid interface route",http.StatusNotFound)
   return
  }
  if origin:=r.Header.Get("Origin");origin!=""&&!origins[origin]{
   http.Error(w,"unapproved origin",http.StatusForbidden)
   return
  }
  if !strings.HasPrefix(r.Header.Get("Authorization"),"Bearer ")||
   subtle.ConstantTimeCompare([]byte(r.Header.Get("Authorization")),
    []byte("Bearer "+cfg.ClientToken))!=1{
   w.Header().Set("WWW-Authenticate",`Bearer realm="homelab"`)
   http.Error(w,"interface authorization required",http.StatusUnauthorized)
   return
  }
  if cfg.Surface==MCP{
   if r.URL.Path!="/mcp"||r.Method!=http.MethodPost{
    http.NotFound(w,r)
    return
   }
  }else{
   if !strings.HasPrefix(r.URL.Path,"/v1/")||
    isPrivilegedToolPath(r.URL.Path){
    http.NotFound(w,r)
    return
   }
   if r.Method!=http.MethodGet&&r.Method!=http.MethodHead{
    if !writable(writePaths,r.URL.Path)||
     (r.Method!=http.MethodPost&&r.Method!=http.MethodPut&&
      r.Method!=http.MethodPatch&&r.Method!=http.MethodDelete){
     http.Error(w,"API operation not admitted",http.StatusForbidden)
     return
    }
   }
  }
  r.Body=http.MaxBytesReader(w,r.Body,maxRequestBytes)
  proxy.ServeHTTP(w,r)
 }),nil
}

func validToken(token string)bool{
 return len(token)>=32&&len(token)<=4096&&
  !strings.ContainsAny(token,"\x00\r\n\t ")
}

func loopbackHost(host string)bool{
 ip:=net.ParseIP(host)
 return ip!=nil&&ip.IsLoopback()
}

func validWritePath(path string)bool{
 if !strings.HasPrefix(path,"/v1/")||strings.HasSuffix(path,"/")||
  isPrivilegedToolPath(path)||strings.Contains(path,"//")||
  strings.ContainsAny(path,"\\%?#\x00"){
  return false
 }
 for _,component:=range strings.Split(strings.TrimPrefix(path,"/"),"/"){
  if component==""||component=="."||component==".."{return false}
  for _,r:=range component{
   if !((r>='a'&&r<='z')||(r>='A'&&r<='Z')||
    (r>='0'&&r<='9')||r=='-'||r=='_'){return false}
  }
 }
 return true
}

func validRequestPath(path,escaped string)bool{
 if !strings.HasPrefix(path,"/")||strings.Contains(path,"//")||
  strings.Contains(path,"\\")||strings.ContainsRune(path,0){
  return false
 }
 lowered:=strings.ToLower(escaped)
 if strings.Contains(lowered,"%2f")||strings.Contains(lowered,"%5c"){
  return false
 }
 for _,segment:=range strings.Split(path,"/"){
  if segment=="."||segment==".."{return false}
 }
 return true
}

func isPrivilegedToolPath(path string)bool{
 return path=="/v1/tools"||strings.HasPrefix(path,"/v1/tools/")
}

func writable(allowed map[string]bool,path string)bool{
 for prefix:=range allowed{
  if path==prefix||strings.HasPrefix(path,prefix+"/"){return true}
 }
 return false
}

// systemd places service credentials in a private directory. The files can
// be group-readable to a service with a transient identity; they must never
// be world-accessible or group-writable.
func privateCredentialMode(path string, mode os.FileMode) bool {
 if mode.Perm()&0077==0{return true}
 directory:=os.Getenv("CREDENTIALS_DIRECTORY")
 return directory!=""&&filepath.Dir(filepath.Clean(path))==filepath.Clean(directory)&&mode.Perm()&0037==0
}

func protectedToken(path string)(string,error){
 if !filepath.IsAbs(path){return "",errors.New("credential path must be absolute")}
 // Validate the open descriptor, not a path inspected before opening it.
 file,err:=os.OpenFile(path,os.O_RDONLY|syscall.O_NOFOLLOW,0)
 if err!=nil{return "",errors.New("interface credential file is unavailable or has unsafe access")}
 defer file.Close()
 info,err:=file.Stat()
 if err!=nil||!info.Mode().IsRegular()||!privateCredentialMode(path,info.Mode())||
  info.Size()<32||info.Size()>4097{
  return "",errors.New("interface credential file is unavailable or has unsafe access")
 }
 data,err:=io.ReadAll(io.LimitReader(file,4098))
 if err!=nil||len(data)>4097{return "",errors.New("interface credential unreadable")}
 token:=strings.TrimSuffix(string(data),"\n")
 if !validToken(token){return "",errors.New("invalid interface credential")}
 return token,nil
}

// Serve starts one optional interface listener. No domain store is opened and
// no background job, database writer, scheduler or worker is started here.
func Serve(surface Surface,getenv func(string)string)error{
 prefix:="HOMELAB_"+strings.ToUpper(string(surface))+"_"
 listen:=getenv(prefix+"LISTEN_ADDR")
 host,_,err:=net.SplitHostPort(listen)
 if err!=nil||!loopbackHost(host){
  return errors.New("interface listener must use an explicit loopback IP")
 }
 clientToken,err:=protectedToken(getenv(prefix+"CLIENT_TOKEN_FILE"))
 if err!=nil{return err}
 serviceToken,err:=protectedToken(getenv(prefix+"SERVICE_TOKEN_FILE"))
 if err!=nil{return err}
 var origins,writePaths []string
 if raw:=getenv(prefix+"ALLOWED_ORIGINS");raw!=""{
  origins=strings.Split(raw,",")
 }
 if surface==API {
  if raw:=getenv(prefix+"WRITE_PATHS");raw!=""{
   writePaths=strings.Split(raw,",")
  }
 }
 handler,err:=New(Config{Surface:surface,BackendURL:getenv(prefix+"BACKEND_URL"),
  ClientToken:clientToken,ServiceToken:serviceToken,Origins:origins,WritePaths:writePaths})
 if err!=nil{return err}
 server:=&http.Server{Addr:listen,Handler:handler,ReadHeaderTimeout:10*time.Second,
  ReadTimeout:90*time.Second,WriteTimeout:180*time.Second,IdleTimeout:120*time.Second,
  MaxHeaderBytes:1<<16}
 fmt.Printf("%s interface listening on %s\n",surface,listen)
 return server.ListenAndServe()
}
