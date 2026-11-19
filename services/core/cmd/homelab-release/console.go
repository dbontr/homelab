package main

import (
 "context"
 "crypto/rand"
 "encoding/hex"
 "encoding/json"
 "errors"
 "fmt"
 "io"
 "os"
 "os/exec"
 "path/filepath"
 "regexp"
 "strings"
 "syscall"
 "time"

 "github.com/dbontr/homelab-new/system/release"
)

var consoleSessionID=regexp.MustCompile("^[0-9]{1,8}$")
var consoleTTY=regexp.MustCompile("^/dev/tty[1-9][0-9]*$")

// Console authority is restricted to an authenticated root login attached
// to the host's local seat. Pseudoterminals and remote sessions are denied.
func validConsoleSession(uid int,tty,sessionID string,properties map[string]string)bool{
 if uid!=0||!consoleTTY.MatchString(tty)||!consoleSessionID.MatchString(sessionID){return false}
 return properties["User"]=="0"&&properties["Seat"]=="seat0"&&
  properties["Type"]=="tty"&&properties["Remote"]=="no"&&
  properties["Service"]=="login"&&properties["TTY"]==strings.TrimPrefix(tty,"/dev/")
}

func consoleSession()(string,string,error){
 if os.Getuid()!=0||os.Geteuid()!=0{return "","",errors.New("root console login required")}
 info,err:=os.Stdin.Stat()
 if err!=nil||info.Mode()&os.ModeCharDevice==0{return "","",errors.New("physical console input required")}
 tty,err:=os.Readlink("/proc/self/fd/0")
 if err!=nil||!consoleTTY.MatchString(tty){return "","",errors.New("local virtual terminal required")}
 sessionID:=os.Getenv("XDG_SESSION_ID")
 if !consoleSessionID.MatchString(sessionID){return "","",errors.New("login session unavailable")}
 ctx,cancel:=context.WithTimeout(context.Background(),5*time.Second)
 defer cancel()
 cmd:=exec.CommandContext(ctx,"/run/current-system/sw/bin/loginctl","show-session",sessionID,
  "-p","User","-p","Seat","-p","Type","-p","Remote","-p","Service","-p","TTY")
 output,err:=cmd.Output()
 if err!=nil{return "","",errors.New("cannot verify local login session")}
 attributes:=map[string]string{}
 for _,line:=range strings.Split(string(output),"\n"){
  parts:=strings.SplitN(line,"=",2)
  if len(parts)==2{attributes[parts[0]]=parts[1]}
 }
 if !validConsoleSession(os.Getuid(),tty,sessionID,attributes){
  return "","",errors.New("login is not a root session on the local seat")
 }
 return tty,sessionID,nil
}

type consoleReleaseIntent struct{
 RequestID string `json:"request_id"`
 ReleaseID string `json:"release_id"`
 Host string `json:"host"`
 Authority string `json:"authority"`
 TTY string `json:"tty"`
 SessionID string `json:"session_id"`
 Created time.Time `json:"created"`
}

type consoleReleaseResult struct{
 RequestID string `json:"request_id"`
 ReleaseID string `json:"release_id"`
 Outcome string `json:"outcome"`
 Detail string `json:"detail,omitempty"`
 Completed time.Time `json:"completed"`
}

func consoleRecordPath(state,kind,id string)string{
 return filepath.Join(state,"console-"+kind+"-"+id+".json")
}

func consoleStateDirectory(state string)error{
 info,err:=os.Lstat(state)
 if err!=nil||!info.IsDir()||info.Mode().Perm()&0077!=0||info.Mode()&os.ModeSymlink!=0{
  return errors.New("trusted release state directory unavailable")
 }
 owner,ok:=info.Sys().(*syscall.Stat_t)
 if !ok||owner.Uid!=uint32(os.Geteuid()){return errors.New("release state is not root-owned")}
 return nil
}

func consoleWriteRecord(state,kind,id string,record any)error{
 if !validRequestID(id){return errors.New("invalid local release request ID")}
 if err:=consoleStateDirectory(state);err!=nil{return err}
 path:=consoleRecordPath(state,kind,id)
 f,err:=os.OpenFile(path,os.O_WRONLY|os.O_CREATE|os.O_EXCL,0600)
 if err!=nil{return err}
 defer f.Close()
 if err:=json.NewEncoder(f).Encode(record);err!=nil{return err}
 if err:=f.Sync();err!=nil{return err}
 d,err:=os.Open(state)
 if err!=nil{return err}
 defer d.Close()
 return d.Sync()
}

func consoleReadIntent(state,id string)(consoleReleaseIntent,error){
 var intent consoleReleaseIntent
 if !validRequestID(id){return intent,errors.New("invalid local release request ID")}
 if err:=consoleStateDirectory(state);err!=nil{return intent,err}
 path:=consoleRecordPath(state,"intent",id)
 info,err:=os.Lstat(path)
 if err!=nil||!info.Mode().IsRegular()||info.Mode().Perm()!=0600{
  return intent,errors.New("local release authorization record unavailable")
 }
 owner,ok:=info.Sys().(*syscall.Stat_t)
 if !ok||owner.Uid!=uint32(os.Geteuid()){return intent,errors.New("local release authorization record is not root-owned")}
 f,err:=os.Open(path)
 if err!=nil{return intent,err}
 defer f.Close()
 dec:=json.NewDecoder(io.LimitReader(f,2049))
 dec.DisallowUnknownFields()
 if err:=dec.Decode(&intent);err!=nil{return intent,err}
 var extra any
 if err:=dec.Decode(&extra);err!=io.EOF{return intent,errors.New("invalid console authorization envelope")}
 if intent.RequestID!=id||intent.Authority!="local-root-console"||
  !validStagedID(intent.ReleaseID)||!consoleTTY.MatchString(intent.TTY)||
  !consoleSessionID.MatchString(intent.SessionID)||
  intent.Created.IsZero()||time.Since(intent.Created)>15*time.Minute||
  time.Until(intent.Created)>time.Minute {
  return intent,errors.New("local console authorization is invalid or expired")
 }
 return intent,nil
}

func consoleRequestID()(string,error){
 var entropy [16]byte
 if _,err:=rand.Read(entropy[:]);err!=nil{return "",err}
 return hex.EncodeToString(entropy[:]),nil
}

// consoleActivate records local authority before starting a detached,
// source-pinned guardian. Remote MCP identity is neither forged nor reused.
func consoleActivate(ctrl *release.Controller,stageID string)error{
 if ctrl==nil||!ctrl.Config.TrustedSource||!validStagedID(stageID){
  return errors.New("trusted exact-stage release required")
 }
 tty,sessionID,err:=consoleSession()
 if err!=nil{return err}
 if err:=ctrl.VerifyStaged(stageID);err!=nil{return err}
 requestID,err:=consoleRequestID()
 if err!=nil{return err}
 intent:=consoleReleaseIntent{RequestID:requestID,ReleaseID:stageID,
  Host:ctrl.Config.Host,Authority:"local-root-console",TTY:tty,
  SessionID:sessionID,Created:time.Now().UTC()}
 if err:=consoleWriteRecord(ctrl.Config.StateDir,"intent",requestID,intent);err!=nil{return err}
 runner:=os.Getenv("HOMELAB_RELEASE_RUNNER_SELF")
 systemdRun:=os.Getenv("HOMELAB_RELEASE_SYSTEMD_RUN")
 if !filepath.IsAbs(runner)||!filepath.IsAbs(systemdRun){
  return errors.New("fixed detached release runner unavailable")
 }
 unit:="homelab-console-release-"+requestID
 ctx,cancel:=context.WithTimeout(context.Background(),15*time.Second)
 defer cancel()
 command:=exec.CommandContext(ctx,systemdRun,"--no-block","--collect",
  "--unit="+unit,"--property=Type=oneshot",
  "--property=TimeoutStartSec=25min",
  "--property=StandardOutput=journal",
  "--property=StandardError=journal",
  runner,"console-guard-activate",requestID,stageID)
 output,err:=command.CombinedOutput()
 if err!=nil{return fmt.Errorf("detached console release unavailable: %w (%s)",err,strings.TrimSpace(string(output)))}
 return json.NewEncoder(os.Stdout).Encode(map[string]string{
  "state":"accepted","authority":"local-root-console",
  "request_id":requestID,"release_id":stageID,"unit":unit,
 })
}

// The guardian can consume each console intent only once. Its own audit
// record is distinct from independently signed remote owner approvals.
func consoleGuardActivate(ctx context.Context,ctrl *release.Controller,requestID,stageID string)error{
 if ctrl==nil||!validRequestID(requestID)||!validStagedID(stageID){
  return errors.New("invalid local console release")
 }
 intent,err:=consoleReadIntent(ctrl.Config.StateDir,requestID)
 if err!=nil{return err}
 if intent.Host!=ctrl.Config.Host||intent.ReleaseID!=stageID{return errors.New("console request scope mismatch")}
 if err:=ctrl.VerifyStaged(stageID);err!=nil{return err}
 if err:=consoleWriteRecord(ctrl.Config.StateDir,"claimed",requestID,map[string]any{
  "request_id":requestID,"release_id":stageID,"authority":"local-root-console","started":time.Now().UTC(),
 });err!=nil{return fmt.Errorf("console release replay or audit failure: %w",err)}
 err=ctrl.Activate(ctx,stageID)
 result:=consoleReleaseResult{RequestID:requestID,ReleaseID:stageID,Outcome:"completed",Completed:time.Now().UTC()}
 if err!=nil{result.Outcome="failed";result.Detail=err.Error()}
 if writeErr:=consoleWriteRecord(ctrl.Config.StateDir,"result",requestID,result);writeErr!=nil{
  return fmt.Errorf("release outcome could not be audited: %w",writeErr)
 }
 return err
}
