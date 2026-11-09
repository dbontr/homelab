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
 "github.com/dbontr/homelab-new/system/security"
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
 vault,err:=security.NewVaultClientFromEnv("HOMELAB_ENGINEERING")
 if err!=nil{log.Fatal("engineering vault trust: ",err)}
 computeAlias:=os.Getenv("HOMELAB_ENGINEERING_COMPUTE_VAULT_ALIAS")
 computeFile:=os.Getenv("HOMELAB_COMPUTE_TOKEN_FILE")
 if (computeAlias=="")==(computeFile==""){log.Fatal("engineering compute requires exactly one credential authority")}
 var compute *agentflow.ComputeClient
 if computeAlias!=""{
  if vault==nil{log.Fatal("engineering compute vault is unavailable")}
  source,sourceErr:=vault.TokenSource(computeAlias)
  if sourceErr!=nil{log.Fatal(sourceErr)}
  compute,err=agentflow.NewComputeClientFromSource(required("HOMELAB_COMPUTE_URL"),source)
 }else{
  compute,err=agentflow.NewComputeClientFromFile(required("HOMELAB_COMPUTE_URL"),computeFile)
 }
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
  modelAlias:=os.Getenv("HOMELAB_ENGINEERING_MODEL_VAULT_ALIAS")
  modelFile:=os.Getenv("HOMELAB_ENGINEERING_MODEL_TOKEN_FILE")
  if modelAlias!=""&&modelFile!=""{log.Fatal("conflicting engineering model credential authorities")}
  var source func()(string,error)
  if modelAlias!=""{
   if vault==nil{log.Fatal("engineering model vault unavailable")}
   source,err=vault.TokenSource(modelAlias)
   if err!=nil{log.Fatal(err)}
  }else if modelFile!=""{
   key,err=credential(modelFile)
   if err!=nil{log.Fatal(err)}
  }
  model,modelErr:=agentflow.NewModelAuthor(endpoint,required("HOMELAB_ENGINEERING_MODEL_NAME"),key)
  if modelErr!=nil{log.Fatal(modelErr)}
  model.TokenSource=source
  author=model
 }else if os.Getenv("HOMELAB_ENGINEERING_MODEL_VAULT_ALIAS")!=""||
  os.Getenv("HOMELAB_ENGINEERING_MODEL_TOKEN_FILE")!=""{
  log.Fatal("engineering model credential requires model endpoint")
 }
 engine:=&agentflow.Engine{Store:store,Compute:compute,Author:author,AllowedProjects:projects}
 ctx,stop:=signal.NotifyContext(context.Background(),syscall.SIGINT,syscall.SIGTERM)
 defer stop()
 go engine.Serve(ctx)
 if err:=agentflow.ServeUnix(ctx,required("HOMELAB_ENGINEERING_SOCKET"),engine);err!=nil{log.Fatal(err)}
}
