package main

import (
 "context"
 "crypto/tls"
 "crypto/x509"
 "net"
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


func trustedRemoteListener(address string)error{
 host,_,err:=net.SplitHostPort(address)
 if err!=nil{return errors.New("remote executor requires an IP:port listener")}
 ip:=net.ParseIP(host)
 if ip==nil||!(ip.IsPrivate()||ip.IsLoopback()){return errors.New("remote executor must bind an explicit private IP address")}
 return nil
}

func remoteIdentity()(tls.Certificate,*x509.CertPool,error){
 cert,err:=tls.LoadX509KeyPair(required("HOMELAB_EXECUTOR_REMOTE_CERT"),required("HOMELAB_EXECUTOR_REMOTE_KEY"))
 if err!=nil{return tls.Certificate{},nil,errors.New("cannot load remote TLS server identity")}
 caBytes,err:=os.ReadFile(required("HOMELAB_EXECUTOR_REMOTE_CA"))
 if err!=nil{return tls.Certificate{},nil,errors.New("cannot read coordinator trust roots")}
 roots:=x509.NewCertPool()
 if !roots.AppendCertsFromPEM(caBytes){return tls.Certificate{},nil,errors.New("invalid coordinator trust roots")}
 return cert,roots,nil
}

func authorizationClient()(privileged.Authorizer,error){
 if endpoint:=os.Getenv("HOMELAB_EXECUTOR_AUTH_ENDPOINT");endpoint!=""{
  cert,err:=tls.LoadX509KeyPair(required("HOMELAB_EXECUTOR_AUTH_CERT"),required("HOMELAB_EXECUTOR_AUTH_KEY"))
  if err!=nil{return nil,errors.New("cannot load executor authorization identity")}
  rootsData,err:=os.ReadFile(required("HOMELAB_EXECUTOR_AUTH_CA"))
  if err!=nil{return nil,errors.New("cannot load authorization trust roots")}
  roots:=x509.NewCertPool()
  if !roots.AppendCertsFromPEM(rootsData){return nil,errors.New("invalid authorization trust roots")}
  return security.NewRemoteAuthorizationClient(security.RemoteAuthorizationConfig{
   Endpoint:endpoint,ServerName:required("HOMELAB_EXECUTOR_AUTH_SERVER_NAME"),
   TrustRoots:roots,Certificate:cert,
  })
 }
 return security.NewAdminClient(required("HOMELAB_EXECUTOR_SECURITY_SOCKET"))
}

func main(){
 log.SetFlags(log.LstdFlags|log.LUTC)
 host:=required("HOMELAB_EXECUTOR_HOST")
 client,err:=authorizationClient()
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
 var kubernetes *privileged.KubernetesPolicy
 if file:=os.Getenv("HOMELAB_EXECUTOR_KUBERNETES_POLICY_FILE");file!=""{
  raw,err:=os.ReadFile(file)
  if err!=nil||len(raw)>64<<10{log.Fatal("invalid Kubernetes policy file")}
  var policy privileged.KubernetesPolicy
  if err:=json.Unmarshal(raw,&policy);err!=nil{log.Fatal("invalid Kubernetes policy")}
  policy.Binary=required("HOMELAB_EXECUTOR_KUBECTL")
  policy.Kubeconfig=required("HOMELAB_EXECUTOR_KUBECONFIG")
  kubernetes=&policy
 }
 var workspace *privileged.WorkspacePolicy
 if file:=os.Getenv("HOMELAB_EXECUTOR_WORKSPACE_POLICY_FILE");file!=""{
  raw,err:=os.ReadFile(file)
  if err!=nil||len(raw)>64<<10{log.Fatal("invalid Workspace executor policy file")}
  var policy privileged.WorkspacePolicy
  if err:=json.Unmarshal(raw,&policy);err!=nil{log.Fatal("invalid Workspace policy")}
  policy.TokenFile=required("HOMELAB_EXECUTOR_WORKSPACE_TOKEN")
  workspace=&policy
 }
 e,err:=privileged.New(privileged.Config{
  Host:host,Systemctl:systemctl,AllowedServices:services,AllowRelease:os.Getenv("HOMELAB_EXECUTOR_RELEASE_ENABLE")=="true",
  Kubernetes:kubernetes,Workspace:workspace,Auth:client,Run:privileged.FixedRun,Releases:releases,
 })
 if err!=nil{log.Fatal(err)}
 group,err:=user.LookupGroup("homelab-executor-client");if err!=nil{log.Fatal(err)}
 gid,err:=strconv.Atoi(group.Gid);if err!=nil||gid<=0{log.Fatal("invalid executor client group")}
 ctx,stop:=signal.NotifyContext(context.Background(),syscall.SIGINT,syscall.SIGTERM)
 defer stop()
 path:="/run/homelab-executord/execute.sock"
 if strings.ContainsAny(path,"\x00") {log.Fatal(errors.New("invalid socket path"))}
 failures:=make(chan error,2)
 go func(){failures<-e.ServeSocket(ctx,path,gid)}()
 if address:=os.Getenv("HOMELAB_EXECUTOR_REMOTE_LISTEN");address!=""{
  if err:=trustedRemoteListener(address);err!=nil{log.Fatal(err)}
  cert,roots,err:=remoteIdentity()
  if err!=nil{log.Fatal(err)}
  coordinator:=required("HOMELAB_EXECUTOR_REMOTE_COORDINATOR")
  go func(){failures<-e.ServeMutualTLS(ctx,address,cert,roots,coordinator)}()
 }
 log.Print("restricted executor endpoints starting")
 err=<-failures
 stop()
 if err!=nil{log.Fatal(err)}
}
