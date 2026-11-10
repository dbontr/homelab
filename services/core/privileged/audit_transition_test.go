package privileged

import (
 "os"
 "path/filepath"
 "strings"
 "sync"
 "testing"
)

func TestAuditJournalRequiresMatchingStartAndSingleOutcome(t *testing.T){
 journal,err:=OpenAuditJournal(filepath.Join(privateAuditDir(t),"actions.jsonl"))
 if err!=nil{t.Fatal(err)}
 defer journal.Close()
 id:=strings.Repeat("a",32)
 digest:=strings.Repeat("b",64)
 if err:=journal.Record(id,"infrastructure__service_restart","saturn",digest,"accepted");err==nil{
  t.Fatal("outcome without a started operation was accepted")
 }
 if err:=journal.Record(id,"infrastructure__service_restart","saturn",digest,"started");err!=nil{t.Fatal(err)}
 if err:=journal.Record(id,"infrastructure__service_restart","saturn",digest,"started");err==nil{
  t.Fatal("repeated start was accepted")
 }
 if err:=journal.Record(id,"infrastructure__service_restart","saturn",strings.Repeat("c",64),"accepted");err==nil{
  t.Fatal("changed argument digest accepted after confirmation")
 }
 if err:=journal.Record(id,"infrastructure__service_stop","saturn",digest,"accepted");err==nil{
  t.Fatal("operation substitution was accepted")
 }
 if err:=journal.Record(id,"infrastructure__service_restart","jupiter",digest,"accepted");err==nil{
  t.Fatal("resource substitution was accepted")
 }
 if err:=journal.Record(id,"infrastructure__service_restart","saturn",digest,"accepted");err!=nil{
  t.Fatal("matching outcome was rejected:",err)
 }
 if err:=journal.Record(id,"infrastructure__service_restart","saturn",digest,"failed");err==nil{
  t.Fatal("second terminal outcome was accepted")
 }
 reopened,err:=OpenAuditJournal(filepath.Join(privateAuditDir(t),"none.jsonl"))
 if err!=nil{t.Fatal(err)}
 reopened.Close()
}

func TestConcurrentAuditOutcomesCannotDoubleComplete(t *testing.T){
 journal,err:=OpenAuditJournal(filepath.Join(privateAuditDir(t),"concurrent.jsonl"))
 if err!=nil{t.Fatal(err)}
 defer journal.Close()
 id:=strings.Repeat("d",32)
 digest:=strings.Repeat("e",64)
 if err:=journal.Record(id,"infrastructure__service_restart","saturn",digest,"started");err!=nil{t.Fatal(err)}
 var wg sync.WaitGroup
 success:=make(chan bool,12)
 for i:=0;i<12;i++{
  wg.Add(1)
  go func(){
   defer wg.Done()
   err:=journal.Record(id,"infrastructure__service_restart","saturn",digest,"accepted")
   success<-err==nil
  }()
 }
 wg.Wait()
 close(success)
 count:=0
 for ok:=range success{if ok{count++}}
 if count!=1{t.Fatalf("exactly one completion must be recorded, got %d",count)}
 path:=journal.path
 if err:=journal.Close();err!=nil{t.Fatal(err)}
 raw,err:=os.ReadFile(path)
 if err!=nil{t.Fatal(err)}
 if strings.Count(string(raw),"\n")!=2{
  t.Fatalf("expected one start and one final outcome, found %d",strings.Count(string(raw),"\n"))
 }
 verified,err:=OpenAuditJournal(path)
 if err!=nil{t.Fatal(err)}
 defer verified.Close()
}
