package agentflow

import (
 "os"
 "path/filepath"
 "strings"
 "testing"
)

func TestEngineeringMachineSecretCustody(t *testing.T) {
 dir:=t.TempDir()
 path:=filepath.Join(dir,"client-secret")
 if err:=os.WriteFile(path,[]byte(strings.Repeat("x",64)),0600);err!=nil{t.Fatal(err)}
 t.Setenv("CREDENTIALS_DIRECTORY","")
 if value,err:=readMachineSecret(path);err!=nil||len(value)!=64{t.Fatalf("private client secret rejected: %v",err)}

 if err:=os.Chmod(path,0640);err!=nil{t.Fatal(err)}
 if _,err:=readMachineSecret(path);err==nil{t.Fatal("unmanaged group-readable client secret accepted")}
 t.Setenv("CREDENTIALS_DIRECTORY",dir)
 if _,err:=readMachineSecret(path);err!=nil{t.Fatalf("managed client secret rejected: %v",err)}
 for _,mode:=range []os.FileMode{0660,0644} {
  if err:=os.Chmod(path,mode);err!=nil{t.Fatal(err)}
  if _,err:=readMachineSecret(path);err==nil{t.Fatalf("unsafe mode %04o accepted",mode)}
 }
 if err:=os.Chmod(path,0640);err!=nil{t.Fatal(err)}
 other:=t.TempDir()
 outside:=filepath.Join(other,"secret")
 if err:=os.WriteFile(outside,[]byte(strings.Repeat("x",64)),0640);err!=nil{t.Fatal(err)}
 if err:=os.Chmod(outside,0640);err!=nil{t.Fatal(err)}
 if _,err:=readMachineSecret(outside);err==nil{t.Fatal("outside credential directory accepted")}
 link:=filepath.Join(dir,"alias")
 if err:=os.Symlink(path,link);err!=nil{t.Fatal(err)}
 if _,err:=readMachineSecret(link);err==nil{t.Fatal("symbolic link accepted")}
}
