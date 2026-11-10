package security

import (
 "bytes"
 "os"
 "path/filepath"
 "testing"
)

func TestVaultRejectsReplacedAndUnprotectedFile(t *testing.T){
 dir:=t.TempDir()
 path:=filepath.Join(dir,"credentials.json")
 key:=bytes.Repeat([]byte("k"),32)
 vault,err:=OpenVault(path,key)
 if err!=nil{t.Fatal(err)}
 if _,err:=vault.Store("service",[]byte("opaque-secret"));err!=nil{t.Fatal(err)}
 symlink:=filepath.Join(dir,"credential-link")
 if err:=os.Symlink(path,symlink);err!=nil{t.Fatal(err)}
 if _,err:=OpenVault(symlink,key);err==nil{
  t.Fatal("symlink credential vault was accepted")
 }
 if err:=os.Chmod(path,0640);err!=nil{t.Fatal(err)}
 if _,err:=OpenVault(path,key);err==nil{t.Fatal("group-readable credential vault was accepted")}
 if err:=os.Chmod(path,0600);err!=nil{t.Fatal(err)}
 if _,err:=OpenVault(path,key);err!=nil{t.Fatal("restored credential vault unavailable:",err)}
}

func TestVaultRejectsOversizedFileWithoutLoadingIt(t *testing.T){
 path:=filepath.Join(t.TempDir(),"credentials.json")
 f,err:=os.OpenFile(path,os.O_CREATE|os.O_WRONLY,0600)
 if err!=nil{t.Fatal(err)}
 if err:=f.Truncate((8<<20)+1);err!=nil{t.Fatal(err)}
 if err:=f.Close();err!=nil{t.Fatal(err)}
 if _,err:=OpenVault(path,bytes.Repeat([]byte("k"),32));err==nil{
  t.Fatal("oversized credential vault was accepted")
 }
}
