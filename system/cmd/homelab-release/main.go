package main

import (
 "context"
 "encoding/json"
 "errors"
 "fmt"
 "os"
 "path/filepath"
 "time"

 "github.com/dbontr/homelab-new/system/release"
)

func configured()( *release.Controller,error){
 host:=os.Getenv("HOMELAB_RELEASE_HOST")
 source:=os.Getenv("HOMELAB_RELEASE_SOURCE")
 state:=os.Getenv("HOMELAB_RELEASE_STATE")
 if host==""||source==""||state==""{return nil,errors.New("release host, immutable source and private state directory are required")}
 healthFile:=os.Getenv("HOMELAB_RELEASE_HEALTH_FILE")
 if healthFile==""{return nil,errors.New("release health policy required")}
 data,err:=os.ReadFile(healthFile)
 if err!=nil{return nil,err}
 var health []string
 if err:=json.Unmarshal(data,&health);err!=nil||len(health)==0{return nil,errors.New("invalid release health policy")}
 cfg:=release.Config{
  Host:host,FlakeDir:source,StateDir:state,TrustedSource:true,
  Nix:os.Getenv("HOMELAB_RELEASE_NIX"),
  NixEnv:os.Getenv("HOMELAB_RELEASE_NIX_ENV"),
  Git:os.Getenv("HOMELAB_RELEASE_GIT"),
  Systemctl:os.Getenv("HOMELAB_RELEASE_SYSTEMCTL"),
  HealthServices:health,
  Run:release.Exec,
  Current:func()(string,error){
   path,err:=filepath.EvalSymlinks("/nix/var/nix/profiles/system")
   if err!=nil{return "",err}
   return path,nil
  },
 }
 return release.New(cfg)
}

func run(args []string)error{
 if os.Geteuid()!=0{return errors.New("release management requires a local administrator")}
 ctrl,err:=configured();if err!=nil{return err}
 if len(args)==0{return errors.New("expected stage, activate, recover, or status")}
 duration:=15*time.Minute
 if args[0]=="stage"{duration=2*time.Hour}
 if args[0]=="status"{duration=10*time.Second}
 ctx,cancel:=context.WithTimeout(context.Background(),duration)
 defer cancel()
 switch args[0]{
 case "stage":
  if len(args)!=2{return errors.New("stage requires an exact source commit")}
  manifest,err:=ctrl.Stage(ctx,args[1]);if err!=nil{return err}
  return json.NewEncoder(os.Stdout).Encode(manifest)
 case "activate":
  if len(args)!=2{return errors.New("activate requires an exact staged release ID")}
  if err:=ctrl.Activate(ctx,args[1]);err!=nil{return err}
  return json.NewEncoder(os.Stdout).Encode(map[string]string{"status":"active","release":args[1]})
 case "revert":
  if len(args)!=2{return errors.New("revert requires the active staged release ID")}
  return ctrl.Revert(ctx,args[1])
 case "recover":
  if len(args)!=1{return errors.New("recover accepts no arguments")}
  return ctrl.Recover(ctx)
 case "status":
  if len(args)!=1{return errors.New("status accepts no arguments")}
  status,err:=ctrl.Status();if err!=nil{return err}
  return json.NewEncoder(os.Stdout).Encode(status)
 default:return errors.New("unknown release operation")
 }
}

func main(){
 if err:=run(os.Args[1:]);err!=nil{
  fmt.Fprintln(os.Stderr,"homelab-release:",err)
  os.Exit(1)
 }
}
