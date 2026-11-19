package main

import (
 "context"
 "errors"
 "fmt"
 "io"
 "os"
 "os/exec"
 "path/filepath"
 "strconv"
 "strings"
 "syscall"
 "time"
)

var lsblkBinary string // Immutable Nix-store binary, injected at link time.

type deviceIdentity struct {
 Path string
 Group uint32
 Rdev uint64
 Dev uint64
 Ino uint64
}

func safeAliasComponent(value string) bool {
 if value=="" {return false}
 for _,ch:=range []byte(value) {
  if !(ch>='0'&&ch<='9'||ch>='a'&&ch<='z'||ch>='A'&&ch<='Z'||
   ch=='.'||ch=='_'||ch==':'||ch=='+'||ch=='-') {return false}
 }
 return true
}
func approvedUSBAlias(path string)(block bool,ok bool){
 switch{
 case strings.HasPrefix(path,"/dev/disk/by-id/usb-"):
  tail:=strings.TrimPrefix(path,"/dev/disk/by-id/usb-")
  return true,safeAliasComponent(tail)&&!strings.Contains(tail,"-part")
 case strings.HasPrefix(path,"/dev/serial/by-id/"):
  return false,safeAliasComponent(strings.TrimPrefix(path,"/dev/serial/by-id/"))
 case strings.HasPrefix(path,"/dev/bus/usb/"):
  parts:=strings.Split(strings.TrimPrefix(path,"/dev/bus/usb/"),"/")
  if len(parts)!=2{return false,false}
  for _,part:=range parts{
   if len(part)!=3{return false,false}
   for _,c:=range part{if c<'0'||c>'9'{return false,false}}
  }
  return false,true
 default:return false,false
 }
}

func statIdentity(path string)(deviceIdentity,error){
 info,err:=os.Stat(path)
 if err!=nil{return deviceIdentity{},err}
 st,ok:=info.Sys().(*syscall.Stat_t)
 if !ok{return deviceIdentity{},errors.New("unsupported device metadata")}
 return deviceIdentity{Path:path,Group:st.Gid,Rdev:uint64(st.Rdev),Dev:uint64(st.Dev),Ino:st.Ino},nil
}

func deviceNumbers(n uint64)(uint64,uint64){
 major:=((n>>8)&0xfff)|((n>>32)&^uint64(0xfff))
 minor:=(n&0xff)|((n>>12)&0xffffff00)
 return major,minor
}

func usbAncestry(path string)bool{
 for _,part:=range strings.Split(filepath.Clean(path),string(filepath.Separator)){
  if !strings.HasPrefix(part,"usb")||len(part)<=3{continue}
  valid:=true
  for _,c:=range part[3:]{if c<'0'||c>'9'{valid=false;break}}
  if valid{return true}
 }
 return false
}

func mapUSB(alias string)(deviceIdentity,error){
 block,ok:=approvedUSBAlias(alias)
 if !ok{return deviceIdentity{},errors.New("USB alias is outside approved device classes")}
 path,err:=filepath.EvalSymlinks(alias)
 if err!=nil{return deviceIdentity{},errors.New("USB alias unavailable")}
 if !strings.HasPrefix(path,"/dev/")||filepath.Clean(path)!=path{
  return deviceIdentity{},errors.New("USB alias escaped device root")
 }
 for _,c:=range []byte(path) {
  if !(c>='0'&&c<='9'||c>='a'&&c<='z'||c>='A'&&c<='Z'||
   c=='/'||c=='_'||c=='.'||c=='-'||c==':') {
   return deviceIdentity{},errors.New("USB device path has invalid characters")
  }
 }
 info,err:=os.Stat(path)
 if err!=nil{return deviceIdentity{},errors.New("USB device unavailable")}
 if block {
  if info.Mode()&os.ModeDevice==0||info.Mode()&os.ModeCharDevice!=0{
   return deviceIdentity{},errors.New("USB whole-disk alias is not a block device")
  }
 }else if info.Mode()&os.ModeDevice==0||info.Mode()&os.ModeCharDevice==0{
  return deviceIdentity{},errors.New("USB alias is not a character device")
 }
 identity,err:=statIdentity(path)
 if err!=nil{return deviceIdentity{},err}
 major,minor:=deviceNumbers(identity.Rdev)
 class:="char"
 if block{class="block"}
 sysfs,err:=filepath.EvalSymlinks(fmt.Sprintf("/sys/dev/%s/%d:%d",class,major,minor))
 if err!=nil||!usbAncestry(sysfs){
  return deviceIdentity{},errors.New("device has no verified USB sysfs ancestry")
 }
 if block {
  if err:=ensureWholeDiskUnused(path);err!=nil{return deviceIdentity{},err}
 }
 return identity,nil
}

func inspectDisk(path string,flags ...string)(string,error){
 if !filepath.IsAbs(lsblkBinary)||!strings.HasPrefix(lsblkBinary,"/nix/store/"){
  return "",errors.New("trusted disk inspection binary is not packaged")
 }
 ctx,cancel:=context.WithTimeout(context.Background(),10*time.Second)
 defer cancel()
 args:=append(append([]string{},flags...),path)
 cmd:=exec.CommandContext(ctx,lsblkBinary,args...)
 cmd.Env=[]string{"LC_ALL=C","PATH=/run/current-system/sw/bin"}
 cmd.Stdin=nil
 data,err:=cmd.Output()
 if err!=nil||len(data)>16<<10||ctx.Err()!=nil{
  return "",errors.New("USB disk inspection failed")
 }
 return string(data),nil
}
func ensureWholeDiskUnused(path string)error{
 kind,err:=inspectDisk(path,"-dnro","TYPE")
 if err!=nil{return err}
 if strings.TrimSpace(kind)!="disk"{
  return errors.New("USB mapping must be a whole disk")
 }
 types,err:=inspectDisk(path,"-nro","TYPE")
 if err!=nil{return err}
 for _,line:=range strings.Split(strings.TrimSpace(types),"\n"){
  if line!="disk"&&line!="part"{return errors.New("USB device has an active mapped child")}
 }
 mounts,err:=inspectDisk(path,"-nro","MOUNTPOINTS")
 if err!=nil{return err}
 if strings.TrimSpace(mounts)!=""{
  return errors.New("USB disk or partition is mounted")
 }
 return nil
}

func mapChar(kind,path string)(deviceIdentity,error){
 if kind!="gpu"&&kind!="npu"&&kind!="fpga"{
  return deviceIdentity{},errors.New("unknown accelerator class")
 }
 if !strings.HasPrefix(path,"/dev/")||len(path)>255||filepath.Clean(path)!=path{
  return deviceIdentity{},errors.New("invalid accelerator device path")
 }
 for _,c:=range []byte(path){
  if !(c>='0'&&c<='9'||c>='a'&&c<='z'||c>='A'&&c<='Z'||
   c=='/'||c=='.'||c=='_'||c=='-'||c==':'){
   return deviceIdentity{},errors.New("invalid accelerator device path")
  }
 }
 policy:=os.Getenv("HOMELAB_NATIVE_DEVICE_POLICY")
 if !strings.HasPrefix(policy,"/nix/store/")||filepath.Clean(policy)!=policy{
  return deviceIdentity{},errors.New("native device policy must be immutable Nix store content")
 }
 stat,err:=os.Lstat(policy)
 if err!=nil||!stat.Mode().IsRegular()||stat.Mode().Perm()&0222!=0||
  stat.Size()>64<<10{
  return deviceIdentity{},errors.New("native policy has unsafe custody")
 }
 file,err:=os.OpenFile(policy,os.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_CLOEXEC,0)
 if err!=nil{return deviceIdentity{},errors.New("native policy unavailable")}
 defer file.Close()
 raw,err:=io.ReadAll(io.LimitReader(file,64<<10+1))
 if err!=nil||len(raw)>64<<10{return deviceIdentity{},errors.New("native policy could not be read")}
 permitted:=false
 for _,line:=range strings.Split(string(raw),"\n"){
  if line==kind+" "+path{permitted=true;break}
 }
 if !permitted{return deviceIdentity{},errors.New("device is not in native operator allowlist")}
 info,err:=os.Lstat(path)
 if err!=nil||info.Mode()&os.ModeDevice==0||info.Mode()&os.ModeCharDevice==0{
  return deviceIdentity{},errors.New("approved accelerator is not a character device")
 }
 return statIdentity(path)
}

func decimalDevNumber(raw string)(uint64,error){return strconv.ParseUint(raw,10,64)}
