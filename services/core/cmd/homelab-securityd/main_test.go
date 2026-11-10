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
 got,err:=readPrivateCredential("HOMELAB_TEST_PROTECTED_CREDENTIAL")
 if err!=nil||!bytes.Equal(got,value){t.Fatalf("protected file was rejected: %v",err)}
 symlink:=filepath.Join(dir,"owner-link")
 if err:=os.Symlink(path,symlink);err!=nil{t.Fatal(err)}
 t.Setenv("HOMELAB_TEST_PROTECTED_CREDENTIAL",symlink)
 if _,err:=readPrivateCredential("HOMELAB_TEST_PROTECTED_CREDENTIAL");err==nil{
  t.Fatal("credential symlink was accepted")
 }
 t.Setenv("HOMELAB_TEST_PROTECTED_CREDENTIAL",path)
 for _,mode:=range []os.FileMode{0640,0644,0666}{
  if err:=os.Chmod(path,mode);err!=nil{t.Fatal(err)}
  if _,err:=readPrivateCredential("HOMELAB_TEST_PROTECTED_CREDENTIAL");err==nil{
   t.Fatalf("group- or world-accessible credential accepted with mode %04o",mode)
  }
 }
 if err:=os.Chmod(path,0600);err!=nil{t.Fatal(err)}
 if err:=os.WriteFile(path,bytes.Repeat([]byte("X"),(1<<20)+1),0600);err!=nil{t.Fatal(err)}
 if _,err:=readPrivateCredential("HOMELAB_TEST_PROTECTED_CREDENTIAL");err==nil{
  t.Fatal("oversized credential was accepted")
 }
}

func TestPublicReadOnlyConfigurationRemainsReadable(t *testing.T){
 dir:=t.TempDir()
 path:=filepath.Join(dir,"policy.json")
 if err:=os.WriteFile(path,[]byte("{}"),0444);err!=nil{t.Fatal(err)}
 if err:=os.Chmod(path,0444);err!=nil{t.Fatal(err)}
 t.Setenv("HOMELAB_TEST_PROTECTED_CREDENTIAL",path)
 if data,err:=readRequiredFile("HOMELAB_TEST_PROTECTED_CREDENTIAL");err!=nil||string(data)!="{}"{
  t.Fatalf("public immutable policy was rejected: %v",err)
 }
 if _,err:=readPrivateCredential("HOMELAB_TEST_PROTECTED_CREDENTIAL");err==nil{
  t.Fatal("public configuration could be used as a private owner credential")
 }
}
