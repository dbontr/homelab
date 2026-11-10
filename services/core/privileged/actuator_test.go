package privileged

import (
 "context"
 "fmt"
 "os"
 "path/filepath"
 "strings"
 "testing"
)

func TestRustActuatorRejectsUnregisteredCommands(t *testing.T){
 dir:=t.TempDir()
 out:=filepath.Join(dir,"args.txt")
 bin:=filepath.Join(dir,"actuator")
 script:=fmt.Sprintf("#!/bin/sh\nprintf '%%s\\n' \"$@\" > %q\n",out)
 if err:=os.WriteFile(bin,[]byte(script),0700);err!=nil{t.Fatal(err)}
 config:=RustActuatorConfig{
  Binary:bin,Systemctl:"/usr/bin/systemctl",Kubectl:"/usr/bin/kubectl",
  Kubeconfig:"/run/credentials/homelab-executord.service/kubeconfig",
  ServicePolicy:"/nix/store/allowlisted-services",KubernetesPolicy:"/nix/store/allowlisted-deployments",
  MaxReplicas:8,
 }
 run,err:=NewRustRun(config)
 if err!=nil{t.Fatal(err)}
 cases:=[]struct{
  name,command string
  args []string
  want string
  permitted bool
 }{
  {"service","/usr/bin/systemctl",[]string{"restart","homelab-platform.service"},"service\nrestart\nhomelab-platform.service",true},
  {"restart","/usr/bin/kubectl",[]string{"--kubeconfig="+config.Kubeconfig,"--namespace=apps","--request-timeout=15s","rollout","restart","deployment/workspace"},"deployment\nrestart\napps\nworkspace",true},
  {"scale","/usr/bin/kubectl",[]string{"--kubeconfig="+config.Kubeconfig,"--namespace=apps","--request-timeout=15s","scale","deployment/workspace","--replicas=4"},"deployment\nscale\napps\nworkspace\n4",true},
  {"arbitrary shell","/bin/sh",[]string{"-c","whoami"},"",false},
  {"systemctl options","/usr/bin/systemctl",[]string{"--root","homelab-platform.service"},"",false},
  {"replica overflow","/usr/bin/kubectl",[]string{"--kubeconfig="+config.Kubeconfig,"--namespace=apps","--request-timeout=15s","scale","deployment/workspace","--replicas=9"},"",false},
  {"unexpected kubectl flag","/usr/bin/kubectl",[]string{"--kubeconfig="+config.Kubeconfig,"--namespace=apps","--request-timeout=15s","scale","deployment/workspace","--force"},"",false},
 }
 for _,tc:=range cases{
  t.Run(tc.name,func(t *testing.T){
   _=os.Remove(out)
   err:=run(context.Background(),tc.command,tc.args...)
   if (err==nil)!=tc.permitted{t.Fatalf("allowed=%t err=%v",tc.permitted,err)}
   data,readErr:=os.ReadFile(out)
   if tc.permitted{
    if readErr!=nil||strings.TrimSpace(string(data))!=tc.want{t.Fatalf("expected %q, got %q (%v)",tc.want,data,readErr)}
   }else if readErr==nil{
    t.Fatal("rejected command reached the actuator")
   }
  })
 }
}

func TestRustActuatorRequiresTrustedConfiguration(t *testing.T){
 _,err:=NewRustRun(RustActuatorConfig{Binary:"actuator",Systemctl:"/usr/bin/systemctl",ServicePolicy:"/nix/store/allow",KubernetesPolicy:"/nix/store/kube",MaxReplicas:4})
 if err==nil{t.Fatal("relative actuator path accepted")}
 _,err=NewRustRun(RustActuatorConfig{Binary:"/nix/store/actuator",Systemctl:"/usr/bin/systemctl",ServicePolicy:"/nix/store/allow",KubernetesPolicy:"/nix/store/kube",MaxReplicas:1001})
 if err==nil{t.Fatal("unbounded replicas accepted")}
}
