package security

import (
 "bytes"
 "errors"
 "os"
 "path/filepath"
 "testing"
)

func TestEncryptedCredentialVault(t *testing.T){
 path:=filepath.Join(t.TempDir(),"credentials.json")
 key:=bytes.Repeat([]byte("z"),32)
 v,err:=OpenVault(path,key);if err!=nil{t.Fatal(err)}
 secret:=[]byte("fake-provider-access-token")
 handle,err:=v.Store("provider",secret);if err!=nil||handle.ID==""||handle.Provider!="provider"{t.Fatalf("invalid handle: %+v %v",handle,err)}
 raw,err:=os.ReadFile(path);if err!=nil||bytes.Contains(raw,secret){t.Fatal("credential stored in plaintext")}
 info,err:=os.Stat(path);if err!=nil||info.Mode().Perm()!=0600{t.Fatal("credential vault not owner-only")}
 reopened,err:=OpenVault(path,key);if err!=nil{t.Fatal(err)}
 if len(reopened.List())!=1{t.Fatal("credential handle not durable")}
 if err:=reopened.WithCredential(handle.ID,"wrong-provider",func([]byte)error{t.Fatal("wrong provider decrypted");return nil});!errors.Is(err,os.ErrNotExist){t.Fatal("provider identity not enforced")}
 if err:=reopened.WithCredential(handle.ID,"provider",func(value []byte)error{
   if !bytes.Equal(value,secret){t.Fatal("credential mismatch")}
   return nil
 });err!=nil{t.Fatal(err)}
 bad,err:=OpenVault(path,bytes.Repeat([]byte("x"),32));if err!=nil{t.Fatal(err)}
 if err:=bad.WithCredential(handle.ID,"provider",func([]byte)error{t.Fatal("wrong key decrypted");return nil});err==nil{t.Fatal("wrong key accepted")}
 if err:=reopened.Delete(handle.ID);err!=nil{t.Fatal(err)}
 if len(reopened.List())!=0{t.Fatal("credential not deleted")}
}
