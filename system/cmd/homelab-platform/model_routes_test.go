package main

import (
 "os"
 "path/filepath"
 "strings"
 "testing"

 "github.com/dbontr/homelab-new/system/agentflow"
)

func TestCapabilityModelRouteConfigurationUsesPrivateTokenFiles(t *testing.T){
 dir:=t.TempDir()
 token:=filepath.Join(dir,"secret")
 if err:=os.WriteFile(token,[]byte(strings.Repeat("x",40)),0600);err!=nil{t.Fatal(err)}
 config:=filepath.Join(dir,"routes.json")
 contents:=`{"routes":[{"name":"local-planner","purpose":"planning","endpoint":"http://127.0.0.1:7654/v1/chat/completions","model":"local","local_only":true,"token_file":"TOKEN"}]}`
 contents=strings.Replace(contents,"TOKEN",token,1)
 if err:=os.WriteFile(config,[]byte(contents),0600);err!=nil{t.Fatal(err)}
 t.Setenv("HOMELAB_CAPABILITY_MODEL_ROUTES_FILE",config)
 t.Setenv("HOMELAB_CAPABILITY_MODEL_URL","")
 t.Setenv("HOMELAB_CAPABILITY_MODEL_NAME","")
 t.Setenv("HOMELAB_CAPABILITY_MODEL_TOKEN_FILE","")
 planner,err:=configuredCapabilityPlanner()
 if err!=nil{t.Fatal(err)}
 if _,ok:=planner.(*agentflow.ModelRouter);!ok{t.Fatalf("expected purpose router: %T",planner)}
 t.Setenv("HOMELAB_CAPABILITY_MODEL_URL","http://127.0.0.1:123/v1/chat/completions")
 if _,err:=configuredCapabilityPlanner();err==nil{t.Fatal("legacy and multi-model config combined")}
 t.Setenv("HOMELAB_CAPABILITY_MODEL_URL","")
 if err:=os.Chmod(token,0644);err!=nil{t.Fatal(err)}
 if _,err:=configuredCapabilityPlanner();err==nil{t.Fatal("world-readable route credential accepted")}
}
