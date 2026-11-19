package main

import (
 "context"
 "crypto/rand"
 "crypto/sha256"
 "encoding/hex"
 "encoding/json"
 "errors"
 "fmt"
 "io"
 "os"
 "os/exec"
 "os/user"
 "path/filepath"
 "strconv"
 "strings"
 "syscall"
 "time"
)

// The detached release guardian, rather than the owner browser or the
// retiring authority, holds custody of the exact-state handoff.
type securityTransition struct {
 StateDir string
 SnapshotParent string
 SecretDir string
 OwnerEmailFile string
 PolicyFile string
 Issuer string
 GatewayAudience string
 BrowserAudience string
 BrowserOrigin string
 Systemctl string
 Runuser string
 Env string
}

func securityTransitionFromEnv() (*securityTransition,error) {
 switch os.Getenv("HOMELAB_RELEASE_SECURITY_TRANSITION") {
 case "":return nil,nil
 case "1":
 default:return nil,errors.New("invalid security transition mode")
 }
 t:=&securityTransition{
  StateDir:os.Getenv("HOMELAB_RELEASE_SECURITY_STATE"),
  SnapshotParent:os.Getenv("HOMELAB_RELEASE_SECURITY_SNAPSHOT_PARENT"),
  SecretDir:os.Getenv("HOMELAB_RELEASE_SECURITY_SECRETS"),
  OwnerEmailFile:os.Getenv("HOMELAB_RELEASE_SECURITY_ACCESS_OWNER"),
  PolicyFile:os.Getenv("HOMELAB_RELEASE_SECURITY_POLICY"),
  Issuer:os.Getenv("HOMELAB_RELEASE_SECURITY_ISSUER"),
  GatewayAudience:os.Getenv("HOMELAB_RELEASE_SECURITY_GATEWAY_AUD"),
  BrowserAudience:os.Getenv("HOMELAB_RELEASE_SECURITY_BROWSER_AUD"),
  BrowserOrigin:os.Getenv("HOMELAB_RELEASE_SECURITY_BROWSER_ORIGIN"),
  Systemctl:os.Getenv("HOMELAB_RELEASE_SYSTEMCTL"),
  Runuser:os.Getenv("HOMELAB_RELEASE_RUNUSER"),
  Env:os.Getenv("HOMELAB_RELEASE_ENV"),
 }
 for _,path:=range []string{t.StateDir,t.SnapshotParent,t.SecretDir,t.OwnerEmailFile,t.PolicyFile,t.Systemctl,t.Runuser,t.Env}{
  if !filepath.IsAbs(path)||filepath.Clean(path)!=path||strings.Contains(path,"/../") {
   return nil,errors.New("invalid protected transition configuration")
  }
 }
 if t.Issuer==""||t.GatewayAudience==""||t.BrowserAudience==""||t.BrowserOrigin=="" {
  return nil,errors.New("incomplete security authority configuration")
 }
 return t,nil
}

// Read only immutable systemd unit definitions from staged NixOS systems.
// A migration target is never accepted from an MCP argument.
func securitySystemUnit(system string)(string,string,error){
 if !strings.HasPrefix(system,"/nix/store/")||filepath.Clean(system)!=system {
  return "","",errors.New("invalid NixOS generation")
 }
 unit:=filepath.Join(system,"etc/systemd/system/homelab-securityd.service")
 raw,err:=os.ReadFile(unit)
 if err!=nil||len(raw)>65536{return "","",errors.New("security unit unavailable")}
 return parseSecurityUnit(raw)
}

func parseSecurityUnit(raw []byte)(string,string,error){
 impl,receipt:="",""
 for _,line:=range strings.Split(string(raw),"\n"){
  if strings.HasPrefix(line,"ExecStart=") {
   command:=strings.Fields(strings.TrimPrefix(line,"ExecStart="))
   if len(command)!=1||impl!=""||!strings.HasPrefix(command[0],"/nix/store/") {
    return "","",errors.New("ambiguous or untrusted authority executable")
   }
   switch {
   case strings.HasSuffix(command[0],"/bin/homelab-securityd"):impl="go"
   case strings.HasSuffix(command[0],"/bin/securityd"):impl="rust"
   default:return "","",errors.New("unrecognized security authority")
   }
  }
  if strings.HasPrefix(line,"LoadCredential="){
   for _,binding:=range strings.Fields(strings.TrimPrefix(line,"LoadCredential=")){
    if strings.HasPrefix(binding,"migration-receipt:"){
     if receipt!="" {return "","",errors.New("duplicate authority migration credential")}
     receipt=strings.TrimPrefix(binding,"migration-receipt:")
    }
   }
  }
 }
 if impl==""||receipt==""||!filepath.IsAbs(receipt)||filepath.Clean(receipt)!=receipt{
  return "","",errors.New("security unit lacks protected transition receipt")
 }
 return impl,receipt,nil
}

func (t *securityTransition) beforeSwitch(ctx context.Context,oldSystem,newSystem string)error{
 return t.prepare(ctx,oldSystem,newSystem)
}
func (t *securityTransition) beforeRollback(ctx context.Context,fromSystem,toSystem string)error{
 return t.prepare(ctx,fromSystem,toSystem)
}

func (t *securityTransition) prepare(ctx context.Context,fromSystem,toSystem string)error{
 sourceImpl,_,err:=securitySystemUnit(fromSystem)
 if err!=nil{return err}
 targetImpl,receiptPath,err:=securitySystemUnit(toSystem)
 if err!=nil{return err}
 if sourceImpl==targetImpl{return nil}
 if sourceImpl!="go"&&sourceImpl!="rust" {return errors.New("invalid previous authority")}
 if targetImpl!="go"&&targetImpl!="rust" {return errors.New("invalid target authority")}
 expectedReceipt:=filepath.Join(t.SecretDir,"authority-"+targetImpl+"-cutover.json")
 if receiptPath!=expectedReceipt {
  return errors.New("security unit receipt differs from reviewed custody policy")
 }
 // The durable release guardian owns the single-writer handoff. A failed
 // capture must not leave the original authority stopped when its NixOS
 // generation still owns the system profile.
 return transitionCaptureWithRecovery(
  func()error{return t.stopCurrentAuthority(ctx)},
  func()error{return t.capture(ctx,fromSystem,toSystem,targetImpl,receiptPath)},
  func()error{return t.restartSourceAuthority(context.Background(),fromSystem)},
 )
}

// A transition may fail after quiescing the old writer. Retain the original
// error and independently verify that its still-selected generation is usable.
func transitionCaptureWithRecovery(stop,capture,restore func()error)error{
 if err:=stop();err!=nil{
  if recoveryErr:=restore();recoveryErr!=nil{
   return errors.Join(err,fmt.Errorf("previous security authority recovery failed: %w",recoveryErr))
  }
  return err
 }
 if err:=capture();err!=nil{
  if recoveryErr:=restore();recoveryErr!=nil{
   return errors.Join(err,fmt.Errorf("previous security authority recovery failed: %w",recoveryErr))
  }
  return err
 }
 return nil
}

func (t *securityTransition) restartSourceAuthority(ctx context.Context,fromSystem string)error{
 selected,err:=filepath.EvalSymlinks("/run/current-system")
 if err!=nil||selected!=fromSystem{
  return errors.New("previous authority generation is no longer selected; release rollback is required")
 }
 restartCtx,cancel:=context.WithTimeout(ctx,35*time.Second)
 defer cancel()
 command:=exec.CommandContext(restartCtx,t.Systemctl,"start","homelab-securityd.service")
 if output,err:=command.CombinedOutput();err!=nil{
  return fmt.Errorf("previous security authority restart failed: %w (%s)",err,limitTransitionLog(output))
 }
 status:=exec.CommandContext(restartCtx,t.Systemctl,"is-active","homelab-securityd.service")
 output,err:=status.Output()
 if err!=nil||strings.TrimSpace(string(output))!="active"{
  return errors.New("previous security authority did not become active")
 }
 return nil
}

func (t *securityTransition) stopCurrentAuthority(ctx context.Context)error{
 stopCtx,cancel:=context.WithTimeout(ctx,35*time.Second)
 defer cancel()
 cmd:=exec.CommandContext(stopCtx,t.Systemctl,"stop","homelab-securityd.service")
 if output,err:=cmd.CombinedOutput();err!=nil {
  return fmt.Errorf("security authority did not surrender exclusive custody: %w (%s)",err,limitTransitionLog(output))
 }
 cmd=exec.CommandContext(stopCtx,t.Systemctl,"is-active","homelab-securityd.service")
 if err:=cmd.Run();err==nil{return errors.New("security authority is still active")}
 return nil
}

func transitionFile(path string,limit int)([]byte,error){
 f,err:=os.OpenFile(path,os.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_CLOEXEC,0)
 if err!=nil{return nil,errors.New("protected credential unavailable")}
 defer f.Close()
 info,err:=f.Stat()
 if err!=nil{return nil,err}
 stat,ok:=info.Sys().(*syscall.Stat_t)
 if !ok||!info.Mode().IsRegular()||info.Mode().Perm()&0077!=0||
  stat.Uid!=0||stat.Nlink!=1 {return nil,errors.New("invalid protected credential custody")}
 data,err:=io.ReadAll(io.LimitReader(f,int64(limit+1)))
 if err!=nil||len(data)==0||len(data)>limit{return nil,errors.New("protected credential size rejected")}
 return data,nil
}

func limitTransitionLog(data []byte)string{
 if len(data)>2048{data=data[:2048]}
 return strings.TrimSpace(string(data))
}

func securityIdentity()(int,int,error){
 acct,err:=user.Lookup("homelab-security")
 if err!=nil{return 0,0,err}
 uid,err:=strconv.Atoi(acct.Uid)
 if err!=nil||uid<=0{return 0,0,errors.New("invalid state owner UID")}
 gid,err:=strconv.Atoi(acct.Gid)
 if err!=nil||gid<=0{return 0,0,errors.New("invalid state owner GID")}
 return uid,gid,nil
}

func (t *securityTransition) privateSnapshotParent(uid,gid int)error{
 if err:=os.MkdirAll(t.SnapshotParent,0700);err!=nil{return err}
 info,err:=os.Lstat(t.SnapshotParent)
 if err!=nil||!info.IsDir()||info.Mode()&os.ModeSymlink!=0||info.Mode().Perm()&0077!=0 {
  return errors.New("unsafe security migration snapshot directory")
 }
 stat,ok:=info.Sys().(*syscall.Stat_t)
 if !ok{return errors.New("snapshot owner unavailable")}
 if stat.Uid==0 {
  if err:=os.Chown(t.SnapshotParent,uid,gid);err!=nil{return err}
 }else if stat.Uid!=uint32(uid)||stat.Gid!=uint32(gid) {
  return errors.New("security snapshot directory is owned by another principal")
 }
 return nil
}

func (t *securityTransition) capture(ctx context.Context,fromSystem,toSystem,target,receiptPath string)error{
 uid,gid,err:=securityIdentity()
 if err!=nil{return err}
 if err:=t.privateSnapshotParent(uid,gid);err!=nil{return err}
 scratch,err:=os.MkdirTemp("/run","homelab-security-cutover-")
 if err!=nil{return err}
 defer os.RemoveAll(scratch)
 if err:=os.Chown(scratch,uid,gid);err!=nil{return err}
 if err:=os.Chmod(scratch,0700);err!=nil{return err}
 files:=map[string]string{
  "password":filepath.Join(t.SecretDir,"owner-verifier.json"),
  "totp":filepath.Join(t.SecretDir,"owner-totp"),
  "vault-key":filepath.Join(t.SecretDir,"vault.key"),
  "access-jwks":filepath.Join(t.SecretDir,"access-jwks.json"),
  "access-owner":t.OwnerEmailFile,
 }
 for name,source:=range files {
  raw,err:=transitionFile(source,1<<20)
  if err!=nil{return fmt.Errorf("security credential %s: %w",name,err)}
  path:=filepath.Join(scratch,name)
  if err:=os.WriteFile(path,raw,0600);err!=nil{return err}
  if err:=os.Chown(path,uid,gid);err!=nil{return err}
 }
 var goSystem,rustSystem string
 if target=="go"{goSystem=toSystem;rustSystem=fromSystem}else{goSystem=fromSystem;rustSystem=toSystem}
 goPreflight:=filepath.Join(goSystem,"sw/bin/homelab-security-preflight")
 rustPreflight:=filepath.Join(rustSystem,"sw/bin/security-preflight")
 migrator:=filepath.Join(goSystem,"sw/bin/homelab-security-migrate")
 for _,name:=range []*string{&goPreflight,&rustPreflight,&migrator}{
  resolved,err:=resolveRegularExecutable(*name)
  if err!=nil||!strings.HasPrefix(resolved,"/nix/store/"){
   return errors.New("immutable authority preflight or Go migrator unavailable")
  }
  *name=resolved
 }
 nonce:=make([]byte,12)
 if _,err:=rand.Read(nonce);err!=nil{return err}
 snapshot:=filepath.Join(t.SnapshotParent,hex.EncodeToString(nonce))
 args:=[]string{"-u","homelab-security","--",t.Env,"-i",
  "HOMELAB_SECURITY_OWNER=owner",
  "HOMELAB_SECURITY_PASSWORD_FILE="+filepath.Join(scratch,"password"),
  "HOMELAB_SECURITY_TOTP_FILE="+filepath.Join(scratch,"totp"),
  "HOMELAB_SECURITY_VAULT_KEY_FILE="+filepath.Join(scratch,"vault-key"),
  "HOMELAB_SECURITY_ACCESS_JWKS_FILE="+filepath.Join(scratch,"access-jwks"),
  "HOMELAB_SECURITY_ACCESS_OWNER_FILE="+filepath.Join(scratch,"access-owner"),
  "HOMELAB_SECURITY_POLICY_FILE="+t.PolicyFile,
  "HOMELAB_SECURITY_ACCESS_ISSUER="+t.Issuer,
  "HOMELAB_SECURITY_ACCESS_GATEWAY_AUD="+t.GatewayAudience,
  "HOMELAB_SECURITY_ACCESS_BROWSER_AUD="+t.BrowserAudience,
  "HOMELAB_SECURITY_BROWSER_ORIGIN="+t.BrowserOrigin,
  "HOMELAB_SECURITY_GO_PREFLIGHT="+goPreflight,
  "HOMELAB_SECURITY_COMPAT_PREFLIGHT="+rustPreflight,
  migrator,"prepare","--target",target,"--state-dir",t.StateDir,"--snapshot-dir",snapshot,
 }
 migrateCtx,cancel:=context.WithTimeout(ctx,180*time.Second)
 defer cancel()
 cmd:=exec.CommandContext(migrateCtx,t.Runuser,args...)
 cmd.Env=[]string{"PATH=/run/current-system/sw/bin:/usr/bin:/bin","LANG=C"}
 if output,err:=cmd.CombinedOutput();err!=nil {
  return fmt.Errorf("security transition compatibility rejected: %w (%s)",err,limitTransitionLog(output))
 }
 receipt,err:=os.ReadFile(filepath.Join(snapshot,"authority-cutover.json"))
 if err!=nil||len(receipt)==0||len(receipt)>4096{return errors.New("migration receipt unavailable")}
 var verified struct{
  Target string `json:"target"`
  AuthenticatorSHA256 string `json:"authenticator_sha256"`
  VaultSHA256 *string `json:"vault_sha256"`
 }
 if err:=json.Unmarshal(receipt,&verified);err!=nil||verified.Target!=target{
  return errors.New("migration receipt does not select the target authority")
 }
 authenticator,err:=os.ReadFile(filepath.Join(t.StateDir,"authenticator.json"))
 if err!=nil{return err}
 sum:=sha256.Sum256(authenticator)
 if verified.AuthenticatorSHA256!=hex.EncodeToString(sum[:]){
  return errors.New("authentication state changed during protected migration")
 }
 if verified.VaultSHA256!=nil {
  raw,err:=os.ReadFile(filepath.Join(t.StateDir,"credentials.json"))
  if err!=nil{return err}
  digest:=sha256.Sum256(raw)
  if *verified.VaultSHA256!=hex.EncodeToString(digest[:]){
   return errors.New("encrypted credential state changed during protected migration")
  }
 }
 if err:=installSecurityReceipt(receiptPath,receipt);err!=nil{return err}
 return nil
}

func installSecurityReceipt(path string,receipt []byte)error{
 dir:=filepath.Dir(path)
 current,err:=os.Lstat(path)
 if err==nil {
  stat,ok:=current.Sys().(*syscall.Stat_t)
  if !ok||!current.Mode().IsRegular()||stat.Uid!=0||current.Mode().Perm()&0077!=0{
   return errors.New("existing migration receipt has unsafe custody")
  }
 }else if !errors.Is(err,os.ErrNotExist){return err}
 f,err:=os.CreateTemp(dir,".authority-receipt-")
 if err!=nil{return err}
 defer os.Remove(f.Name())
 if err:=f.Chmod(0600);err!=nil{f.Close();return err}
 if _,err:=f.Write(receipt);err!=nil{f.Close();return err}
 if err:=f.Sync();err!=nil{f.Close();return err}
 if err:=f.Close();err!=nil{return err}
 if err:=os.Rename(f.Name(),path);err!=nil{return err}
 d,err:=os.Open(dir)
 if err!=nil{return err}
 defer d.Close()
 return d.Sync()
}


// NixOS generation sw/bin entries are symlinks into immutable package outputs.
// The independent migration verifier uses Lstat and intentionally rejects
// symlinks, so hand it the resolved package executable, not the system profile
// symlink. Never relax the verifier's non-writable regular-file checks.
func resolveRegularExecutable(name string)(string,error){
 resolved,err:=filepath.EvalSymlinks(name)
 if err!=nil{return "",err}
 info,err:=os.Lstat(resolved)
 if err!=nil||!info.Mode().IsRegular()||info.Mode().Perm()&0111==0||
  info.Mode().Perm()&0022!=0{
  return "",errors.New("resolved authority binary is not a trusted regular executable")
 }
 return resolved,nil
}
