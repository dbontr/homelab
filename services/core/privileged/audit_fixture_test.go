package privileged

import (
 "path/filepath"
 "os"
 "testing"
)

func testExecutorAudit(t *testing.T)*AuditJournal{
 t.Helper()
 journal,err:=OpenAuditJournal(filepath.Join(privateAuditDir(t),"privileged.jsonl"))
 if err!=nil{t.Fatal(err)}
 t.Cleanup(func(){_ =journal.Close()})
 return journal
}

func privateAuditDir(t *testing.T)string{
 t.Helper()
 dir:=t.TempDir()
 if err:=os.Chmod(dir,0700);err!=nil{t.Fatal(err)}
 return dir
}
