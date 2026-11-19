package main

import (
 "fmt"
 "os"
 "path/filepath"
 "strings"
 "testing"
)

const sampleWorkspace="0123456789abcdef0123456789abcdef"

func TestStableUSBAliasSelection(t *testing.T){
 cases:=[]struct{alias string;block,valid bool}{
  {"/dev/disk/by-id/usb-Model_123",true,true},
  {"/dev/serial/by-id/usb-ACME_99",false,true},
  {"/dev/bus/usb/002/013",false,true},
  {"/dev/sda",false,false},
  {"/dev/disk/by-id/ata-system",false,false},
  {"/dev/disk/by-id/usb-Disk-part1",true,false},
  {"/dev/serial/by-id/usb-ACME/../../etc/passwd",false,false},
  {"/dev/bus/usb/2/3",false,false},
  {"/dev/bus/usb/001/002/003",false,false},
 }
 for _,item:=range cases{
  block,valid:=approvedUSBAlias(item.alias)
  if block!=item.block||valid!=item.valid{
   t.Errorf("alias %q mapped to block=%t allowed=%t",item.alias,block,valid)
  }
 }
}

func TestDeviceIdentityAndBusAncestry(t *testing.T){
 maj,min:=deviceNumbers(0x0103)
 if maj!=1||min!=3{t.Fatalf("Linux device number was not decoded: %d:%d",maj,min)}
 if !usbAncestry("/sys/devices/pci0000:00/0000:00:14.0/usb1/1-2/1-2:1.0") {
  t.Fatal("valid USB kernel ancestry was refused")
 }
 for _,value:=range []string{"/sys/devices/platform/usbnot/","/sys/devices/pci0000:00","/sys/devices/my_usb2"}{
  if usbAncestry(value){t.Fatalf("unrelated sysfs path admitted: %s",value)}
 }
}

func TestDeviceKeysPreserveRustCustodyLayout(t *testing.T){
 if !validWorkspace(sampleWorkspace)||validWorkspace("../bad")||
  validWorkspace(strings.ToUpper(sampleWorkspace)){
  t.Fatal("invalid workspace ID classification")
 }
 if got:=custodyKey("usb","/dev/bus/usb/001/002");len(got)!=22||!strings.HasSuffix(got,".lease") {
  t.Fatalf("legacy custody key is not retained: %q",got)
 }
 if custodyKey("gpu","/dev/dri/renderD128")==custodyKey("npu","/dev/dri/renderD128") {
  t.Fatal("separate accelerator classes share an alias lease")
 }
 if physicalKey("usb","/dev/serial/by-id/usb-A",259)!=physicalKey("gpu","/dev/dri/renderD128",259){
  t.Fatal("character aliases do not share their physical reservation")
 }
 if physicalKey("usb","/dev/disk/by-id/usb-A",259)==physicalKey("gpu","/dev/dri/renderD128",259){
  t.Fatal("block and character reservations collide")
 }
}

func TestCustodyRecordIsStableAndBindsInode(t *testing.T){
 id:=deviceIdentity{Path:"/dev/dri/renderD128",Group:50,Rdev:259,Dev:22,Ino:998}
 data:=custodyContents(sampleWorkspace,"gpu","/dev/dri/renderD128",id)
 expected:=fmt.Sprintf("v2\n%s\ngpu\n/dev/dri/renderD128\n/dev/dri/renderD128\n259\n22\n998\n",sampleWorkspace)
 if data!=expected{t.Fatalf("record layout changed: %q",data)}
 derived,err:=physicalFromRecord("gpu","/dev/dri/renderD128",data)
 if err!=nil||derived!="char-0000000000000103.physical"{
  t.Fatalf("wrong physical key %q: %v",derived,err)
 }
 changed:=id;changed.Ino++
 if custodyContents(sampleWorkspace,"gpu","/dev/dri/renderD128",changed)==data {
  t.Fatal("replaced device inode inherited a prior reservation")
 }
 invalid:=[]string{
  strings.Replace(data,"v2\n","v1\n",1),
  strings.Replace(data,"\n998\n","\n",1),
  strings.Replace(data,"\n998\n","\ninvalid\n",1),
  strings.Replace(data,"\ngpu\n","\nnpu\n",1),
 }
 for i,body:=range invalid{
  if _,err:=physicalFromRecord("gpu","/dev/dri/renderD128",body);err==nil{
   t.Errorf("invalid record variant %d was accepted",i)
  }
 }
}

func TestPrivateCustodyFilesRejectLinksAndWorldAccess(t *testing.T){
 dir:=t.TempDir()
 path:=filepath.Join(dir,"identity.lease")
 if err:=os.WriteFile(path,[]byte("private custody"),0600);err!=nil{t.Fatal(err)}
 if value,err:=privateRecord(path);err!=nil||value!="private custody"{
  t.Fatalf("private record rejected: %q %v",value,err)
 }
 if err:=os.Chmod(path,0644);err!=nil{t.Fatal(err)}
 if _,err:=privateRecord(path);err==nil{t.Fatal("world-readable custody record accepted")}
 if err:=os.Chmod(path,0600);err!=nil{t.Fatal(err)}
 link:=filepath.Join(dir,"alias.lease")
 if err:=os.Symlink(path,link);err!=nil{t.Fatal(err)}
 if _,err:=privateRecord(link);err==nil{t.Fatal("symlink custody record accepted")}
 extra:=filepath.Join(dir,"hardlink.lease")
 if err:=os.Link(path,extra);err!=nil{t.Fatal(err)}
 if _,err:=privateRecord(path);err==nil{t.Fatal("multiply-linked custody record accepted")}
}

func TestUnsupportedDeviceCallsFailWithoutMakingLeases(t *testing.T){
 if _,err:=acquireDevice("unknown","/dev/null",sampleWorkspace);err==nil{
  t.Fatal("unsupported hardware class passed admission")
 }
 if _,err:=acquireDevice("usb","/dev/bus/usb/001/002",sampleWorkspace);err==nil{
  t.Fatal("reassignable bus path admitted as stable USB lease")
 }
 if err:=releaseDevice("gpu","/dev/dri/renderD128","invalid");err==nil{
  t.Fatal("invalid workspace released custody")
 }
 if err:=containerAbsent(sampleWorkspace);err==nil {
  t.Fatal("unconfigured Docker inspector allowed device release")
 }
 if _,err:=mapChar("other","/dev/null");err==nil{
  t.Fatal("unsupported accelerator device class allowed")
 }
}

func TestCLIProtocolAndOutputAreStrict(t *testing.T){
 for _,args:=range [][]string{
  {},{"map-usb"},{"custody-v1","release-device","gpu","/dev/null","bad"},
  {"custody-v1","delete-all",sampleWorkspace},
 }{
  if _,err:=dispatch(args);err==nil{
   t.Fatalf("unsupported command admitted: %q",args)
  }
 }
}
