package main

import (
 "context"
 "errors"
 "fmt"
 "hash/fnv"
 "io"
 "os"
 "os/exec"
 "path/filepath"
 "sort"
 "strconv"
 "strings"
 "syscall"
 "time"
)

var dockerBinary string // Immutable Nix-store executable, injected at link time.

const (
 custodyVersion = "v2"
 defaultCustodyDirectory = "/var/lib/homelab-device-leases"
 runtimeCustodyDirectory = "/run/homelab-device-leases"
)

func validWorkspace(id string)bool{
 if len(id)!=32{return false}
 for _,c:=range []byte(id){if !(c>='0'&&c<='9'||c>='a'&&c<='f'){return false}}
 return true
}

func custodyDirectory()(string,error){
 dir:=os.Getenv("HOMELAB_NATIVE_CUSTODY_DIR")
 if dir==""{dir=defaultCustodyDirectory}
 if dir!=defaultCustodyDirectory&&dir!=runtimeCustodyDirectory{
  return "",errors.New("device custody requires an approved private directory")
 }
 st,err:=os.Lstat(dir)
 if err!=nil||!st.IsDir()||st.Mode().Perm()&0077!=0{
  return "",errors.New("device custody directory is missing or insecure")
 }
 sys,ok:=st.Sys().(*syscall.Stat_t)
 if !ok||sys.Uid!=uint32(os.Geteuid()) {
  return "",errors.New("device custody directory owner is incorrect")
 }
 resolved,err:=filepath.EvalSymlinks(dir)
 if err!=nil||resolved!=dir{return "",errors.New("device custody directory must not be a symlink")}
 return dir,nil
}

func custodyKey(kind,alias string)string{
 h:=fnv.New64a()
 _,_=h.Write([]byte(kind))
 _,_=h.Write([]byte{0})
 _,_=h.Write([]byte(alias))
 return fmt.Sprintf("%016x.lease",h.Sum64())
}

func physicalKey(kind,alias string,dev uint64)string{
 class:="char"
 if kind=="usb"&&strings.HasPrefix(alias,"/dev/disk/by-id/usb-"){class="block"}
 return fmt.Sprintf("%s-%016x.physical",class,dev)
}

func custodyContents(workspace,kind,alias string,identity deviceIdentity)string{
 return fmt.Sprintf("%s\n%s\n%s\n%s\n%s\n%d\n%d\n%d\n",
  custodyVersion,workspace,kind,alias,identity.Path,
  identity.Rdev,identity.Dev,identity.Ino)
}

func recordRows(content string)([]string,error){
 if !strings.HasSuffix(content,"\n"){return nil,errors.New("custody record has no line terminator")}
 rows:=strings.Split(strings.TrimSuffix(content,"\n"),"\n")
 if len(rows)!=8||rows[0]!=custodyVersion||!validWorkspace(rows[1]){
  return nil,errors.New("custody record version or owner is invalid")
 }
 for _,row:=range rows{
  if row==""||strings.ContainsAny(row,"\x00\r\n"){return nil,errors.New("custody record contains an invalid field")}
 }
 for i:=5;i<=7;i++{
  if _,err:=strconv.ParseUint(rows[i],10,64);err!=nil{
   return nil,errors.New("custody kernel identity is invalid")
  }
 }
 return rows,nil
}

func physicalFromRecord(kind,alias,content string)(string,error){
 rows,err:=recordRows(content)
 if err!=nil{return "",err}
 if rows[2]!=kind||rows[3]!=alias{
  return "",errors.New("custody record device identity is inconsistent")
 }
 n,err:=strconv.ParseUint(rows[5],10,64)
 if err!=nil{return "",err}
 return physicalKey(kind,alias,n),nil
}

func privateRecord(path string)(string,error){
 named,err:=os.Lstat(path)
 if err!=nil||!named.Mode().IsRegular()||named.Mode().Perm()&0077!=0||
  named.Size()<=0||named.Size()>4096{
  return "",errors.New("device custody record is unavailable or insecure")
 }
 f,err:=os.OpenFile(path,os.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_CLOEXEC,0)
 if err!=nil{return "",errors.New("device custody record cannot be opened safely")}
 defer f.Close()
 opened,err:=f.Stat()
 if err!=nil{return "",err}
 na,nOK:=named.Sys().(*syscall.Stat_t)
 op,oOK:=opened.Sys().(*syscall.Stat_t)
 if !nOK||!oOK||na.Nlink!=1||na.Uid!=uint32(os.Geteuid())||
  na.Dev!=op.Dev||na.Ino!=op.Ino||!opened.Mode().IsRegular()||
  opened.Mode().Perm()&0077!=0 {
  return "",errors.New("device custody record identity changed")
 }
 raw,err:=io.ReadAll(io.LimitReader(f,4097))
 if err!=nil||len(raw)==0||len(raw)>4096{
  return "",errors.New("device custody record exceeds its limit")
 }
 return string(raw),nil
}

func reserveRecord(path,content string)error{
 f,err:=os.OpenFile(path,os.O_WRONLY|os.O_CREATE|os.O_EXCL|syscall.O_NOFOLLOW,0600)
 if err!=nil{return errors.New("device is already leased or custody is unavailable")}
 defer f.Close()
 if _,err:=io.WriteString(f,content);err!=nil{return err}
 if err:=f.Sync();err!=nil{return err}
 return nil
}

func syncCustodyDirectory(dir string)error{
 f,err:=os.Open(dir)
 if err!=nil{return err}
 defer f.Close()
 return f.Sync()
}

func resolveDevice(kind,alias string)(deviceIdentity,error){
 if kind=="usb"{return mapUSB(alias)}
 if kind=="gpu"||kind=="npu"||kind=="fpga"{return mapChar(kind,alias)}
 return deviceIdentity{},errors.New("unsupported device custody class")
}

func acquireDevice(kind,alias,workspace string)(deviceIdentity,error){
 if !validWorkspace(workspace){return deviceIdentity{},errors.New("invalid workspace identity")}
 if kind=="usb"&&strings.HasPrefix(alias,"/dev/bus/usb/"){
  return deviceIdentity{},errors.New("durable USB custody requires stable by-id alias")
 }
 device,err:=resolveDevice(kind,alias)
 if err!=nil{return deviceIdentity{},err}
 dir,err:=custodyDirectory()
 if err!=nil{return deviceIdentity{},err}
 contents:=custodyContents(workspace,kind,alias,device)
 physical:=filepath.Join(dir,physicalKey(kind,alias,device.Rdev))
 key:=filepath.Join(dir,custodyKey(kind,alias))
 // Physical reservation precedes the alias. Interrupted claims stay fenced.
 if err:=reserveRecord(physical,contents);err!=nil{return deviceIdentity{},err}
 if err:=syncCustodyDirectory(dir);err!=nil{return deviceIdentity{},err}
 if err:=reserveRecord(key,contents);err!=nil{return deviceIdentity{},err}
 if err:=syncCustodyDirectory(dir);err!=nil{return deviceIdentity{},err}
 current,err:=statIdentity(device.Path)
 if err!=nil||current.Rdev!=device.Rdev||current.Dev!=device.Dev||
  current.Ino!=device.Ino {
  return deviceIdentity{},errors.New("device identity changed during custody acquisition")
 }
 return device,nil
}

func verifyDevice(kind,alias,workspace string)(deviceIdentity,error){
 if !validWorkspace(workspace){return deviceIdentity{},errors.New("invalid workspace identity")}
 device,err:=resolveDevice(kind,alias)
 if err!=nil{return deviceIdentity{},err}
 dir,err:=custodyDirectory()
 if err!=nil{return deviceIdentity{},err}
 expected:=custodyContents(workspace,kind,alias,device)
 aliasRecord,err:=privateRecord(filepath.Join(dir,custodyKey(kind,alias)))
 if err!=nil{return deviceIdentity{},err}
 physicalRecord,err:=privateRecord(filepath.Join(dir,physicalKey(kind,alias,device.Rdev)))
 if err!=nil{return deviceIdentity{},err}
 if expected!=aliasRecord||expected!=physicalRecord{
  return deviceIdentity{},errors.New("device identity differs from its exclusive custody")
 }
 return device,nil
}

type boundedOutput struct{
 data []byte
 limit int
}
func (b *boundedOutput)Write(part []byte)(int,error){
 if len(b.data)+len(part)>b.limit{return 0,errors.New("output exceeds custody limit")}
 b.data=append(b.data,part...)
 return len(part),nil
}

func containerAbsent(workspace string)error{
 if !validWorkspace(workspace){return errors.New("invalid managed workspace identity")}
 if !strings.HasPrefix(dockerBinary,"/nix/store/")||!filepath.IsAbs(dockerBinary){
  return errors.New("trusted Docker inspection binary is not packaged")
 }
 ctx,cancel:=context.WithTimeout(context.Background(),10*time.Second)
 defer cancel()
 filter:="name=^/pwm-"+workspace+"$"
 cmd:=exec.CommandContext(ctx,dockerBinary,
  "--host","unix:///var/run/docker.sock","ps","-a",
  "--filter",filter,"--format","{{.Names}}")
 cmd.Env=[]string{"LC_ALL=C","HOME=/var/empty",
  "DOCKER_HOST=unix:///var/run/docker.sock","DOCKER_CONTEXT="}
 cmd.Stdin=nil
 output:=&boundedOutput{limit:1024}
 cmd.Stdout=output
 cmd.Stderr=io.Discard
 if err:=cmd.Run();err!=nil||ctx.Err()!=nil{
  return errors.New("independent Docker custody inspection unavailable")
 }
 if strings.TrimSpace(string(output.data))!=""{
  return errors.New("managed workspace still exists in Docker")
 }
 return nil
}

func releaseDevice(kind,alias,workspace string)error{
 if !validWorkspace(workspace)||!validCustodyKind(kind){
  return errors.New("invalid device custody release identity")
 }
 dir,err:=custodyDirectory()
 if err!=nil{return err}
 if err:=containerAbsent(workspace);err!=nil{return err}
 name:=filepath.Join(dir,custodyKey(kind,alias))
 content,err:=privateRecord(name)
 if err!=nil{return err}
 rows,err:=recordRows(content)
 if err!=nil{return err}
 if rows[1]!=workspace{return errors.New("device custody belongs to another workspace")}
 physicalName,err:=physicalFromRecord(kind,alias,content)
 if err!=nil{return err}
 physical:=filepath.Join(dir,physicalName)
 physicalContent,err:=privateRecord(physical)
 if err!=nil{return err}
 if content!=physicalContent{return errors.New("physical custody differs from alias record")}
 if err:=os.Remove(name);err!=nil{return err}
 if err:=syncCustodyDirectory(dir);err!=nil{return err}
 if err:=os.Remove(physical);err!=nil{return err}
 return syncCustodyDirectory(dir)
}

func validCustodyKind(kind string)bool{
 return kind=="usb"||kind=="gpu"||kind=="npu"||kind=="fpga"
}

func recordOwner(path string)(string,error){
 content,err:=privateRecord(path)
 if err!=nil{return "",err}
 rows,err:=recordRows(content)
 if err!=nil{return "",err}
 return rows[1],nil
}

func verifyWorkspace(workspace string)(int,error){
 if !validWorkspace(workspace){return 0,errors.New("invalid workspace identity")}
 dir,err:=custodyDirectory()
 if err!=nil{return 0,err}
 entries,err:=os.ReadDir(dir)
 if err!=nil{return 0,err}
 count:=0
 for _,entry:=range entries{
  ext:=filepath.Ext(entry.Name())
  if ext!=".lease"&&ext!=".physical"{
   return 0,errors.New("unexpected device custody inventory entry")
  }
  path:=filepath.Join(dir,entry.Name())
  owner,err:=recordOwner(path)
  if err!=nil{return 0,err}
  if owner!=workspace{continue}
  contents,err:=privateRecord(path)
  if err!=nil{return 0,err}
  rows,err:=recordRows(contents)
  if err!=nil{return 0,err}
  kind,alias:=rows[2],rows[3]
  if !validCustodyKind(kind){return 0,errors.New("invalid recorded custody kind")}
  if ext==".physical"{
   physical,err:=physicalFromRecord(kind,alias,contents)
   if err!=nil||physical!=entry.Name(){
    return 0,errors.New("physical custody name does not match its record")
   }
   aliasContent,err:=privateRecord(filepath.Join(dir,custodyKey(kind,alias)))
   if err!=nil||aliasContent!=contents{
    return 0,errors.New("physical custody record has no matching alias")
   }
   continue
  }
  if entry.Name()!=custodyKey(kind,alias){
   return 0,errors.New("alias custody name does not match its record")
  }
  if _,err:=verifyDevice(kind,alias,workspace);err!=nil{return 0,err}
  count++
  if count>64{return 0,errors.New("too many device custody records")}
 }
 return count,nil
}

func releaseWorkspace(workspace string)(int,error){
 if !validWorkspace(workspace){return 0,errors.New("invalid workspace identity")}
 dir,err:=custodyDirectory()
 if err!=nil{return 0,err}
 if err:=containerAbsent(workspace);err!=nil{return 0,err}
 entries,err:=os.ReadDir(dir)
 if err!=nil{return 0,err}
 var aliasNames,physicalNames []string
 for _,entry:=range entries{
  ext:=filepath.Ext(entry.Name())
  if ext!=".lease"&&ext!=".physical"{
   return 0,errors.New("unexpected file in custody directory")
  }
  path:=filepath.Join(dir,entry.Name())
  owner,err:=recordOwner(path)
  if err!=nil{return 0,err}
  if owner!=workspace{continue}
  if ext==".lease"{aliasNames=append(aliasNames,path)}else{physicalNames=append(physicalNames,path)}
  if len(aliasNames)+len(physicalNames)>128{
   return 0,errors.New("too many device custody records")
  }
 }
 sort.Strings(aliasNames)
 sort.Strings(physicalNames)
 // Orphaned physical reservations survive interruptions; remove aliases first.
 for _,path:=range append(aliasNames,physicalNames...){
  if err:=os.Remove(path);err!=nil{return 0,err}
  if err:=syncCustodyDirectory(dir);err!=nil{return 0,err}
 }
 return len(aliasNames),nil
}
