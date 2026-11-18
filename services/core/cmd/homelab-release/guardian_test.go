package main

import (
 "context"
 "crypto/sha256"
 "encoding/hex"
 "encoding/json"
 "errors"
 "os"
 "path/filepath"
 "strings"
 "testing"
 "time"

 "github.com/dbontr/homelab-new/system/canonicaljson"
 "github.com/dbontr/homelab-new/system/privileged"
 "github.com/dbontr/homelab-new/system/release"
)

const (
 guardianRev="1111111111111111111111111111111111111111"
 guardianOld="/nix/store/aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa-previous"
 guardianTarget="/nix/store/bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb-target"
)

func guardianFixture(t *testing.T)(*release.Controller,*string,string){
 t.Helper()
 root:=t.TempDir()
 state:=filepath.Join(root,"release")
 current:=guardianOld
 runner:=func(_ context.Context,bin string,args ...string)(string,error){
  command:=bin+" "+strings.Join(args," ")
  switch{
  case strings.Contains(command,"rev-parse HEAD"):return guardianRev+"\n",nil
  case strings.Contains(command,"status --porcelain"):return "",nil
  case strings.Contains(command,"build --offline"):return guardianTarget+"\n",nil
  case strings.HasSuffix(bin,"/nix-env"):
   current=args[len(args)-1]
   return "",nil
  case strings.Contains(command,"switch-to-configuration"):return "",nil
  case strings.HasSuffix(bin,"/systemctl"):return "active\n",nil
  default:return "",errors.New("unexpected fake release command")
  }
 }
 ctrl,err:=release.New(release.Config{
  Host:"saturn",FlakeDir:root,StateDir:state,
  Nix:"/usr/bin/nix",NixEnv:"/usr/bin/nix-env",
  Git:"/usr/bin/git",Systemctl:"/usr/bin/systemctl",
  HealthServices:[]string{"homelab-securityd.service"},
  Run:runner,Current:func()(string,error){return current,nil},
 })
 if err!=nil{t.Fatal(err)}
 stage,err:=ctrl.Stage(context.Background(),guardianRev)
 if err!=nil{t.Fatal(err)}
 t.Setenv("HOMELAB_RELEASE_AUDIT_FILE",filepath.Join(state,"executor-actions.jsonl"))
 return ctrl,&current,stage.ID
}

func authorizedGuardianAudit(t *testing.T,ctrl *release.Controller,id,requestID,outcome string){
 t.Helper()
 raw,err:=json.Marshal(map[string]string{"host":"saturn","release_id":id})
 if err!=nil{t.Fatal(err)}
 canonical,_,err:=canonicaljson.Object(raw)
 if err!=nil{t.Fatal(err)}
 digest:=sha256.Sum256(canonical)
 a,err:=privileged.OpenAuditJournal(filepath.Join(ctrl.Config.StateDir,"executor-actions.jsonl"))
 if err!=nil{t.Fatal(err)}
 defer a.Close()
 operation:="infrastructure__release_activate"
 if err:=a.Record(requestID,operation,"saturn",hex.EncodeToString(digest[:]),"started");err!=nil{t.Fatal(err)}
 if outcome=="accepted"{
  if err:=a.Record(requestID,operation,"saturn",hex.EncodeToString(digest[:]),"accepted");err!=nil{t.Fatal(err)}
 }
}

func TestGuardianAcceptsOnlyAuditedRelease(t *testing.T){
 ctrl,current,id:=guardianFixture(t)
 requestID:=strings.Repeat("c",32)
 authorizedGuardianAudit(t,ctrl,id,requestID,"accepted")
 if err:=guardRelease(context.Background(),ctrl,"activate",requestID,id);err!=nil{t.Fatal(err)}
 journal,err:=ctrl.Status()
 if err!=nil||journal.Phase!="active"||*current!=guardianTarget{t.Fatalf("release not active: %+v %v",journal,err)}
 if err:=guardRelease(context.Background(),ctrl,"activate",requestID,id);err==nil{
  t.Fatal("replayed accepted release")
 }
}

func TestGuardianRejectsUnapprovedRelease(t *testing.T){
 ctrl,current,id:=guardianFixture(t)
 requestID:=strings.Repeat("d",32)
 authorizedGuardianAudit(t,ctrl,id,requestID,"started")
 ctx,cancel:=context.WithTimeout(context.Background(),20*time.Millisecond)
 defer cancel()
 if err:=guardRelease(ctx,ctrl,"activate",requestID,id);err==nil{t.Fatal("unapproved release started")}
 if *current!=guardianOld{t.Fatal("unapproved request changed generation")}
}

func TestGuardianResumesAuthorizedInterruptedTransition(t *testing.T){
 ctrl,current,id:=guardianFixture(t)
 requestID:=strings.Repeat("e",32)
 authorizedGuardianAudit(t,ctrl,id,requestID,"started")
 txn:=release.Journal{Phase:"activating",ID:id,Previous:guardianOld,Target:guardianTarget,Time:time.Now().UTC()}
 for _,name:=range []string{"journal.json","attempt-"+id+".json"}{
  raw,err:=json.Marshal(txn)
  if err!=nil{t.Fatal(err)}
  if err:=os.WriteFile(filepath.Join(ctrl.Config.StateDir,name),raw,0600);err!=nil{t.Fatal(err)}
 }
 *current=guardianTarget
 if err:=guardRelease(context.Background(),ctrl,"activate",requestID,id);err!=nil{t.Fatal(err)}
 state,err:=ctrl.Status()
 if err!=nil||state.Phase!="active"{t.Fatalf("interrupted release not completed: %+v %v",state,err)}
}
