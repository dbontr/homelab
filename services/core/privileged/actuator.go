package privileged

import (
 "context"
 "errors"
 "io"
 "os/exec"
 "path/filepath"
 "strconv"
 "strings"
 "syscall"
)

// RustActuatorConfig is trusted operator configuration, not a tool argument.
// Source policies are immutable files in the Nix store.
type RustActuatorConfig struct {
 Binary string
 Systemctl string
 Kubectl string
 Kubeconfig string
 ServicePolicy string
 KubernetesPolicy string
 MaxReplicas int
}

// NewRustRun translates an already authorized typed operation into one fixed
// Rust actuator action. It has no generic shell or user-selected binary path.
func NewRustRun(cfg RustActuatorConfig)(Run,error){
 for _,path:=range []string{cfg.Binary,cfg.Systemctl,cfg.ServicePolicy,cfg.KubernetesPolicy}{
  if !filepath.IsAbs(path)||filepath.Clean(path)!=path{return nil,errors.New("Rust actuator requires canonical absolute paths")}
 }
 if (cfg.Kubectl=="")!=(cfg.Kubeconfig=="")||cfg.MaxReplicas<1||cfg.MaxReplicas>1000{
  return nil,errors.New("invalid Rust actuator Kubernetes configuration")
 }
 if cfg.Kubectl!=""&&(!filepath.IsAbs(cfg.Kubectl)||!filepath.IsAbs(cfg.Kubeconfig)){
  return nil,errors.New("invalid Rust actuator Kubernetes path")
 }
 return func(ctx context.Context,binary string,argv ...string)error{
  var args []string
  switch{
  case binary==cfg.Systemctl:
   if len(argv)!=2||!(argv[0]=="start"||argv[0]=="stop"||argv[0]=="reload"||argv[0]=="restart")||!unitPattern.MatchString(argv[1]){
    return errors.New("invalid Rust systemd action")
   }
   args=[]string{"service",argv[0],argv[1]}
  case cfg.Kubectl!=""&&binary==cfg.Kubectl:
   if (len(argv)!=6&&len(argv)!=7)||argv[0]!="--kubeconfig="+cfg.Kubeconfig||
    !strings.HasPrefix(argv[1],"--namespace=")||argv[2]!="--request-timeout=15s"{
    return errors.New("invalid Rust Kubernetes action")
   }
   namespace:=strings.TrimPrefix(argv[1],"--namespace=")
   if !validKubeName(namespace){return errors.New("invalid Rust Kubernetes namespace")}
   if len(argv)==6&&argv[3]=="rollout"&&argv[4]=="restart"&&strings.HasPrefix(argv[5],"deployment/"){
    deployment:=strings.TrimPrefix(argv[5],"deployment/")
    if !validKubeName(deployment){return errors.New("invalid Rust Kubernetes deployment")}
    args=[]string{"deployment","restart",namespace,deployment}
   }else if argv[3]=="scale"&&strings.HasPrefix(argv[4],"deployment/")&&strings.HasPrefix(argv[5],"--replicas="){
    deployment:=strings.TrimPrefix(argv[4],"deployment/")
    replicas:=strings.TrimPrefix(argv[5],"--replicas=")
    if !validKubeName(deployment)||replicas==""||strings.Trim(replicas,"0123456789")!=""{
     return errors.New("invalid Rust Kubernetes scale")
    }
    count,err:=strconv.Atoi(replicas)
    if err!=nil||count<0||count>cfg.MaxReplicas{return errors.New("Rust replica limit exceeded")}
    args=[]string{"deployment","scale",namespace,deployment,strconv.Itoa(count)}
    if len(argv)==7{
     if !strings.HasPrefix(argv[6],"--resource-version="){
      return errors.New("unsupported Rust Kubernetes scale precondition")
     }
     version:=strings.TrimPrefix(argv[6],"--resource-version=")
     if !kubeResourceVersion.MatchString(version){return errors.New("invalid Rust Kubernetes resource version")}
     args=append(args,version)
    }
   }else{return errors.New("unsupported Rust Kubernetes arguments")}
  default:return errors.New("unsupported Rust administrative binary")
  }
  cmd:=exec.CommandContext(ctx,cfg.Binary,args...)
  cmd.Env=[]string{
   "HOME=/var/empty","LANG=C","PATH=/run/current-system/sw/bin:/usr/bin:/bin",
   "HOMELAB_ACTUATOR_SYSTEMCTL="+cfg.Systemctl,
   "HOMELAB_ACTUATOR_KUBECTL="+cfg.Kubectl,
   "HOMELAB_ACTUATOR_KUBECONFIG="+cfg.Kubeconfig,
   "HOMELAB_ACTUATOR_SERVICES="+cfg.ServicePolicy,
   "HOMELAB_ACTUATOR_KUBERNETES="+cfg.KubernetesPolicy,
   "HOMELAB_ACTUATOR_MAX_REPLICAS="+strconv.Itoa(cfg.MaxReplicas),
  }
  cmd.SysProcAttr=&syscall.SysProcAttr{Setpgid:true}
  cmd.Cancel=func()error{return syscall.Kill(-cmd.Process.Pid,syscall.SIGKILL)}
  cmd.Stdin=nil
  cmd.Stdout=io.Discard
  cmd.Stderr=io.Discard
  err:=cmd.Run()
  if canceled:=ctx.Err();canceled!=nil{return canceled}
  return err
 },nil
}
