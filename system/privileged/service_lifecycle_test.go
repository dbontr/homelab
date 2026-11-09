package privileged

import (
 "context"
 "encoding/json"
 "strings"
 "testing"

 "github.com/dbontr/homelab-new/system/security"
)

func TestAllowlistedServiceLifecycleUsesOnlyFixedSystemctlVerbs(t *testing.T){
 auth:=&fakeAuth{decision:security.Authorization{Allowed:true}}
 commands:=[]string{}
 exec,err:=New(Config{Audit:testExecutorAudit(t),
  Host:"jupiter",Systemctl:"/usr/bin/systemctl",
  AllowedServices:[]string{"homelab-platform.service"},
  Auth:auth,Releases:&fakeRelease{},
  Run:func(_ context.Context,cmd string,args ...string)error{
   if cmd!="/usr/bin/systemctl"||len(args)!=2||args[1]!="homelab-platform.service"{
    t.Fatal("unsafe system command constructed",cmd,args)
   }
   commands=append(commands,args[0]);return nil
  },
 })
 if err!=nil{t.Fatal(err)}
 args:=json.RawMessage(`{"host":"jupiter","service":"homelab-platform.service"}`)
 for _,tc:=range []struct{operation,verb string}{
  {"infrastructure__service_start","start"},
  {"infrastructure__service_stop","stop"},
  {"infrastructure__service_reload","reload"},
  {"infrastructure__service_restart","restart"},
 }{
  result,err:=exec.Execute(context.Background(),tc.operation,"jupiter",args)
  if err!=nil||!result.Allowed||len(commands)==0||commands[len(commands)-1]!=tc.verb{
   t.Fatal("incorrect fixed action",tc,commands,err)
  }
 }
 calls:=auth.calls
 for _,operation:=range []string{"infrastructure__service_enable","infrastructure__service_disable","infrastructure__service_shell"}{
  result,err:=exec.Execute(context.Background(),operation,"jupiter",args)
  if err!=nil||result.Allowed||auth.calls!=calls{t.Fatal("unknown action reached authorization",operation)}
 }
 for _,bad:=range []json.RawMessage{
  json.RawMessage(`{"host":"jupiter","service":"sshd.service"}`),
  json.RawMessage(`{"host":"jupiter","service":"homelab-platform.service","command":"sh"}`),
  json.RawMessage(`{"host":"venus","service":"homelab-platform.service"}`),
 }{
  result,err:=exec.Execute(context.Background(),"infrastructure__service_stop","jupiter",bad)
  if err!=nil||result.Allowed||auth.calls!=calls{t.Fatal("invalid unit or argument reached authorization",string(bad))}
 }
 auth.decision=security.Authorization{Reason:"owner confirmation required"}
 result,err:=exec.Execute(context.Background(),"infrastructure__service_stop","jupiter",args)
 if err!=nil||result.Allowed||len(commands)!=4{t.Fatal("owner denial allowed service mutation")}
}

func TestExecutorNeverAllowsCriticalServices(t *testing.T){
 for _,service:=range []string{
  "sshd.service","systemd-networkd.service","homelab-securityd.service","homelab-executord.service",
  "nftables.service","wireguard.service","homelab-fleetd.service","prometheus.service",
 }{
  _,err:=New(Config{Audit:testExecutorAudit(t),
   Host:"saturn",Systemctl:"/usr/bin/systemctl",AllowedServices:[]string{service},
   Auth:&fakeAuth{},Releases:&fakeRelease{},
   Run:func(context.Context,string,...string)error{return nil},
  })
  if err==nil||!strings.Contains(err.Error(),"out-of-band"){t.Fatalf("critical service %s is configurable: %v",service,err)}
 }
}
