package remote

import (
 "context"
 "errors"
 "fmt"
 "os"
 "os/exec"
 "path/filepath"
 "strconv"
 "strings"

 "github.com/dbontr/homelab-new/system/devices"
)

// Inspection probes do not install agents, change host configuration or elevate privileges.
var probes=map[string]string{
 "kernel":"uname -a",
 "cpu":"cat /proc/cpuinfo",
 "memory":"cat /proc/meminfo",
 "storage":"df -h",
 "network":"ip -brief address",
}

// SSHArguments builds an ordinary-user inspection with strict host-key checking.
func SSHArguments(catalog *devices.Catalog,deviceID,probe,knownHosts string)([]string,error){
 command,ok:=probes[probe]
 if !ok{return nil,errors.New("unsupported read-only SSH probe")}
 if !filepath.IsAbs(knownHosts)||filepath.Clean(knownHosts)!=knownHosts{
   return nil,errors.New("a trusted absolute known-hosts file is required")
 }
 info,err:=os.Lstat(knownHosts)
 if err!=nil||!info.Mode().IsRegular()||info.Mode().Perm()&0022!=0{return nil,errors.New("trusted host-key inventory is unavailable")}
 if catalog==nil{return nil,errors.New("device inventory is required")}
 for _,d:=range catalog.List() {
  if d.ID!=deviceID{continue}
  if d.Mode!=devices.ModeSSH||d.SSH==nil||d.Connection!="ssh"||d.EligibleForCompute{
    return nil,errors.New("device is not authorized for agentless SSH")
  }
  permitted:=false
  for _,op:=range d.AllowedOperations{if op=="inspect"{permitted=true}}
  if !permitted{return nil,errors.New("SSH inspection is not authorized")}
  if d.SSH.User=="root"||strings.ContainsAny(d.SSH.Host," \n\t"){return nil,errors.New("invalid SSH target")}
  return []string{
    "-F","/dev/null","-o","BatchMode=yes","-o","StrictHostKeyChecking=yes",
    "-o","UserKnownHostsFile="+knownHosts,"-o","GlobalKnownHostsFile=/dev/null",
    "-o","ClearAllForwardings=yes","-o","ForwardAgent=no",
    "-o","PermitLocalCommand=no","-o","ControlMaster=no","-o","ConnectTimeout=10",
    "-T","-p",strconv.Itoa(d.SSH.Port),"--",d.SSH.User+"@"+d.SSH.Host,command,
  },nil
 }
 return nil,errors.New("SSH device is not registered")
}

func Inspect(ctx context.Context,catalog *devices.Catalog,deviceID,probe,knownHosts string)(string,error){
 args,err:=SSHArguments(catalog,deviceID,probe,knownHosts);if err!=nil{return "",err}
 output,err:=exec.CommandContext(ctx,"ssh",args...).CombinedOutput()
 if len(output)>1<<20{return "",errors.New("SSH inspection output exceeds limit")}
 if err!=nil{return "",fmt.Errorf("SSH inspection failed: %w",err)}
 return string(output),nil
}
