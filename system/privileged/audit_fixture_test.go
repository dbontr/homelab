package privileged

import (
 "path/filepath"
 "testing"
)

func testExecutorAudit(t *testing.T)*AuditJournal{
 t.Helper()
 journal,err:=OpenAuditJournal(filepath.Join(t.TempDir(),"privileged.jsonl"))
 if err!=nil{t.Fatal(err)}
 return journal
}
