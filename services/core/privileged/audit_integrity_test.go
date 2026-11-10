package privileged

import (
 "context"
 "encoding/json"
 "os"
 "path/filepath"
 "strings"
 "testing"

 "github.com/dbontr/homelab-new/system/security"
)

func TestAuditJournalReopensValidatedOperationHistory(t *testing.T){
 path:=filepath.Join(privateAuditDir(t),"operations.jsonl")
 journal,err:=OpenAuditJournal(path)
 if err!=nil{t.Fatal(err)}
 id:=strings.Repeat("a",32)
 digest:=strings.Repeat("b",64)
 if err:=journal.Record(id,"infrastructure__service_restart","jupiter",digest,"started");err!=nil{t.Fatal(err)}
 if err:=journal.Record(id,"infrastructure__service_restart","jupiter",digest,"accepted");err!=nil{t.Fatal(err)}
 if err:=journal.Close();err!=nil{t.Fatal(err)}
 reopened,err:=OpenAuditJournal(path)
 if err!=nil{t.Fatalf("valid durable audit history rejected: %v",err)}
 defer reopened.Close()
 if err:=reopened.Record(strings.Repeat("c",32),"infrastructure__service_restart","jupiter",digest,"started");err!=nil{t.Fatal(err)}
}

func TestAuditJournalRejectsCorruptionAndMissingFinalNewline(t *testing.T){
 for _,tc:=range []struct{name,record string}{
  {"incomplete",`{"id":"missing"}`},
  {"malformed",`not-json
`},
  {"outcome_without_start",`{"id":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","operation":"infrastructure__service_restart","target":"jupiter","arguments_sha256":"`+strings.Repeat("b",64)+`","outcome":"accepted","at":"2026-01-01T00:00:00Z"}
`},
  {"replayed_start",`{"id":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","operation":"infrastructure__service_restart","target":"jupiter","arguments_sha256":"`+strings.Repeat("b",64)+`","outcome":"started","at":"2026-01-01T00:00:00Z"}
{"id":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","operation":"infrastructure__service_restart","target":"jupiter","arguments_sha256":"`+strings.Repeat("b",64)+`","outcome":"started","at":"2026-01-01T00:00:01Z"}
`},
 }{
  t.Run(tc.name,func(t *testing.T){
   path:=filepath.Join(privateAuditDir(t),"audit.jsonl")
   if err:=os.WriteFile(path,[]byte(tc.record),0600);err!=nil{t.Fatal(err)}
   if journal,err:=OpenAuditJournal(path);err==nil{
    journal.Close()
    t.Fatal("corrupt audit history was accepted")
   }
  })
 }
}

func TestAuditJournalReplacementFailsClosed(t *testing.T){
 dir:=privateAuditDir(t)
 path:=filepath.Join(dir,"audit.jsonl")
 journal,err:=OpenAuditJournal(path)
 if err!=nil{t.Fatal(err)}
 defer journal.Close()
 before:=strings.Repeat("a",32)
 digest:=strings.Repeat("b",64)
 if err:=journal.Record(before,"infrastructure__service_restart","jupiter",digest,"started");err!=nil{t.Fatal(err)}
 if err:=os.Rename(path,path+".moved");err!=nil{t.Fatal(err)}
 if err:=os.WriteFile(path,[]byte{},0600);err!=nil{t.Fatal(err)}
 if err:=journal.Record(before,"infrastructure__service_restart","jupiter",digest,"accepted");err==nil{
  t.Fatal("replacement audit inode accepted an outcome")
 }
 auth:=&fakeAuth{decision:security.Authorization{Allowed:true}}
 calls:=0
 executor,err:=New(Config{
  Host:"jupiter",Systemctl:"/usr/bin/systemctl",AllowedServices:[]string{"homelab-platform.service"},
  Auth:auth,Releases:&fakeRelease{},Audit:journal,
  Run:func(context.Context,string,...string)error{calls++;return nil},
 })
 if err!=nil{t.Fatal(err)}
 args:=json.RawMessage(`{"host":"jupiter","service":"homelab-platform.service"}`)
 result,err:=executor.Execute(context.Background(),"infrastructure__service_restart","jupiter",args)
 if err==nil||result.Allowed||calls!=0{t.Fatalf("executor acted after journal substitution: %+v, %v",result,err)}
}

func TestAuditJournalRefusesWritesAfterClose(t *testing.T){
 path:=filepath.Join(privateAuditDir(t),"audit.jsonl")
 journal,err:=OpenAuditJournal(path)
 if err!=nil{t.Fatal(err)}
 if err:=journal.Close();err!=nil{t.Fatal(err)}
 if err:=journal.Close();err!=nil{t.Fatal(err)}
 if err:=journal.Record(strings.Repeat("a",32),"infrastructure__service_restart","jupiter",strings.Repeat("b",64),"started");err==nil{
  t.Fatal("closed journal accepted an operation")
 }
}
