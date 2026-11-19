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

// The build pins these executables to immutable Nix-store paths. CLI
// arguments and workspace files cannot replace an administrative executable.
var (
 gitBinary string
 bubblewrapBinary string
 goBinary string
 nixBinary string
)

func hexID(s string,lengths ...int)bool{
 lengthOK:=false
 for _,n:=range lengths {if len(s)==n{lengthOK=true}}
 if !lengthOK{return false}
 for _,c:=range []byte(s){
  if !(c>='0'&&c<='9'||c>='a'&&c<='f'){return false}
 }
 return true
}

func immutableTool(path string)error{
 if !strings.HasPrefix(path,"/nix/store/")||filepath.Clean(path)!=path{
  return errors.New("sandbox executable must be a fixed immutable store path")
 }
 info,err:=os.Stat(path)
 if err!=nil||!info.Mode().IsRegular()||info.Mode().Perm()&0022!=0{
  return errors.New("sandbox executable has unsafe custody")
 }
 return nil
}
func gitRevision(source string)(string,error){
 if err:=immutableTool(gitBinary);err!=nil{return "",err}
 ctx,cancel:=context.WithTimeout(context.Background(),15*time.Second)
 defer cancel()
 cmd:=exec.CommandContext(ctx,gitBinary,"--no-optional-locks","-C",source,"rev-parse","--verify","HEAD")
 cmd.Env=[]string{"HOME=/tmp","GIT_CONFIG_NOSYSTEM=1","LC_ALL=C"}
 cmd.Stdin=nil
 output,err:=cmd.Output()
 if err!=nil||len(output)>100||ctx.Err()!=nil{return "",errors.New("Git revision verification failed")}
 value:=strings.TrimSpace(string(output))
 if !hexID(value,40,64){return "",errors.New("invalid source commit")}
 return value,nil
}
func workspacePath(id string)(string,error){
 if !hexID(id,32){return "",errors.New("invalid workspace identity")}
 root:=os.Getenv("HOMELAB_SANDBOX_WORKSPACES")
 if root==""{return "",errors.New("workspace root unavailable")}
 resolved,err:=filepath.EvalSymlinks(root)
 if err!=nil||resolved=="/"{return "",errors.New("untrusted workspace root")}
 base:=filepath.Join(resolved,id)
 stat,err:=os.Lstat(base)
 if err!=nil||!stat.IsDir()||stat.Mode().Perm()&0002!=0{return "",errors.New("workspace directory unavailable")}
 actual,err:=filepath.EvalSymlinks(base)
 if err!=nil||filepath.Dir(actual)!=resolved||actual!=base {
  return "",errors.New("workspace directory escaped its root")
 }
 return actual,nil
}

type toolSpec struct {
 executable string
 args []string
}
func localAction(action string)(toolSpec,error){
 switch action {
 case "go-test":return toolSpec{goBinary,[]string{"test","-count=1","./..."}},nil
 case "go-vet":return toolSpec{goBinary,[]string{"vet","./..."}},nil
 case "nix-check":return toolSpec{nixBinary,[]string{"flake","check","--offline","--no-build"}},nil
 default:return toolSpec{},errors.New("unregistered Engineering verification action")
 }
}

func limitedRun(ctx context.Context,cmd *exec.Cmd)error{
 err:=cmd.Run()
 if ctx.Err()!=nil{return errors.New("sandbox verification timed out")}
 if err!=nil{return errors.New("sandbox verification failed")}
 return nil
}

func runLocal(id,revision,action string)error{
 if os.Geteuid()==0{return errors.New("Engineering verification cannot run as root")}
 if !hexID(revision,40,64){return errors.New("invalid source revision")}
 source,err:=workspacePath(id)
 if err!=nil{return err}
 before,err:=os.Stat(source)
 if err!=nil{return err}
 current,err:=gitRevision(source)
 if err!=nil||current!=revision{return errors.New("workspace revision is stale")}
 selected,err:=localAction(action)
 if err!=nil{return err}
 for _,tool:=range []string{bubblewrapBinary,selected.executable}{
  if err:=immutableTool(tool);err!=nil{return err}
 }
 deadline:=300*time.Second
 if s:=os.Getenv("HOMELAB_SANDBOX_TIMEOUT_SECONDS");s!=""{
  value,e:=strconv.ParseUint(s,10,32)
  if e==nil&&value>=1&&value<=1800{deadline=time.Duration(value)*time.Second}
 }
 ctx,cancel:=context.WithTimeout(context.Background(),deadline)
 defer cancel()
 args:=[]string{
  "--unshare-all","--die-with-parent","--new-session",
  "--ro-bind","/nix/store","/nix/store",
  "--ro-bind",source,"/workspace",
  "--proc","/proc","--dev","/dev","--tmpfs","/tmp","--dir","/home",
  "--chdir","/workspace","--setenv","HOME","/tmp",
  "--setenv","TMPDIR","/tmp","--setenv","GOCACHE","/tmp/go-cache",
  "--setenv","GOPATH","/tmp/go","--setenv","GOPROXY","off",
  "--setenv","GOSUMDB","off","--setenv","GOTOOLCHAIN","local",
  "--setenv","LC_ALL","C","--",
  selected.executable,
 }
 args=append(args,selected.args...)
 cmd:=exec.CommandContext(ctx,bubblewrapBinary,args...)
 cmd.Env=[]string{"LC_ALL=C"}
 cmd.Stdin=nil
 cmd.Stdout=io.Discard
 cmd.Stderr=io.Discard
 after,err:=os.Stat(source)
 if err!=nil||!os.SameFile(before,after){
  return errors.New("workspace was replaced")
 }
 if err:=limitedRun(ctx,cmd);err!=nil{return err}
 current,err=gitRevision(source)
 if err!=nil||current!=revision{return errors.New("workspace revision changed during verification")}
 fmt.Println("completed")
 return nil
}

func validRepoName(s string)bool{
 if s==""||len(s)>100||s=="."||s==".."||
  s[0]=='.'||s[len(s)-1]=='.'{return false}
 for _,ch:=range []byte(s){
  if !(ch>='0'&&ch<='9'||ch>='a'&&ch<='z'||ch>='A'&&ch<='Z'||
   ch=='.'||ch=='_'||ch=='-'){return false}
 }
 return true
}

func containedRepo(repository string)(string,error){
 fields:=strings.Split(repository,"/")
 if len(fields)!=2||!validRepoName(fields[0])||!validRepoName(fields[1]){
  return "",errors.New("repository must be a canonical owner/name")
 }
 root:="/workspace/repos"
 info,err:=os.Lstat(root)
 if err!=nil||!info.IsDir(){return "",errors.New("repository root unavailable")}
 source:=filepath.Join(root,fields[0]+"__"+fields[1])
 current,err:=os.Lstat(source)
 if err!=nil||!current.IsDir()||current.Mode().Perm()&0002!=0{
  return "",errors.New("repository checkout has unsafe custody")
 }
 resolved,err:=filepath.EvalSymlinks(source)
 if err!=nil||resolved!=source{return "",errors.New("repository checkout was redirected")}
 return source,nil
}

func verifyReceipt(source,repository,revision string)error{
 marker:=filepath.Join(source,".pwm-source-revision")
 info,err:=os.Lstat(marker)
 if err!=nil||!info.Mode().IsRegular()||info.Size()>256{
  return errors.New("source import receipt unavailable")
 }
 f,err:=os.OpenFile(marker,os.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_CLOEXEC,0)
 if err!=nil{return errors.New("unsafe source import receipt")}
 defer f.Close()
 body,err:=io.ReadAll(io.LimitReader(f,257))
 if err!=nil||len(body)>256||string(body)!=repository+"\n"+revision+"\n"{
  return errors.New("source import receipt differs from approved Git identity")
 }
 return nil
}

func containedAction(action string)(toolSpec,error){
 switch action {
 case "go-test":return toolSpec{"/usr/local/go/bin/go",[]string{"test","-count=1","./..."}},nil
 case "go-vet":return toolSpec{"/usr/local/go/bin/go",[]string{"vet","./..."}},nil
 default:return toolSpec{},errors.New("action is not approved for isolated source verification")
 }
}

func runContained(repository,revision,action string)error{
 if os.Geteuid()==0{return errors.New("isolated Engineering verification must not run as root")}
 if !hexID(revision,40,64){return errors.New("invalid expected source revision")}
 source,err:=containedRepo(repository)
 if err!=nil{return err}
 if err:=verifyReceipt(source,repository,revision);err!=nil{return err}
 before,err:=os.Stat(source)
 if err!=nil{return err}
 selected,err:=containedAction(action)
 if err!=nil{return err}
 ctx,cancel:=context.WithTimeout(context.Background(),600*time.Second)
 defer cancel()
 cmd:=exec.CommandContext(ctx,selected.executable,selected.args...)
 cmd.Dir=source
 cmd.Env=[]string{
  "HOME=/tmp","TMPDIR=/tmp","GOENV=off","GOPATH=/tmp/go",
  "GOCACHE=/tmp/go-cache","GOPROXY=off","GOSUMDB=off",
  "GOTOOLCHAIN=local","LC_ALL=C","PATH=/usr/local/go/bin:/usr/bin:/bin",
 }
 cmd.Stdin=nil
 cmd.Stdout=io.Discard
 cmd.Stderr=io.Discard
 after,err:=os.Stat(source)
 if err!=nil||!os.SameFile(before,after){return errors.New("source checkout changed identity")}
 if err:=limitedRun(ctx,cmd);err!=nil{return err}
 if err:=verifyReceipt(source,repository,revision);err!=nil{return err}
 fmt.Println("completed")
 return nil
}

func run(args []string)error{
 if len(args)!=4{return errors.New("usage: homelab-engineering-sandbox run WORKSPACE_ID REVISION ACTION")}
 switch args[0] {
 case "run":return runLocal(args[1],args[2],args[3])
 case "run-contained":return runContained(args[1],args[2],args[3])
 default:return errors.New("unregistered Engineering verification mode")
 }
}

func main(){
 if err:=run(os.Args[1:]);err!=nil{
  fmt.Fprintln(os.Stderr,err)
  os.Exit(1)
 }
}
