// Package release provides local, auditable NixOS generation staging and rollback.
// It never accepts shell commands or unrestricted paths from an MCP request.
package release

import (
 "context"
 "crypto/rand"
 "encoding/hex"
 "encoding/json"
 "errors"
 "fmt"
 "io"
 "net"
 "net/http"
 "net/url"
 "os"
 "os/exec"
 "path/filepath"
 "regexp"
 "strconv"
 "strings"
 "syscall"
 "time"
)

var revisionPattern=regexp.MustCompile(`^[0-9a-f]{40}$`)
var idPattern=regexp.MustCompile(`^[0-9a-f]{48}$`)

type Run func(context.Context,string,...string)(string,error)

type Config struct {
 Host string
 FlakeDir string
 StateDir string
 Nix string
 NixEnv string
 Git string
 Systemctl string
 HealthServices []string
 HealthSockets []string
 TrustedSource bool
 Run Run
 Current func()(string,error)
 // The protected guardian invokes these only after a durable, owner-approved
 // release attempt. Callbacks cannot change the immutable generation path.
 BeforeSwitch func(context.Context,string,string) error
 BeforeRollback func(context.Context,string,string) error
}

type Manifest struct {
 ID string `json:"id"`
 Host string `json:"host"`
 Revision string `json:"revision"`
 System string `json:"system"`
 Created time.Time `json:"created"`
}

type Journal struct {
 Phase string `json:"phase"`
 ID string `json:"id"`
 Previous string `json:"previous"`
 Target string `json:"target"`
 Time time.Time `json:"time"`
}

type Controller struct{Config Config}

func Exec(ctx context.Context,name string,args ...string)(string,error){
 // Nix needs a writable XDG cache even when HOME is deliberately read-only.
 // Give each command a private cache and remove it when the command finishes.
 cacheDir,err:=os.MkdirTemp("","homelab-release-cache-")
 if err!=nil{return "",fmt.Errorf("create private release cache: %w",err)}
 defer os.RemoveAll(cacheDir)
 cmd:=exec.CommandContext(ctx,name,args...)
 cmd.Env=[]string{"PATH=/run/current-system/sw/bin:/usr/bin:/bin","LANG=C","HOME=/var/empty","XDG_CACHE_HOME="+cacheDir}
 data,err:=cmd.CombinedOutput()
 if len(data)>8192{data=data[:8192]}
 if err!=nil{return "",fmt.Errorf("release command %s failed: %w (%s)",filepath.Base(name),err,strings.TrimSpace(string(data)))}
 return string(data),nil
}

func New(cfg Config)(*Controller,error){
 if !regexp.MustCompile(`^[a-z][a-z0-9-]*$`).MatchString(cfg.Host) ||
 !filepath.IsAbs(cfg.FlakeDir)||!filepath.IsAbs(cfg.StateDir) || cfg.StateDir=="/" ||
 !filepath.IsAbs(cfg.Nix)||!filepath.IsAbs(cfg.NixEnv)||!filepath.IsAbs(cfg.Git)||!filepath.IsAbs(cfg.Systemctl) ||
 cfg.Run==nil||cfg.Current==nil||len(cfg.HealthServices)==0||len(cfg.HealthServices)>32 {
  return nil,errors.New("invalid local release controller configuration")
 }
 if len(cfg.HealthSockets)>16{return nil,errors.New("too many release health sockets")}
 seenSockets:=make(map[string]bool,len(cfg.HealthSockets))
 for _,socket:=range cfg.HealthSockets{
  host,port,err:=net.SplitHostPort(socket)
  ip:=net.ParseIP(host)
  value,portErr:=strconv.Atoi(port)
  if err!=nil||portErr!=nil||value<1||value>65535||ip==nil||!(ip.IsPrivate()||ip.IsLoopback())||seenSockets[socket]{
   return nil,errors.New("invalid private release health socket")
  }
  seenSockets[socket]=true
 }
 // Health probes are read-only. Critical service names remain forbidden
 // to the privileged service-mutation policy, but must be checked on release.
 healthUnit := regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9@_.-]{0,119}[.]service$`)
 for _,svc:=range cfg.HealthServices{
  if len(svc)>128||!healthUnit.MatchString(svc){
   return nil,errors.New("invalid health service")
  }
 }
 return &Controller{Config:cfg},nil
}

// TrustedSource and the private release state must not be writable by
// the gateway account or another unprivileged local user.
func requireRootOwnedParents(path string) error {
 for path=filepath.Clean(path);;path=filepath.Dir(path) {
  info,err:=os.Lstat(path)
  if err!=nil{return err}
  stat,ok:=info.Sys().(*syscall.Stat_t)
  if !ok||stat.Uid!=0||info.Mode()&os.ModeSymlink!=0||info.Mode().Perm()&0022!=0{
   return errors.New("release source has an untrusted owner, symlink or writable parent")
  }
  if path=="/"{return nil}
 }
}


func checkStorePath(path string)bool{
 return filepath.IsAbs(path)&&filepath.Clean(path)==path&&strings.HasPrefix(path,"/nix/store/")&&
  !strings.ContainsAny(path,"\x00\r\n\t ")&&len(path)<512
}

func(c *Controller) locked(fn func()error)error{
 if err:=os.MkdirAll(c.Config.StateDir,0700);err!=nil{return err}
 info,err:=os.Lstat(c.Config.StateDir)
 if err!=nil||!info.IsDir()||info.Mode().Perm()&0077!=0{return errors.New("release state directory is not owner-only")}
 if c.Config.TrustedSource{
  if err:=requireRootOwnedParents(c.Config.StateDir);err!=nil{return err}
 }
 f,err:=os.OpenFile(filepath.Join(c.Config.StateDir,".lock"),os.O_RDWR|os.O_CREATE,0600)
 if err!=nil{return err}
 defer f.Close()
 if err:=syscall.Flock(int(f.Fd()),syscall.LOCK_EX);err!=nil{return err}
 defer syscall.Flock(int(f.Fd()),syscall.LOCK_UN)
 return fn()
}

func(c *Controller) atomic(name string, v any)error{
 data,err:=json.Marshal(v);if err!=nil{return err}
 if len(data)>8192{return errors.New("release record too large")}
 f,err:=os.CreateTemp(c.Config.StateDir,".record-*");if err!=nil{return err}
 defer os.Remove(f.Name())
 if err:=f.Chmod(0600);err!=nil{f.Close();return err}
 if _,err:=f.Write(append(data,'\n'));err!=nil{f.Close();return err}
 if err:=f.Sync();err!=nil{f.Close();return err}
 if err:=f.Close();err!=nil{return err}
 if err:=os.Rename(f.Name(),filepath.Join(c.Config.StateDir,name));err!=nil{return err}
 dir,err:=os.Open(c.Config.StateDir);if err!=nil{return err}
 defer dir.Close()
 return dir.Sync()
}

func(c *Controller) read(name string,v any)error{
 f,err:=os.Open(filepath.Join(c.Config.StateDir,name));if err!=nil{return err}
 defer f.Close()
 info,err:=f.Stat()
 if err!=nil||!info.Mode().IsRegular()||info.Mode().Perm()&0077!=0{return errors.New("unsafe release record")}
 raw,err:=io.ReadAll(io.LimitReader(f,8193));if err!=nil{return err}
 if len(raw)>8192{return errors.New("release record too large")}
 return json.Unmarshal(raw,v)
}

func newID()(string,error){
 b:=make([]byte,24)
 if _,err:=rand.Read(b);err!=nil{return "",err}
 return hex.EncodeToString(b),nil
}

// Stage accepts only a clean, exact Git commit and an offline Nix build.
func(c *Controller) Stage(ctx context.Context,revision string)(Manifest,error){
 var staged Manifest
 if !revisionPattern.MatchString(revision){return staged,errors.New("exact Git commit required")}
 if c.Config.TrustedSource{
  if err:=requireRootOwnedParents(c.Config.FlakeDir);err!=nil{return staged,err}
 }
 err:=c.locked(func()error{
  var active Journal
  if err:=c.read("journal.json",&active);err==nil&&(active.Phase=="activating"||active.Phase=="reverting"||active.Phase=="rollback-failed"){
   return errors.New("interrupted deployment requires recovery")
  }else if err!=nil&&!errors.Is(err,os.ErrNotExist){return err}
  head,err:=c.Config.Run(ctx,c.Config.Git,"-C",c.Config.FlakeDir,"rev-parse","HEAD")
  if err!=nil||strings.TrimSpace(head)!=revision{return errors.New("source revision does not match clean checkout")}
  dirty,err:=c.Config.Run(ctx,c.Config.Git,"-C",c.Config.FlakeDir,"status","--porcelain","--untracked-files=all")
  if err!=nil||strings.TrimSpace(dirty)!=""{return errors.New("release requires a clean Git worktree")}
  // Pin the exact Git object in the Nix flake reference. A moving checkout
  // cannot change which source revision is built after the cleanliness check.
  source:=(&url.URL{Scheme:"git+file",Path:c.Config.FlakeDir,RawQuery:"rev="+revision,
   Fragment:"nixosConfigurations."+c.Config.Host+".config.system.build.toplevel"}).String()
  system,err:=c.Config.Run(ctx,c.Config.Nix,"build","--offline","--no-link","--print-out-paths",source)
  if err!=nil{return err}
  system=strings.TrimSpace(system)
  if !checkStorePath(system){return errors.New("Nix build did not return one valid store path")}
  id,err:=newID();if err!=nil{return err}
  staged=Manifest{ID:id,Host:c.Config.Host,Revision:revision,System:system,Created:time.Now().UTC()}
  if err:=c.atomic("stage-"+id+".json",staged);err!=nil{return err}
  // A distinct, durable receipt prevents old implementation stages from
  // crossing the security-authority boundary without requalification.
  return c.atomic("stage-ready-"+id+".json",staged)
 })
 return staged,err
}

func(c *Controller) loadStage(id string)(Manifest,error){
 var m Manifest
 if !idPattern.MatchString(id){return m,errors.New("invalid release ID")}
 if err:=c.read("stage-"+id+".json",&m);err!=nil{return m,err}
 if m.ID!=id||m.Host!=c.Config.Host||!revisionPattern.MatchString(m.Revision)||!checkStorePath(m.System){
  return m,errors.New("invalid staged generation")
 }
 return m,nil
}

// VerifyStaged checks an existing protected manifest without activating it.
// Activation revalidates the staged generation while holding its own lock.
func(c *Controller)VerifyStaged(id string)error{
 if !idPattern.MatchString(id){return errors.New("invalid release ID")}
 return c.locked(func()error{
  stage,err:=c.loadStage(id)
  if err!=nil{return err}
  var ready Manifest
  if err:=c.read("stage-ready-"+id+".json",&ready);err!=nil{
   return fmt.Errorf("stage authority receipt unavailable: %w",err)
  }
  if ready.ID!=stage.ID||ready.Host!=stage.Host||ready.Revision!=stage.Revision||
   ready.System!=stage.System||!ready.Created.Equal(stage.Created){
   return errors.New("stage authority receipt differs from immutable manifest")
  }
  return nil
 })
}

// waitForHealth permits bounded service startup after NixOS switches the
// generation. Every required service and private socket must become ready;
// expiry fails the release closed and retains normal rollback behavior.
func(c *Controller) waitForHealth(ctx context.Context)error{
 readyCtx,cancel:=context.WithTimeout(ctx,45*time.Second)
 defer cancel()
 ticker:=time.NewTicker(time.Second)
 defer ticker.Stop()
 for {
  err:=c.health(readyCtx)
  if err==nil{return nil}
  select {
  case <-readyCtx.Done():
   return fmt.Errorf("required release services did not become ready: %w",err)
  case <-ticker.C:
  }
 }
}

func(c *Controller) health(ctx context.Context)error{
 for _,svc:=range c.Config.HealthServices{
  out,err:=c.Config.Run(ctx,c.Config.Systemctl,"is-active",svc)
  if err!=nil||strings.TrimSpace(out)!="active"{return fmt.Errorf("service %s is not active",svc)}
 }
 for _,socket:=range c.Config.HealthSockets{
  dialer:=net.Dialer{Timeout:3*time.Second}
  conn,err:=dialer.DialContext(ctx,"tcp",socket)
  if err!=nil{return errors.New("release protected TCP health check failed")}
  if err:=conn.Close();err!=nil{return errors.New("release TCP health connection did not close")}
 }
 return nil
}

func(c *Controller) set(ctx context.Context,system string)error{
 if !checkStorePath(system){return errors.New("unsafe generation path")}
 if _,err:=c.Config.Run(ctx,c.Config.NixEnv,"-p","/nix/var/nix/profiles/system","--set",system);err!=nil{return err}
 _,err:=c.Config.Run(ctx,filepath.Join(system,"bin/switch-to-configuration"),"switch")
 return err
}

func(c *Controller) rollback(ctx context.Context,j Journal)error{
 if c.Config.BeforeRollback!=nil {
  if err:=c.Config.BeforeRollback(ctx,j.Target,j.Previous);err!=nil {
   j.Phase="rollback-failed"
   _=c.atomic("journal.json",j)
   return fmt.Errorf("security recovery preparation failed: %w",err)
  }
 }
 if err:=c.set(ctx,j.Previous);err!=nil{
  j.Phase="rollback-failed"
  _=c.atomic("journal.json",j)
  return err
 }
 if err:=c.health(ctx);err!=nil{
  j.Phase="rollback-failed"
  _=c.atomic("journal.json",j)
  return err
 }
 j.Phase="rolled-back"
 return c.atomic("journal.json",j)
}

// Activate performs a local NixOS switch and restores the old generation if
// either activation or the declared service health probes fail.
func(c *Controller) Activate(ctx context.Context,id string)error{
 return c.locked(func()error{
  m,err:=c.loadStage(id);if err!=nil{return err}
  var ready Manifest
  if err:=c.read("stage-ready-"+id+".json",&ready);err!=nil{
   return fmt.Errorf("stage lacks Go authority receipt: %w",err)
  }
  if ready.ID!=m.ID||ready.Host!=m.Host||ready.Revision!=m.Revision||
     ready.System!=m.System||!ready.Created.Equal(m.Created){
   return errors.New("stage authority receipt does not match immutable manifest")
  }
  var attempted Journal
  if err:=c.read("attempt-"+id+".json",&attempted);err==nil{
   return errors.New("staged release already attempted")
  }else if !errors.Is(err,os.ErrNotExist){
   return fmt.Errorf("release attempt record cannot be verified: %w",err)
  }
  var previous Journal
  if err:=c.read("journal.json",&previous);err==nil&&(previous.ID==id||previous.Phase=="activating"||previous.Phase=="reverting"||previous.Phase=="rollback-failed"){
   return errors.New("previous activation must be recovered first")
  }else if err!=nil&&!errors.Is(err,os.ErrNotExist){return err}
  current,err:=c.Config.Current()
  if err!=nil||!checkStorePath(current){return errors.New("current NixOS generation cannot be verified")}
  if current==m.System{return errors.New("generation already active")}
  txn:=Journal{Phase:"activating",ID:id,Previous:current,Target:m.System,Time:time.Now().UTC()}
  // Persist the attempt before an operation that could switch the host.
  // A later journal replacement or restart cannot restore this capability.
  if err:=c.atomic("attempt-"+id+".json",txn);err!=nil{return err}
  if err:=c.atomic("journal.json",txn);err!=nil{return err}
  // Do not use the caller's cancellation for rollback. A cancelled user
  // request must not leave an unverified generation active.
  var switchErr error
  if c.Config.BeforeSwitch!=nil {
   switchErr=c.Config.BeforeSwitch(ctx,current,m.System)
   if switchErr!=nil{return c.preparationFailed(ctx,txn,switchErr)}
  }
  if switchErr==nil{switchErr=c.set(ctx,m.System)}
  if switchErr==nil{switchErr=c.waitForHealth(ctx)}
  if switchErr!=nil{
   cleanup,cancel:=context.WithTimeout(context.Background(),90*time.Second);defer cancel()
   rollbackErr:=c.rollback(cleanup,txn)
   if rollbackErr!=nil{return fmt.Errorf("activation failed: %v; rollback failed: %w",switchErr,rollbackErr)}
   return fmt.Errorf("activation failed and previous generation restored: %w",switchErr)
  }
  txn.Phase="active"
  return c.atomic("journal.json",txn)
 })
}

// A failed pre-switch preparation may have stopped and restored the same
// authority, but no NixOS generation has been selected. Avoid a second,
// unnecessary migration to the previous implementation once both the running
// generation and its protected health probes have been verified.
func(c *Controller) preparationFailed(ctx context.Context,txn Journal,failure error)error{
 current,err:=c.Config.Current()
 sourceSelected:=err==nil&&current==txn.Previous
 if sourceSelected&&c.Config.TrustedSource{
  running,readErr:=filepath.EvalSymlinks("/run/current-system")
  sourceSelected=readErr==nil&&running==txn.Previous
 }
 cleanup,cancel:=context.WithTimeout(context.Background(),90*time.Second)
 defer cancel()
 if sourceSelected&&c.health(cleanup)==nil{
  txn.Phase="rolled-back"
  if err:=c.atomic("journal.json",txn);err!=nil{
   return fmt.Errorf("security preparation failed: %v; cannot persist recovery: %w",failure,err)
  }
  return fmt.Errorf("security preparation failed; previous generation remains healthy: %w",failure)
 }
 if err:=c.rollback(cleanup,txn);err!=nil{
  return fmt.Errorf("security preparation failed: %v; rollback failed: %w",failure,err)
 }
 return fmt.Errorf("security preparation failed and previous generation restored: %w",failure)
}

// Revert restores a completed release only when the active system is the
// generation recorded in that release. The release ID is never accepted as a
// free-form target path or an instruction to execute arbitrary code.
func(c *Controller) Revert(ctx context.Context,id string)error{
 if !idPattern.MatchString(id){return errors.New("invalid release ID")}
 return c.locked(func()error{
  var txn Journal
  if err:=c.read("journal.json",&txn);err!=nil{return err}
  if txn.Phase!="active"||txn.ID!=id||!checkStorePath(txn.Target)||!checkStorePath(txn.Previous){
   return errors.New("release is not the active recorded generation")
  }
  current,err:=c.Config.Current()
  if err!=nil||current!=txn.Target{return errors.New("active generation differs from release journal")}
  txn.Phase="reverting"
  if err:=c.atomic("journal.json",txn);err!=nil{return err}
  cleanup,cancel:=context.WithTimeout(context.Background(),90*time.Second)
  defer cancel()
  if err:=c.rollback(cleanup,txn);err!=nil{return err}
  return nil
 })
}

// Recover resolves an interrupted switch, using the last proven generation.
func(c *Controller) Recover(ctx context.Context)error{
 return c.locked(func()error{
  var txn Journal
  if err:=c.read("journal.json",&txn);err!=nil{return err}
  if txn.Phase!="activating"&&txn.Phase!="reverting"&&txn.Phase!="rollback-failed"{return errors.New("no interrupted release")}
  if !checkStorePath(txn.Previous){return errors.New("invalid recovery generation")}
  current,err:=c.Config.Current()
  if err!=nil||!checkStorePath(current){return errors.New("current generation cannot be verified")}
  if current==txn.Previous{
   // An interrupted rollback can already have restored the boot profile.
   // Do not restart privileged services while the release lease is held.
   // Trusted hosts must also prove the actual running generation.
   if c.Config.TrustedSource{
    running,err:=filepath.EvalSymlinks("/run/current-system")
    if err!=nil||running!=txn.Previous{return errors.New("running generation differs from recovery target")}
   }
   if err:=c.health(ctx);err!=nil{return err}
   txn.Phase="rolled-back"
   return c.atomic("journal.json",txn)
  }
  return c.rollback(ctx,txn)
 })
}

func(c *Controller) Status() (Journal,error) {
 var status Journal
 err:=c.locked(func()error{return c.read("journal.json",&status)})
 return status,err
}

// LocalHealth is useful only for protected, loopback-only services.
func LocalHealth(ctx context.Context,url string)error{
 if !strings.HasPrefix(url,"http://127.0.0.1:")&&!strings.HasPrefix(url,"http://[::1]:"){
  return errors.New("health check must use loopback")
 }
 request,err:=http.NewRequestWithContext(ctx,http.MethodGet,url,nil)
 if err!=nil{return err}
 client:=&http.Client{Timeout:5*time.Second,CheckRedirect:func(*http.Request,[]*http.Request)error{return errors.New("health redirect denied")}}
 response,err:=client.Do(request);if err!=nil{return err}
 defer response.Body.Close()
 if response.StatusCode!=http.StatusOK{return fmt.Errorf("health endpoint returned %d",response.StatusCode)}
 return nil
}
