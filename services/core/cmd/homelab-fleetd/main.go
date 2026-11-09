// homelab-fleetd runs the protected release coordinator. It does not accept
// shell commands, private keys or owner factors from the MCP gateway.
package main

import (
 "context"
 "crypto/tls"
 "crypto/x509"
 "encoding/json"
 "errors"
 "log"
 "os"
 "os/signal"
 "os/user"
 "strconv"
 "syscall"

 "github.com/dbontr/homelab-new/system/release"
 "github.com/dbontr/homelab-new/system/security"
)

type hostConfig struct {
 Host string `json:"host"`
 Role string `json:"role"`
 Endpoint string `json:"endpoint"`
 ServerName string `json:"server_name"`
}

func required(name string)string{
 value:=os.Getenv(name)
 if value==""{log.Fatal("missing fleet configuration: ",name)}
 return value
}

func configured()(*release.FleetService,error){
 cert,err:=tls.LoadX509KeyPair(required("HOMELAB_FLEET_CERT"),required("HOMELAB_FLEET_KEY"))
 if err!=nil{return nil,errors.New("fleet coordinator TLS certificate unavailable")}
 rootsData,err:=os.ReadFile(required("HOMELAB_FLEET_CA"))
 if err!=nil{return nil,errors.New("fleet host trust roots unavailable")}
 roots:=x509.NewCertPool()
 if !roots.AppendCertsFromPEM(rootsData){return nil,errors.New("invalid fleet trust roots")}
 hostsData,err:=os.ReadFile(required("HOMELAB_FLEET_HOSTS_FILE"))
 if err!=nil||len(hostsData)>16384{return nil,errors.New("invalid fleet host policy")}
 var hosts []hostConfig
 if err:=json.Unmarshal(hostsData,&hosts);err!=nil||len(hosts)==0||len(hosts)>16{return nil,errors.New("invalid fleet hosts")}
 agents:=make(map[string]release.HostReleaseAgent,len(hosts))
 stagers:=make(map[string]release.FleetStageAgent,len(hosts))
 targets:=make([]release.FleetStageTarget,0,len(hosts))
 for _,host:=range hosts{
  if _,exists:=agents[host.Host];exists{return nil,errors.New("duplicate fleet host")}
  agent,err:=release.NewRemoteReleaseAgent(release.RemoteAgentConfig{
   Host:host.Host,Endpoint:host.Endpoint,ServerName:host.ServerName,
   TrustRoots:roots,Certificate:cert,AccessAssertion:security.AccessAssertion,
  })
  if err!=nil{return nil,err}
  agents[host.Host]=agent
  stagers[host.Host]=agent
  targets=append(targets,release.FleetStageTarget{Host:host.Host,Role:host.Role})
 }
 auth,err:=security.NewAdminClient(required("HOMELAB_FLEET_SECURITY_SOCKET"))
 if err!=nil{return nil,err}
 return &release.FleetService{
  Controller:&release.FleetOrchestrator{
   Fleet:&release.DurableFleet{StateFile:required("HOMELAB_FLEET_STATE"),Agents:agents},
   PlanFile:required("HOMELAB_FLEET_PLAN"),StageTargets:targets,StageAgents:stagers,
  },
  Authorizer:auth,
 },nil
}

func main(){
 log.SetFlags(log.LstdFlags|log.LUTC)
 service,err:=configured();if err!=nil{log.Fatal(err)}
 group,err:=user.LookupGroup("homelab-fleet-client");if err!=nil{log.Fatal(err)}
 gid,err:=strconv.Atoi(group.Gid);if err!=nil||gid<=0{log.Fatal("invalid fleet client group")}
 ctx,stop:=signal.NotifyContext(context.Background(),syscall.SIGINT,syscall.SIGTERM)
 defer stop()
 log.Print("restricted fleet coordination socket starting")
 if err:=service.ServeSocket(ctx,"/run/homelab-fleetd/fleet.sock",gid);err!=nil{log.Fatal(err)}
}
