package security

import (
 "errors"
 "os"
 "path/filepath"
 "strings"
 "testing"
 "time"
)

// A scoped use is linearized with rotation. A rotation cannot complete while
// an earlier validated use still holds the old credential.
func TestCredentialUseSerializesAgainstRotation(t *testing.T){
 vault,err:=OpenVault(filepath.Join(privateDir(t),"vault.json"),[]byte(strings.Repeat("k",32)))
 if err!=nil{t.Fatal(err)}
 handle,err:=vault.Store("provider",[]byte("previous-secret"))
 if err!=nil{t.Fatal(err)}
 prior,err:=vault.Version(handle.ID,"provider")
 if err!=nil{t.Fatal(err)}
 entered:=make(chan struct{})
 release:=make(chan struct{})
 used:=make(chan error,1)
 go func(){
  used<-vault.WithCredentialVersion(handle.ID,"provider",prior,func(secret []byte)error{
   if vault.mu.TryLock(){
    vault.mu.Unlock()
    return errors.New("credential callback does not hold a version guard")
   }
   if string(secret)!="previous-secret"{return errors.New("wrong prior secret")}
   close(entered)
   <-release
   return nil
  })
 }()
 select{case <-entered:case <-time.After(5*time.Second):t.Fatal("credential use did not start")}
 rotated:=make(chan error,1)
 go func(){_,err:=vault.Rotate(handle.ID,"provider",[]byte("replacement-secret"));rotated<-err}()
 select{
 case err:=<-rotated:t.Fatalf("rotation completed while prior credential use was active: %v",err)
 case <-time.After(50*time.Millisecond):
 }
 close(release)
 if err:=<-used;err!=nil{t.Fatal(err)}
 if err:=<-rotated;err!=nil{t.Fatal(err)}
 if err:=vault.WithCredentialVersion(handle.ID,"provider",prior,func([]byte)error{
  t.Error("pre-rotation capability used replacement credential")
  return nil
 });err==nil{t.Fatal("old scoped credential use was not invalidated")}
 next,err:=vault.Version(handle.ID,"provider")
 if err!=nil||next==prior{t.Fatal("rotated record did not change")}
 if err:=vault.WithCredentialVersion(handle.ID,"provider",next,func(secret []byte)error{
  if string(secret)!="replacement-secret"{return errors.New("wrong replacement credential")}
  return nil
 });err!=nil{t.Fatal(err)}
}

func TestDeletedCredentialCannotBeUsedWithOldVersion(t *testing.T){
 vault,err:=OpenVault(filepath.Join(privateDir(t),"vault.json"),[]byte(strings.Repeat("z",32)))
 if err!=nil{t.Fatal(err)}
 handle,err:=vault.Store("vendor",[]byte("deleted"))
 if err!=nil{t.Fatal(err)}
 version,err:=vault.Version(handle.ID,"vendor")
 if err!=nil{t.Fatal(err)}
 if err:=vault.Delete(handle.ID);err!=nil{t.Fatal(err)}
 if err:=vault.WithCredentialVersion(handle.ID,"vendor",version,func([]byte)error{
  t.Error("deleted credential was decrypted")
  return nil
 });!errors.Is(err,os.ErrNotExist){t.Fatal("deletion did not revoke old version",err)}
}
