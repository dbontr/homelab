package main

import (
 "os"
 "path/filepath"
 "strings"
 "testing"
)

func TestMachineIssuerCredentialCustody(t *testing.T) {
 dir:=t.TempDir()
 path:=filepath.Join(dir,"signing-key")
 if err:=os.WriteFile(path,[]byte(strings.Repeat("s",64)),0600);err!=nil{t.Fatal(err)}
 t.Setenv("CREDENTIALS_DIRECTORY","")
 if _,err:=credential(path,256);err!=nil{t.Fatalf("private credential refused: %v",err)}

 if err:=os.Chmod(path,0640);err!=nil{t.Fatal(err)}
 if _,err:=credential(path,256);err==nil{t.Fatal("unmanaged group-readable secret accepted")}
 t.Setenv("CREDENTIALS_DIRECTORY",dir)
 if _,err:=credential(path,256);err!=nil{t.Fatalf("systemd-managed credential refused: %v",err)}

 for _,mode:=range []os.FileMode{0660,0644} {
  if err:=os.Chmod(path,mode);err!=nil{t.Fatal(err)}
  if _,err:=credential(path,256);err==nil{t.Fatalf("unsafe credential mode %04o accepted",mode)}
 }
 if err:=os.Chmod(path,0640);err!=nil{t.Fatal(err)}
 nested:=filepath.Join(dir,"nested")
 if err:=os.Mkdir(nested,0700);err!=nil{t.Fatal(err)}
 nestedPath:=filepath.Join(nested,"copy")
 if err:=os.WriteFile(nestedPath,[]byte(strings.Repeat("s",64)),0640);err!=nil{t.Fatal(err)}
 if err:=os.Chmod(nestedPath,0640);err!=nil{t.Fatal(err)}
 if _,err:=credential(nestedPath,256);err==nil{t.Fatal("nested unmanaged copy accepted")}
 link:=filepath.Join(dir,"alias")
 if err:=os.Symlink(path,link);err!=nil{t.Fatal(err)}
 if _,err:=credential(link,256);err==nil{t.Fatal("symbolic link accepted")}
}
