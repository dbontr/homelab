package privileged

import (
 "context"
 "encoding/json"
 "errors"
 "os"
 "path/filepath"
 "strings"
 "testing"

 "github.com/dbontr/homelab-new/system/security"
)

func TestExecutorAuditDeniesWhenPrivateJournalCannotBeWritten(t *testing.T){
 dir:=privateAuditDir(t)
 journal,err:=OpenAuditJournal(filepath.Join(dir,"actions.jsonl"))
 if err!=nil{t.Fatal(err)}
 runner:=0
 executor,err:=New(Config{Host:"jupiter",Systemctl:"/usr/bin/systemctl",
  AllowedServices:[]string{"eminence.service"},Auth:&fakeAuth{decision:security.Authorization{Allowed:true}},
  Releases:&fakeRelease{},Audit:journal,
  Run:func(context.Context,string,...string)error{runner++;return nil}})
 if err!=nil{t.Fatal(err)}
 args:=json.RawMessage(`{"host":"jupiter","service":"eminence.service"}`)
 decision,err:=executor.Execute(context.Background(),"infrastructure__service_restart","jupiter",args)
 if err!=nil||!decision.Allowed||runner!=1{t.Fatalf("write not executed: %v %v",decision,err)}
 raw,err:=os.ReadFile(filepath.Join(dir,"actions.jsonl"))
 if err!=nil{t.Fatal(err)}
 if strings.Count(string(raw),"\n")!=2||strings.Contains(string(raw),"service")&&strings.Contains(string(raw),"eminence.service") {
  t.Fatal("audit record count incorrect or raw arguments disclosed")
 }
 if !strings.Contains(string(raw),`"outcome":"started"`)||!strings.Contains(string(raw),`"outcome":"accepted"`){
  t.Fatal("audit lacked started or accepted outcome")
 }
 blocker:=filepath.Join(privateAuditDir(t),"unwriteable")
 if err:=os.Mkdir(blocker,0700);err!=nil{t.Fatal(err)}
 blockedJournal,err:=OpenAuditJournal(filepath.Join(blocker,"actions.jsonl"))
 if err!=nil{t.Fatal(err)}
 if err:=blockedJournal.Close();err!=nil{t.Fatal(err)}
 if err:=os.Remove(filepath.Join(blocker,"actions.jsonl"));err!=nil{t.Fatal(err)}
 if err:=os.Remove(blocker);err!=nil{t.Fatal(err)}
 executor.cfg.Audit=blockedJournal
 decision,err=executor.Execute(context.Background(),"infrastructure__service_restart","jupiter",args)
 if err==nil||decision.Allowed||runner!=1{t.Fatal("failed journal did not prevent execution")}
}
func TestAuditJournalRejectsUnsafePaths(t *testing.T){
 if _,err:=OpenAuditJournal("relative.log");err==nil{t.Fatal("relative audit file accepted")}
 dir:=privateAuditDir(t)
 target:=filepath.Join(dir,"real")
 if err:=os.WriteFile(target,[]byte(""),0644);err!=nil{t.Fatal(err)}
 if _,err:=OpenAuditJournal(target);err==nil{t.Fatal("world-readable audit accepted")}
 if err:=os.Symlink(target,filepath.Join(dir,"link"));err!=nil{t.Fatal(err)}
 if _,err:=OpenAuditJournal(filepath.Join(dir,"link"));err==nil{t.Fatal("symlink audit accepted")}
}

func TestAuditJournalReopensIndeterminateRustOutcomes(t *testing.T){
 dir:=privateAuditDir(t)
 path:=filepath.Join(dir,"executor-actions.jsonl")
 journal,err:=OpenAuditJournal(path)
 if err!=nil{t.Fatal(err)}
 id:=strings.Repeat("a",32)
 digest:=strings.Repeat("b",64)
 if err=journal.Record(id,"infrastructure__service_restart","saturn",digest,"started");err!=nil{t.Fatal(err)}
 if err=journal.Record(id,"infrastructure__service_restart","saturn",digest,"indeterminate");err!=nil{t.Fatal(err)}
 if err=journal.Close();err!=nil{t.Fatal(err)}
 resumed,err:=OpenAuditJournal(path)
 if err!=nil{t.Fatal("valid Rust indeterminate event was rejected:",err)}
 defer resumed.Close()
 pending,err:=resumed.Unresolved()
 if err!=nil||len(pending)!=1||pending[0].ID!=id||pending[0].Outcome!="indeterminate"{
  t.Fatalf("unresolved attempted side effect lost across restart: %+v, %v",pending,err)
 }
 if err:=resumed.Record(id,"infrastructure__service_restart","saturn",digest,"started");err==nil{
  t.Fatal("duplicate started event was accepted")
 }
}

func TestExecutorDoesNotClaimFailedEffectWasNotApplied(t *testing.T){
 journal:=testExecutorAudit(t)
 executor,err:=New(Config{
  Host:"saturn",Systemctl:"/usr/bin/systemctl",
  AllowedServices:[]string{"homelab-platform.service"},
  Auth:&fakeAuth{decision:security.Authorization{Allowed:true}},
  Releases:&fakeRelease{},Audit:journal,
  Run:func(context.Context,string,...string)error{
   return errors.New("actuator result unknown")
  },
 })
 if err!=nil{t.Fatal(err)}
 result,err:=executor.Execute(context.Background(),
  "infrastructure__service_restart","saturn",
  json.RawMessage(`{"host":"saturn","service":"homelab-platform.service"}`))
 if err==nil||result.Allowed{t.Fatal("ambiguous operation was claimed successful")}
 pending,err:=journal.Unresolved()
 if err!=nil||len(pending)!=1||pending[0].Outcome!="indeterminate"{
  t.Fatalf("executor lost an uncertain side effect: %+v, %v",pending,err)
 }
}
