package privileged

import (
 "context"
 "encoding/json"
 "errors"
 "os"
 "path/filepath"
 "strings"
 "testing"
 "time"

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

func TestAuditRejectsDuplicateUnresolvedExecutionAcrossRestarts(t *testing.T){
 dir:=privateAuditDir(t)
 path:=filepath.Join(dir,"executor-actions.jsonl")
 journal,err:=OpenAuditJournal(path)
 if err!=nil{t.Fatal(err)}
 operation:="infrastructure__service_restart"
 first:=strings.Repeat("a",32)
 replay:=strings.Repeat("b",32)
 digest:=strings.Repeat("c",64)
 if err:=journal.Record(first,operation,"jupiter",digest,"started");err!=nil{t.Fatal(err)}
 if err:=journal.Record(replay,operation,"jupiter",digest,"started");err==nil{
  t.Fatal("concurrent identical execution was admitted")
 }
 if err:=journal.Record(first,operation,"jupiter",digest,"indeterminate");err!=nil{t.Fatal(err)}
 if err:=journal.Close();err!=nil{t.Fatal(err)}
 recovered,err:=OpenAuditJournal(path)
 if err!=nil{t.Fatal(err)}
 defer recovered.Close()
 if err:=recovered.Record(replay,operation,"jupiter",digest,"started");err==nil{
  t.Fatal("unresolved attempt could be retried after executor restart")
 }
 if err:=recovered.Record(replay,operation,"jupiter",strings.Repeat("d",64),"started");err!=nil{
  t.Fatal("unrelated authorized attempt was denied:",err)
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

func TestGoAuditResumesAfterRustEvidenceReconciliation(t *testing.T){
 dir:=privateAuditDir(t)
 path:=filepath.Join(dir,"executor-actions.jsonl")
 id:=strings.Repeat("a",32)
 digest:=strings.Repeat("b",64)
 evidence:=strings.Repeat("c",64)
 at:=time.Now().UTC()
 started:=auditEntry{ID:id,Operation:"infrastructure__service_restart",
  Target:"saturn",ArgumentsSHA256:digest,Outcome:"started",At:at}
 unknown:=started
 unknown.Outcome="indeterminate"
 reconciled:=started
 reconciled.Outcome="reconciled"
 reconciled.ReconciliationSHA256=evidence
 writeJournal:=func(entries ...auditEntry){
  t.Helper()
  lines:=make([]string,0,len(entries))
  for _,entry:=range entries{
   raw,err:=json.Marshal(entry)
   if err!=nil{t.Fatal(err)}
   lines=append(lines,string(raw))
  }
  if err:=os.WriteFile(path,[]byte(strings.Join(lines,"\n")+"\n"),0600);err!=nil{
   t.Fatal(err)
  }
 }
 writeJournal(started,unknown,reconciled)
 journal,err:=OpenAuditJournal(path)
 if err!=nil{t.Fatal("Go cannot read evidence-bound Rust reconciliation:",err)}
 unresolved,err:=journal.Unresolved()
 if err!=nil||len(unresolved)!=0{t.Fatalf("resolved event remains blocked: %+v %v",unresolved,err)}
 if err:=journal.Record(strings.Repeat("d",32),started.Operation,started.Target,digest,"started");err!=nil{
  t.Fatal("reconciled action cannot begin an independently authorized attempt:",err)
 }
 if err:=journal.Close();err!=nil{t.Fatal(err)}
 reconciled.ReconciliationSHA256="wrong"
 writeJournal(started,unknown,reconciled)
 if journal,err:=OpenAuditJournal(path);err==nil{
  journal.Close()
  t.Fatal("invalid evidence digest accepted")
 }
 reconciled.ReconciliationSHA256=evidence
 reconciled.Target="venus"
 writeJournal(started,unknown,reconciled)
 if journal,err:=OpenAuditJournal(path);err==nil{
  journal.Close()
  t.Fatal("substituted target accepted in reconciliation")
 }
}

func TestLegacyGoFailedReceiptNeedsOperatorInspection(t *testing.T) {
 dir:=privateAuditDir(t)
 path:=filepath.Join(dir,"executor-actions.jsonl")
 journal,err:=OpenAuditJournal(path)
 if err!=nil{t.Fatal(err)}
 id:=strings.Repeat("c",32)
 duplicate:=strings.Repeat("d",32)
 digest:=strings.Repeat("e",64)
 operation:="infrastructure__service_restart"
 if err:=journal.Record(id,operation,"saturn",digest,"started");err!=nil{t.Fatal(err)}
 if err:=journal.Record(id,operation,"saturn",digest,"failed");err!=nil{t.Fatal(err)}
 if err:=journal.Close();err!=nil{t.Fatal(err)}
 journal,err=OpenAuditJournal(path)
 if err!=nil{t.Fatal(err)}
 defer journal.Close()
 unresolved,err:=journal.Unresolved()
 if err!=nil||len(unresolved)!=1||unresolved[0].Outcome!="failed" {
  t.Fatalf("historical failed side effect was assumed safe: %+v, %v",unresolved,err)
 }
 if err:=journal.Record(duplicate,operation,"saturn",digest,"started");err==nil {
  t.Fatal("attempt was repeated before inspection of a legacy failure")
 }
}

func TestGoAuditReconciliationRequiresEvidenceAndUnresolvedAttempt(t *testing.T){
 dir:=privateAuditDir(t)
 path:=filepath.Join(dir,"executor-actions.jsonl")
 journal,err:=OpenAuditJournal(path)
 if err!=nil{t.Fatal(err)}
 operation:="infrastructure__service_restart"
 id:=strings.Repeat("a",32)
 digest:=strings.Repeat("b",64)
 evidence:=strings.Repeat("c",64)
 if err:=journal.Record(id,operation,"saturn",digest,"started");err!=nil{t.Fatal(err)}
 if err:=journal.Record(id,operation,"saturn",digest,"indeterminate");err!=nil{t.Fatal(err)}
 if err:=journal.Record(id,operation,"saturn",digest,"reconciled");err==nil{
  t.Fatal("ordinary audit append bypassed evidence requirement")
 }
 if err:=journal.Reconcile(id,"short");err==nil{
  t.Fatal("incomplete evidence digest accepted")
 }
 if err:=journal.Reconcile(id,evidence);err!=nil{t.Fatal(err)}
 if err:=journal.Reconcile(id,evidence);err==nil{
  t.Fatal("reconciled attempt accepted a second resolution")
 }
 if err:=journal.Close();err!=nil{t.Fatal(err)}
 recovered,err:=OpenAuditJournal(path)
 if err!=nil{t.Fatal("reconciliation was not durable:",err)}
 defer recovered.Close()
 pending,err:=recovered.Unresolved()
 if err!=nil||len(pending)!=0{t.Fatalf("resolved operation stayed unresolved: %+v %v",pending,err)}
 if err:=recovered.Record(strings.Repeat("d",32),operation,"saturn",digest,"started");err!=nil{
  t.Fatal("valid independently reauthorized attempt was still blocked:",err)
 }
}

func TestAuditJournalRejectsAmbiguousWireRecords(t *testing.T){
 directory:=privateAuditDir(t)
 path:=filepath.Join(directory,"executor-actions.jsonl")
 start:=auditEntry{
  ID:strings.Repeat("a",32),Operation:"infrastructure__service_restart",
  Target:"saturn",ArgumentsSHA256:strings.Repeat("b",64),
  Outcome:"started",At:time.Now().UTC(),
 }
 raw,err:=json.Marshal(start)
 if err!=nil{t.Fatal(err)}
 cases:=map[string]string{
  "duplicate identity":strings.Replace(string(raw),
    `"id":"`+start.ID+`"`,`"id":"`+start.ID+`","id":"`+strings.Repeat("c",32)+`"`,1),
  "unknown field":strings.Replace(string(raw),
    `"outcome":"started"`,`"outcome":"started","extra":"privileged"`,1),
  "uppercase digest":strings.Replace(string(raw),
    `"arguments_sha256":"`+start.ArgumentsSHA256+`"`,
    `"arguments_sha256":"`+strings.Repeat("A",64)+`"`,1),
 }
 for name,record:=range cases{
  t.Run(name,func(t *testing.T){
   if err:=os.WriteFile(path,[]byte(record+"\n"),0600);err!=nil{t.Fatal(err)}
   journal,err:=OpenAuditJournal(path)
   if err==nil{
    journal.Close()
    t.Fatal("ambiguous or Rust-incompatible audit record admitted")
   }
  })
 }
}
