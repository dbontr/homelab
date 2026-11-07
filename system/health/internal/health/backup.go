package health

import(
 "context"
 "errors"
 "fmt"
 "os"
 "path/filepath"
 "strings"
)

func(s *Store)IntegrityCheck(ctx context.Context)error{
 var report string
 if err:=s.db.QueryRowContext(ctx,"PRAGMA integrity_check").Scan(&report);err!=nil{return err}
 if report!="ok"{return fmt.Errorf("database integrity check failed: %s",report)}
 return nil
}

// Backup creates a consistent SQLite snapshot in a private destination file.
// This is an explicit local operator action, not an automatic upload.
func(s *Store)Backup(ctx context.Context,destination string)error{
 if strings.TrimSpace(destination)==""||destination==":memory:"{return errors.New("backup destination is required")}
 parent:=filepath.Dir(destination)
 info,err:=os.Stat(parent)
 if err!=nil{return err}
 if !info.IsDir()||info.Mode().Perm()&0077!=0{
  return errors.New("backup directory must already exist and be private (0700)")
 }
 if err=s.IntegrityCheck(ctx);err!=nil{return err}
 f,err:=os.OpenFile(destination,os.O_WRONLY|os.O_CREATE|os.O_EXCL,0600)
 if err!=nil{return fmt.Errorf("create backup destination: %w",err)}
 if err=f.Close();err!=nil{_ =os.Remove(destination);return err}
 _,err=s.db.ExecContext(ctx,"VACUUM INTO ?",destination)
 if err!=nil{_ =os.Remove(destination);return fmt.Errorf("SQLite backup: %w",err)}
 if _,err=os.Stat(destination);err!=nil{return err}
 return nil
}
