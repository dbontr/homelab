package privileged

import (
 "encoding/json"
 "errors"
 "path/filepath"
 "regexp"
 "strconv"
 "strings"
)

// KubernetesPolicy restricts infrastructure mutations to an operator-managed
// set of namespaces and deployments. No model-supplied command is executed.
type KubernetesPolicy struct {
 Binary string
 Kubeconfig string
 Namespaces map[string][]string
 MaxReplicas int
}

var kubeName=regexp.MustCompile(`^[a-z0-9]([-a-z0-9]*[a-z0-9])?$`)

func validKubeName(value string) bool {
 return len(value)>0&&len(value)<=63&&kubeName.MatchString(value)
}

func (p *KubernetesPolicy) validate() error {
 if p==nil{return nil}
 if !filepath.IsAbs(p.Binary)||!filepath.IsAbs(p.Kubeconfig)||
  p.MaxReplicas<1||p.MaxReplicas>1000||len(p.Namespaces)==0||len(p.Namespaces)>64{
  return errors.New("invalid Kubernetes executor policy")
 }
 for namespace,names:=range p.Namespaces {
  if !validKubeName(namespace)||len(names)==0||len(names)>128{
   return errors.New("invalid Kubernetes namespace allowlist")
  }
  seen:=map[string]bool{}
  for _,name:=range names {
   if !validKubeName(name)||seen[name]{
    return errors.New("invalid Kubernetes deployment allowlist")
   }
   seen[name]=true
  }
 }
 return nil
}

func (p *KubernetesPolicy) allowed(namespace,deployment string) bool {
 if p==nil||!validKubeName(namespace)||!validKubeName(deployment){return false}
 for _,name:=range p.Namespaces[namespace]{
  if name==deployment{return true}
 }
 return false
}

// command returns an immutable binary path and validated argv for one action.
// The kubeconfig is supplied only by the executor's systemd credential directory.
func (p *KubernetesPolicy) command(operation string,fields map[string]any)(string,[]string,error) {
 if p==nil{return "",nil,errors.New("Kubernetes administration is disabled")}
 namespace,nOK:=fields["namespace"].(string)
 deployment,dOK:=fields["deployment"].(string)
 if !nOK||!dOK||!p.allowed(namespace,deployment){
  return "",nil,errors.New("Kubernetes resource is not permitted")
 }
 base:=[]string{"--kubeconfig="+p.Kubeconfig,"--namespace="+namespace,"--request-timeout=15s"}
 resource:="deployment/"+deployment
 switch operation{
 case "cluster__deployment_restart":
  if len(fields)!=3{return "",nil,errors.New("unexpected Kubernetes arguments")}
  return p.Binary,append(base,"rollout","restart",resource),nil
 case "cluster__deployment_scale":
  if len(fields)!=4{return "",nil,errors.New("unexpected Kubernetes arguments")}
  raw,ok:=fields["replicas"].(json.Number)
  if !ok{return "",nil,errors.New("replica count must be an integer")}
  count,err:=strconv.Atoi(raw.String())
  if err!=nil||count<0||count>p.MaxReplicas||strings.Contains(raw.String(),"."){
   return "",nil,errors.New("replica count outside policy")
  }
  return p.Binary,append(base,"scale",resource,"--replicas="+strconv.Itoa(count)),nil
 default:
  return "",nil,errors.New("unknown Kubernetes action")
 }
}
