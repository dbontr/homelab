package main

import (
 "bytes"
 "os"
 "path/filepath"
 "strings"
 "testing"
)

func TestProtectedCredentialRead(t *testing.T){
 dir:=t.TempDir()
 path:=filepath.Join(dir,"owner-credential")
 value:=[]byte(strings.Repeat("k",64))
 if err:=os.WriteFile(path,value,0600);err!=nil{t.Fatal(err)}
 t.Setenv("HOMELAB_TEST_PROTECTED_CREDENTIAL",path)
 got,err:=readRequiredFile("HOMELAB_TEST_PROTECTED_CREDENTIAL")
 if err!=nil||!bytes.Equal(got,value){t.Fatalf("protected file was rejected: %v",err)}
 symlink:=filepath.Join(dir,"owner-link")
 if err:=os.Symlink(path,symlink);err!=nil{t.Fatal(err)}
 t.Setenv("HOMELAB_TEST_PROTECTED_CREDENTIAL",symlink)
 if _,err:=readRequiredFile("HOMELAB_TEST_PROTECTED_CREDENTIAL");err==nil{
  t.Fatal("credential symlink was accepted")
 }
 if err:=os.Chmod(path,0666);err!=nil{t.Fatal(err)}
 t.Setenv("HOMELAB_TEST_PROTECTED_CREDENTIAL",path)
 if _,err:=readRequiredFile("HOMELAB_TEST_PROTECTED_CREDENTIAL");err==nil{
  t.Fatal("writable credential file was accepted")
 }
 if err:=os.Chmod(path,0600);err!=nil{t.Fatal(err)}
 if err:=os.WriteFile(path,bytes.Repeat([]byte("X"),(1<<20)+1),0600);err!=nil{t.Fatal(err)}
 if _,err:=readRequiredFile("HOMELAB_TEST_PROTECTED_CREDENTIAL");err==nil{
  t.Fatal("oversized credential was accepted")
 }
}
