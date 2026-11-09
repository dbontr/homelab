// Package privileged owns local administrative operations unavailable to the
// primary MCP gateway. Every operation is checked by the independent owner
// authorization service before a credential-owning handler runs.
package privileged

import (
 "context"
 "crypto/rand"
 "crypto/sha256"
 "encoding/hex"
 "encoding/json"
 "errors"
 "fmt"
 "io"
 "log"
 "net"
 "net/http"
 "os"
 "os/exec"
 "path/filepath"
 "regexp"
 "strings"
 "time"

 "github.com/dbontr/homelab-new/system/canonicaljson"
 "github.com/dbontr/homelab-new/system/security"
)

type Authorizer interface {
 Authorize(context.Context,string,string,json.RawMessage)(security.Authorization,error)
}

type Run func(context.Context,string,...string)error

type Actuator interface {
 Activate(context.Context,string)error
 Revert(context.Context,string)error
}

type Config struct{
 Host string
 Systemctl string
 AllowedServices []string
 AllowRelease bool
 Kubernetes *KubernetesPolicy
 Workspace *WorkspacePolicy
 Auth Authorizer
 Run Run
 Releases Actuator
 Audit *AuditJournal
}

type Executor struct {
 cfg Config
 services map[string]bool
}

func serviceAction(operation string)string{
 switch operation{
 case "infrastructure__service_start":return "start"
 case "infrastructure__service_stop":return "stop"
 case "infrastructure__service_reload":return "reload"
 case "infrastructure__service_restart":return "restart"
 default:return ""
 }
}

var unitPattern=regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9@_.-]{0,127}[.]service$`)
var nodePattern=regexp.MustCompile(`^[a-z][a-z0-9-]{0,62}$`)
var releasePattern=regexp.MustCompile(`^[a-f0-9]{48}$`)

func FixedRun(ctx context.Context,command string,args ...string)error{
 cmd:=exec.CommandContext(ctx,command,args...)
 cmd.Env=[]string{"PATH=/run/current-system/sw/bin:/usr/bin:/bin","LANG=C"}
 output,err:=cmd.CombinedOutput()
 if err!=nil{
  if len(output)>512{output=output[:512]}
  return fmt.Errorf("administrative execution failed: %w (%s)",err,strings.TrimSpace(string(output)))
 }
 return nil
}

func New(cfg Config)(*Executor,error){
 if !nodePattern.MatchString(cfg.Host)||!filepath.IsAbs(cfg.Systemctl)||cfg.Auth==nil||cfg.Run==nil||
 cfg.Releases==nil||len(cfg.AllowedServices)>64{return nil,errors.New("invalid executor configuration")}
 if err:=cfg.Kubernetes.validate();err!=nil{return nil,err}
 if err:=cfg.Workspace.validate();err!=nil{return nil,err}
 services:=make(map[string]bool,len(cfg.AllowedServices))
 for _,service:=range cfg.AllowedServices{
  if !unitPattern.MatchString(service)||services[service]{
   return nil,errors.New("invalid service allowlist")
  }
  for _,sensitive:=range []string{"network","firewall","router","wireguard","vpn","dns","nftables","iptables","nginx","cloudflared","pihole","tailscale","systemd","docker","k3s","kube","jellyfin","moonfin","ssh","security","executor","fleet","prometheus","loki","alertmanager"}{
   if strings.Contains(strings.ToLower(service),sensitive){
    return nil,errors.New("network, security and media services require out-of-band administration")
   }
  }
  services[service]=true
 }
 return &Executor{cfg:cfg,services:services},nil
}

// Execute accepts only typed operations and canonical JSON. The executor
// cannot be used as an arbitrary command runner, even by an unlocked gateway.
func(e *Executor) Execute(ctx context.Context,operation,target string,args json.RawMessage)(security.Authorization,error){
 if target!=e.cfg.Host{return security.Authorization{Reason:"target is not this host"},nil}
 canonical,fields,err:=canonicaljson.Object(args)
 if err!=nil{return security.Authorization{Reason:"invalid operation arguments"},nil}
 verb:=serviceAction(operation)
 switch operation{
 case "infrastructure__service_start","infrastructure__service_stop","infrastructure__service_reload","infrastructure__service_restart":
  if len(fields)!=2{return security.Authorization{Reason:"unexpected arguments"},nil}
  service,ok:=fields["service"].(string)
  if !ok||!e.services[service]{return security.Authorization{Reason:"service not permitted"},nil}
  host,ok:=fields["host"].(string)
  if !ok||host!=target{return security.Authorization{Reason:"host mismatch"},nil}
 case "cluster__deployment_restart","cluster__deployment_scale":
  if _,_,err:=e.cfg.Kubernetes.command(operation,fields);err!=nil{
   return security.Authorization{Reason:"Kubernetes policy denied operation"},nil
  }
  host,ok:=fields["host"].(string)
  if !ok||host!=target{return security.Authorization{Reason:"host mismatch"},nil}
 case "infrastructure__workspace_stop","infrastructure__workspace_build_environment",
 "infrastructure__workspace_register_software","infrastructure__workspace_revoke_worker":
  host,ok:=fields["host"].(string)
  if !ok||host!=target{return security.Authorization{Reason:"host mismatch"},nil}
  if _,_,err:=e.cfg.Workspace.operationArgs(operation,fields);err!=nil{
   return security.Authorization{Reason:"Workspace resource policy denied operation"},nil
  }
 case "infrastructure__release_activate","infrastructure__release_revert":
  if !e.cfg.AllowRelease{return security.Authorization{Reason:"release activation not enabled for this host"},nil}
  if len(fields)!=2{return security.Authorization{Reason:"unexpected arguments"},nil}
  host,ok:=fields["host"].(string)
  if !ok||host!=target{return security.Authorization{Reason:"host mismatch"},nil}
  id,ok:=fields["release_id"].(string)
  if !ok||!releasePattern.MatchString(id){return security.Authorization{Reason:"invalid staged release ID"},nil}
 default:
  return security.Authorization{Reason:"unregistered privileged operation"},nil
 }
 // Authentication/confirmation takes place in the separate security process.
 // No gateway-supplied claim of approval is accepted.
 decision,err:=e.cfg.Auth.Authorize(ctx,operation,target,canonical)
 if err!=nil{
  log.Print("security audit privileged_operation denied")
  return security.Authorization{Reason:"independent authorization unavailable"},err
 }
 if !decision.Allowed{
  log.Print("security audit privileged_operation denied")
  return decision,nil
 }
 // An audit append is required before execution when the owner has enabled
 // durable audit custody. Unavailable audit storage must not permit a write.
 var auditID string
 if e.cfg.Audit!=nil {
  var entropy [16]byte
  if _,err:=rand.Read(entropy[:]);err!=nil{return security.Authorization{Reason:"audit identity unavailable"},err}
  auditID=hex.EncodeToString(entropy[:])
  digest:=sha256.Sum256(canonical)
  if err:=e.cfg.Audit.Record(auditID,operation,target,hex.EncodeToString(digest[:]),"started");err!=nil{
   return security.Authorization{Reason:"audit recording unavailable"},err
  }
 }
 switch operation{
 case "infrastructure__service_start","infrastructure__service_stop","infrastructure__service_reload","infrastructure__service_restart":
  err=e.cfg.Run(ctx,e.cfg.Systemctl,verb,fields["service"].(string))
 case "infrastructure__workspace_stop","infrastructure__workspace_build_environment",
 "infrastructure__workspace_register_software","infrastructure__workspace_revoke_worker":
  err=e.cfg.Workspace.Call(ctx,operation,fields)
 case "infrastructure__release_activate":
  err=e.cfg.Releases.Activate(ctx,fields["release_id"].(string))

 case "infrastructure__release_revert":
  err=e.cfg.Releases.Revert(ctx,fields["release_id"].(string))
 case "cluster__deployment_restart","cluster__deployment_scale":
  var command string
  var argv []string
  command,argv,err=e.cfg.Kubernetes.command(operation,fields)
  if err==nil{err=e.cfg.Run(ctx,command,argv...)}

 }
 if e.cfg.Audit!=nil {
  outcome:="succeeded"
  if err!=nil{outcome="failed"}
  digest:=sha256.Sum256(canonical)
  if auditErr:=e.cfg.Audit.Record(auditID,operation,target,hex.EncodeToString(digest[:]),outcome);auditErr!=nil {
   return security.Authorization{Reason:"privileged outcome is uncertain: audit unavailable"},errors.Join(err,auditErr)
  }
 }
 if err!=nil{
  log.Print("security audit privileged_operation failed")
  return security.Authorization{Reason:"execution failed"},err
 }
 log.Print("security audit privileged_operation succeeded")
 return security.Authorization{Allowed:true},nil
}

func(e *Executor) Handler()http.Handler{
 mux:=http.NewServeMux()
 mux.HandleFunc("POST /v1/execute",func(w http.ResponseWriter,r *http.Request){
  w.Header().Set("Cache-Control","no-store")
  w.Header().Set("X-Content-Type-Options","nosniff")
  r.Body=http.MaxBytesReader(w,r.Body,64<<10)
  decoder:=json.NewDecoder(r.Body)
  decoder.DisallowUnknownFields()
  var in struct{
   Operation string `json:"operation"`
   Target string `json:"target"`
   Arguments json.RawMessage `json:"arguments"`
  }
  if err:=decoder.Decode(&in);err!=nil{http.Error(w,"invalid request",400);return}
  var trailing any
  if err:=decoder.Decode(&trailing);err!=io.EOF{http.Error(w,"invalid request",400);return}
  if r.Header.Get("Cf-Access-Jwt-Assertion")==""{http.Error(w,"owner assertion required",403);return}
  ctx:=security.WithAccessAssertion(r.Context(),r.Header.Get("Cf-Access-Jwt-Assertion"))
  result,err:=e.Execute(ctx,in.Operation,in.Target,in.Arguments)
  if err!=nil{http.Error(w,"privileged operation failed",503);return}
  w.Header().Set("Content-Type","application/json")
  _=json.NewEncoder(w).Encode(result)
 })
 return mux
}

func(e *Executor) ServeSocket(ctx context.Context,path string,groupID int)error{
 if !filepath.IsAbs(path)||groupID<=0||strings.ContainsRune(path,0){return errors.New("invalid executor socket")}
 dir:=filepath.Dir(path)
 info,err:=os.Lstat(dir)
 if err!=nil||!info.IsDir()||info.Mode().Perm()&0007!=0{return errors.New("executor runtime directory is not private")}
 if err:=os.Chown(dir,-1,groupID);err!=nil{return err}
 if err:=os.Chmod(dir,0750);err!=nil{return err}
 if _,err:=os.Lstat(path);err==nil{return errors.New("executor socket already exists")}else if !errors.Is(err,os.ErrNotExist){return err}
 listener,err:=net.Listen("unix",path);if err!=nil{return err}
 defer listener.Close();defer os.Remove(path)
 if err:=os.Chown(path,-1,groupID);err!=nil{return err}
 if err:=os.Chmod(path,0660);err!=nil{return err}
 server:=&http.Server{Handler:e.Handler(),ReadHeaderTimeout:5*time.Second,ReadTimeout:15*time.Second,WriteTimeout:180*time.Second}
 stopped:=make(chan struct{})
 go func(){select{case <-ctx.Done():_=server.Close();case <-stopped:}}()
 err=server.Serve(listener);close(stopped)
 if errors.Is(err,http.ErrServerClosed){return nil}
 return err
}
