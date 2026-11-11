package main

import (
 "context"
 "errors"
 "log"
 "os"
 "os/signal"
 "strings"
 "syscall"
 "time"

 "github.com/dbontr/homelab-new/system/agentflow"
)

func required(name string) string {
 v:=os.Getenv(name)
 if v==""{log.Fatal("missing engineering configuration: ",name)}
 return v
}
func credential(file string)(string,error){
 if file==""{return "",errors.New("missing protected credential file")}
 data,err:=os.ReadFile(file)
 if err!=nil{return "",errors.New("cannot read protected credential")}
 token:=strings.TrimSpace(string(data))
 if len(token)<32||strings.ContainsAny(token,"\r\n"){return "",errors.New("invalid protected credential")}
 return token,nil
}
func main(){
 log.SetFlags(log.LstdFlags|log.LUTC)
 projects:=map[string]bool{}
 for _,item:=range strings.Split(required("HOMELAB_ENGINEERING_PROJECTS"),","){
  project:=strings.TrimSpace(item)
  if project!=""{projects[project]=true}
 }
 if len(projects)==0||len(projects)>32{log.Fatal("invalid engineering project scope")}
 // Autonomous engineering work executes after the initiating request has
 // finished. A short-lived human owner ticket cannot authorize unattended
 // compute or model calls. These use separately scoped service credentials.
 if os.Getenv("HOMELAB_ENGINEERING_COMPUTE_VAULT_ALIAS")!=""||
  os.Getenv("HOMELAB_ENGINEERING_MODEL_VAULT_ALIAS")!=""||
  os.Getenv("HOMELAB_ENGINEERING_VAULT_URL")!=""{
  log.Fatal("unattended engineering requires a protected service credential, not an owner-operation vault alias")
 }
 computeFile:=required("HOMELAB_COMPUTE_TOKEN_FILE")
 compute,err:=agentflow.NewComputeClientFromFile(required("HOMELAB_COMPUTE_URL"),computeFile)
 if err!=nil{log.Fatal(err)}
 probeCtx,stopProbe:=context.WithTimeout(context.Background(),10*time.Second)
 err=compute.VerifyProjectCredential(probeCtx)
 stopProbe()
 if err!=nil{log.Fatal(err)}
 for project:=range projects {
  grantCtx,cancel:=context.WithTimeout(context.Background(),10*time.Second)
  grantErr:=compute.VerifyProjectEditor(grantCtx,project)
  cancel()
  if grantErr!=nil{log.Fatalf("engineering service lacks an exact editor grant for %s: %v",project,grantErr)}
 }
 store,err:=agentflow.Open(required("HOMELAB_ENGINEERING_DIR"));if err!=nil{log.Fatal(err)}
 var author agentflow.Author
 if endpoint:=os.Getenv("HOMELAB_ENGINEERING_MODEL_URL");endpoint!=""{
  key:=""
  modelFile:=os.Getenv("HOMELAB_ENGINEERING_MODEL_TOKEN_FILE")
  if modelFile!=""{
   key,err=credential(modelFile)
   if err!=nil{log.Fatal(err)}
  }
  model,modelErr:=agentflow.NewModelAuthor(endpoint,required("HOMELAB_ENGINEERING_MODEL_NAME"),key)
  if modelErr!=nil{log.Fatal(modelErr)}
  author=model
 }else if os.Getenv("HOMELAB_ENGINEERING_MODEL_TOKEN_FILE")!=""{
  log.Fatal("engineering model credential requires model endpoint")
 }
 nativeMode:=os.Getenv("HOMELAB_ENGINEERING_NATIVE_VERIFICATION")
 if nativeMode!=""&&nativeMode!="0"&&nativeMode!="1"{log.Fatal("invalid engineering verification selection")}
 engine:=&agentflow.Engine{Store:store,Compute:compute,Author:author,AllowedProjects:projects,NativeVerification:nativeMode=="1"}
 ctx,stop:=signal.NotifyContext(context.Background(),syscall.SIGINT,syscall.SIGTERM)
 defer stop()
 go engine.Serve(ctx)
 if err:=agentflow.ServeUnix(ctx,required("HOMELAB_ENGINEERING_SOCKET"),engine);err!=nil{log.Fatal(err)}
}
