package transport

import (
 "strings"
 "testing"

 "github.com/dbontr/homelab-new/system/release"
)

func TestFleetReleaseMCPToolsAreUnavailableWithoutProtectedCoordinator(t *testing.T){
 server:=&Server{}
 containsFleet:=func()map[string]bool{
  names:=map[string]bool{}
  for _,item:=range server.localTools(){
   name,ok:=item["name"].(string)
   if ok&&strings.HasPrefix(name,"infrastructure__fleet_"){names[name]=true}
  }
  return names
 }
 if names:=containsFleet();len(names)!=0{t.Fatalf("unconfigured fleet tools leaked: %v",names)}
 // A protected coordinator socket is represented by the only internal client
 // type. Its use still requires signed independent owner authorization.
 server.fleetClient=&release.FleetClient{}
 names:=containsFleet()
 for _,name:=range []string{
  "infrastructure__fleet_stage",
  "infrastructure__fleet_plan",
  "infrastructure__fleet_prepare",
  "infrastructure__fleet_activate",
  "infrastructure__fleet_status",
  "infrastructure__fleet_recover",
  "infrastructure__fleet_clear",
 }{
  if !names[name]{t.Fatalf("protected fleet tool %s missing",name)}
 }
 if len(names)!=7{t.Fatalf("unexpected fleet release surface: %v",names)}
}
