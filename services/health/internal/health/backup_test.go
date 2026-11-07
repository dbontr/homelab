package health

import(
 "context"
 "os"
 "path/filepath"
 "testing"
 "time"
)

func TestBackupCapturesCommittedHealthRecords(t *testing.T){
 s:=testStore(t)
 ctx:=context.Background()
 _,err:=s.Add(ctx,Record{Domain:"biometric",Metric:"weight",Value:82,Unit:"kg",
  At:time.Now(),Source:"manual",Method:"reported"})
 if err!=nil{t.Fatal(err)}
 destination:=filepath.Join(t.TempDir(),"snapshot.db")
 if err=s.Backup(ctx,destination);err!=nil{t.Fatal(err)}
 info,err:=os.Stat(destination)
 if err!=nil{t.Fatal(err)}
 if info.Mode().Perm()&0077!=0{t.Fatalf("backup has unsafe permissions: %v",info.Mode())}
 restored,err:=Open(destination)
 if err!=nil{t.Fatal(err)}
 defer restored.Close()
 if err=restored.IntegrityCheck(ctx);err!=nil{t.Fatal(err)}
 list,err:=restored.List(ctx,"biometric","weight",20,false)
 if err!=nil||len(list)!=1||list[0].Value!=82{t.Fatalf("backup missing measurements: %+v %v",list,err)}
 if err=s.Backup(ctx,destination);err==nil{t.Fatal("backup silently overwrote existing snapshot")}
}
