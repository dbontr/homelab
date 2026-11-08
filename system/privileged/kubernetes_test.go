package privileged

import (
 "context"
 "encoding/json"
 "strings"
 "testing"

 "github.com/dbontr/homelab-new/system/security"
)

func TestKubernetesDeploymentCommandPolicy(t *testing.T) {
 p:=&KubernetesPolicy{
  Binary:"/run/current-system/sw/bin/kubectl",
  Kubeconfig:"/run/credentials/homelab-executord.service/kubeconfig",
  Namespaces:map[string][]string{"apps":{"workspace","health"}},
  MaxReplicas:12,
 }
 if err:=p.validate();err!=nil{t.Fatal(err)}
 cases:=[]struct{operation string;args string;allowed bool}{
  {"cluster__deployment_restart",`{"host":"saturn","namespace":"apps","deployment":"workspace"}`,true},
  {"cluster__deployment_scale",`{"host":"saturn","namespace":"apps","deployment":"health","replicas":0}`,true},
  {"cluster__deployment_scale",`{"host":"saturn","namespace":"apps","deployment":"health","replicas":12}`,true},
  {"cluster__deployment_scale",`{"host":"saturn","namespace":"apps","deployment":"health","replicas":13}`,false},
  {"cluster__deployment_scale",`{"host":"saturn","namespace":"apps","deployment":"health","replicas":1.5}`,false},
  {"cluster__deployment_scale",`{"host":"saturn","namespace":"apps","deployment":"health","replicas":"2"}`,false},
  {"cluster__deployment_restart",`{"host":"saturn","namespace":"kube-system","deployment":"workspace"}`,false},
  {"cluster__deployment_restart",`{"host":"saturn","namespace":"apps","deployment":"other"}`,false},
  {"cluster__deployment_restart",`{"host":"saturn","namespace":"apps","deployment":"--all"}`,false},
  {"cluster__deployment_restart",`{"host":"saturn","namespace":"apps","deployment":"workspace","command":"shell"}`,false},
 }
 for _,tc:=range cases{
  var fields map[string]any
  decoder:=json.NewDecoder(strings.NewReader(tc.args))
  decoder.UseNumber()
  if err:=decoder.Decode(&fields);err!=nil{t.Fatal(err)}
  cmd,argv,err:=p.command(tc.operation,fields)
  if tc.allowed {
   if err!=nil||cmd!=p.Binary||len(argv)<5{t.Fatalf("allowed command rejected: %s %v",tc.args,err)}
  }else if err==nil{t.Fatalf("forbidden command accepted: %s %v",tc.args,argv)}
 }
}

func TestKubernetesExecutorIndependentAuthorization(t *testing.T){
 auth:=&fakeAuth{decision:security.Authorization{Reason:"owner confirmation required"}}
 called:=0
 p:=&KubernetesPolicy{Binary:"/usr/bin/kubectl",Kubeconfig:"/run/credentials/homelab-executord.service/kubeconfig",Namespaces:map[string][]string{"apps":{"workspace"}},MaxReplicas:3}
 e,err:=New(Config{Host:"saturn",Systemctl:"/usr/bin/systemctl",Kubernetes:p,Auth:auth,Releases:&fakeRelease{},Run:func(_ context.Context,command string,args ...string)error{
  called++
  if command!="/usr/bin/kubectl"||len(args)!=6||args[3]!="rollout"||args[4]!="restart"||args[5]!="deployment/workspace"{t.Fatalf("unexpected fixed command: %s %v",command,args)}
  return nil
 }})
 if err!=nil{t.Fatal(err)}
 args:=json.RawMessage(`{"host":"saturn","namespace":"apps","deployment":"workspace"}`)
 denied,err:=e.Execute(context.Background(),"cluster__deployment_restart","saturn",args)
 if err!=nil||denied.Allowed||called!=0||auth.calls!=1{t.Fatal("independent confirmation bypassed",err)}
 auth.decision=security.Authorization{Allowed:true}
 allowed,err:=e.Execute(context.Background(),"cluster__deployment_restart","saturn",args)
 if err!=nil||!allowed.Allowed||called!=1||auth.calls!=2{t.Fatal("authorized operation failed",err)}
}

func TestKubernetesPolicyRejectsInvalidConfig(t *testing.T){
 cases:=[]KubernetesPolicy{
  {Binary:"kubectl",Kubeconfig:"/tmp/secret",Namespaces:map[string][]string{"apps":{"api"}},MaxReplicas:5},
  {Binary:"/usr/bin/kubectl",Kubeconfig:"/tmp/secret",Namespaces:map[string][]string{"apps":{"api","api"}},MaxReplicas:5},
  {Binary:"/usr/bin/kubectl",Kubeconfig:"/tmp/secret",Namespaces:map[string][]string{"apps":{"api"}},MaxReplicas:0},
  {Binary:"/usr/bin/kubectl",Kubeconfig:"/tmp/secret",Namespaces:map[string][]string{"apps":{"api"}},MaxReplicas:1001},
 }
 for _,p:=range cases{if p.validate()==nil{t.Fatalf("invalid policy accepted: %+v",p)}}
}
