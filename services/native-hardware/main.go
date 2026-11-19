package main

import (
 "fmt"
 "os"
)

func usage()error{
 return fmt.Errorf("usage: homelab-native-hardware <map-usb ALIAS | map-char KIND PATH | custody-v1 acquire-device KIND PATH WORKSPACE | custody-v1 verify-device KIND PATH WORKSPACE | custody-v1 release-device KIND PATH WORKSPACE | custody-v1 verify-workspace WORKSPACE | custody-v1 release-workspace WORKSPACE>")
}

func dispatch(args []string)(string,error){
 switch{
 case len(args)==2&&args[0]=="map-usb":
  device,err:=mapUSB(args[1])
  if err!=nil{return "",err}
  return fmt.Sprintf("%s\n%d\n",device.Path,device.Group),nil
 case len(args)==3&&args[0]=="map-char":
  device,err:=mapChar(args[1],args[2])
  if err!=nil{return "",err}
  return fmt.Sprintf("%s\n%d\n",device.Path,device.Group),nil
 case len(args)==5&&args[0]=="custody-v1":
  var device deviceIdentity
  var err error
  switch args[1]{
  case "acquire-device":
   device,err=acquireDevice(args[2],args[3],args[4])
  case "verify-device":
   device,err=verifyDevice(args[2],args[3],args[4])
  case "release-device":
   err=releaseDevice(args[2],args[3],args[4])
   if err==nil{return "custody-v1\nreleased\n",nil}
  default:return "",usage()
  }
  if err!=nil{return "",err}
  return fmt.Sprintf("custody-v1\n%s\n%d\n",device.Path,device.Group),nil
 case len(args)==3&&args[0]=="custody-v1":
  var count int
  var err error
  switch args[1]{
  case "verify-workspace":
   count,err=verifyWorkspace(args[2])
  case "release-workspace":
   count,err=releaseWorkspace(args[2])
  default:return "",usage()
  }
  if err!=nil{return "",err}
  return fmt.Sprintf("custody-v1\n%d\n",count),nil
 default:return "",usage()
 }
}

func main(){
 response,err:=dispatch(os.Args[1:])
 if err!=nil {
  fmt.Fprintln(os.Stderr,err)
  os.Exit(1)
 }
 fmt.Print(response)
}
