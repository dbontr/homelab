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
 token,err:=credential(required("HOMELAB_COMPUTE_TOKEN_FILE"));if err!=nil{log.Fatal(err)}
 compute,err:=agentflow.NewComputeClient(required("HOMELAB_COMPUTE_URL"),token);if err!=nil{log.Fatal(err)}
 probeCtx,stopProbe:=context.WithTimeout(context.Background(),10*time.Second)
 err=compute.VerifyProjectCredential(probeCtx)
 stopProbe()
 if err!=nil{log.Fatal(err)}
 store,err:=agentflow.Open(required("HOMELAB_ENGINEERING_DIR"));if err!=nil{log.Fatal(err)}
 var author agentflow.Author
 if endpoint:=os.Getenv("HOMELAB_ENGINEERING_MODEL_URL");endpoint!=""{
  key:=""
  if file:=os.Getenv("HOMELAB_ENGINEERING_MODEL_TOKEN_FILE");file!=""{
   key,err=credential(file);if err!=nil{log.Fatal(err)}
  }
  author,err=agentflow.NewModelAuthor(endpoint,required("HOMELAB_ENGINEERING_MODEL_NAME"),key)
  if err!=nil{log.Fatal(err)}
 }
 engine:=&agentflow.Engine{Store:store,Compute:compute,Author:author,AllowedProjects:projects}
 ctx,stop:=signal.NotifyContext(context.Background(),syscall.SIGINT,syscall.SIGTERM)
 defer stop()
 go engine.Serve(ctx)
 if err:=agentflow.ServeUnix(ctx,required("HOMELAB_ENGINEERING_SOCKET"),engine);err!=nil{log.Fatal(err)}
}
