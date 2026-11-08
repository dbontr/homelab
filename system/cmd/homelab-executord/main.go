package main

import (
 "context"
 "encoding/json"
 "errors"
 "log"
 "os"
 "os/signal"
 "os/user"
 "path/filepath"
 "strconv"
 "strings"
 "syscall"

 "github.com/dbontr/homelab-new/system/privileged"
 "github.com/dbontr/homelab-new/system/release"
 "github.com/dbontr/homelab-new/system/security"
)

func required(name string)string{
 value:=os.Getenv(name)
 if value==""{log.Fatal("missing executor configuration: ",name)}
 return value
}

func main(){
 log.SetFlags(log.LstdFlags|log.LUTC)
 host:=required("HOMELAB_EXECUTOR_HOST")
 socket:=required("HOMELAB_EXECUTOR_SECURITY_SOCKET")
 client,err:=security.NewAdminClient(socket)
 if err!=nil{log.Fatal(err)}
 policyFile:=required("HOMELAB_EXECUTOR_SERVICES_FILE")
 data,err:=os.ReadFile(policyFile);if err!=nil{log.Fatal(err)}
 var services []string
 if err:=json.Unmarshal(data,&services);err!=nil{log.Fatal("invalid service allowlist")}
 healthFile:=required("HOMELAB_EXECUTOR_HEALTH_FILE")
 healthData,err:=os.ReadFile(healthFile);if err!=nil{log.Fatal(err)}
 var health []string
 if err:=json.Unmarshal(healthData,&health);err!=nil{log.Fatal(err)}
 if len(health)==0{log.Fatal("at least one health service is required")}
 source:=required("HOMELAB_EXECUTOR_SOURCE")
 state:=required("HOMELAB_EXECUTOR_RELEASE_STATE")
 nix:=required("HOMELAB_EXECUTOR_NIX")
 nixEnv:=required("HOMELAB_EXECUTOR_NIX_ENV")
 git:=required("HOMELAB_EXECUTOR_GIT")
 systemctl:=required("HOMELAB_EXECUTOR_SYSTEMCTL")
 releases,err:=release.New(release.Config{
  Host:host,FlakeDir:source,StateDir:state,TrustedSource:true,Nix:nix,NixEnv:nixEnv,Git:git,Systemctl:systemctl,
  HealthServices:health,Run:release.Exec,
  Current:func()(string,error){return filepath.EvalSymlinks("/nix/var/nix/profiles/system")},
 })
 if err!=nil{log.Fatal("invalid release policy: ",err)}
 e,err:=privileged.New(privileged.Config{
  Host:host,Systemctl:systemctl,AllowedServices:services,AllowRelease:os.Getenv("HOMELAB_EXECUTOR_RELEASE_ENABLE")=="true",
  Auth:client,Run:privileged.FixedRun,Releases:releases,
 })
 if err!=nil{log.Fatal(err)}
 group,err:=user.LookupGroup("homelab-executor-client");if err!=nil{log.Fatal(err)}
 gid,err:=strconv.Atoi(group.Gid);if err!=nil||gid<=0{log.Fatal("invalid executor client group")}
 ctx,stop:=signal.NotifyContext(context.Background(),syscall.SIGINT,syscall.SIGTERM)
 defer stop()
 path:="/run/homelab-executord/execute.sock"
 if strings.ContainsAny(path,"\x00") {log.Fatal(errors.New("invalid socket path"))}
 log.Print("restricted local executor socket starting")
 if err:=e.ServeSocket(ctx,path,gid);err!=nil{log.Fatal(err)}
}
