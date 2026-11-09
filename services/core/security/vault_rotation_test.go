package security

import (
 "context"
 "encoding/json"
 "errors"
 "os"
 "path/filepath"
 "strings"
 "testing"
 "time"
)

func TestVaultRotationKeepsHandleAndChangesEncryptedRevision(t *testing.T){
 path:=filepath.Join(privateDir(t),"credentials.json")
 key:=[]byte(strings.Repeat("k",32))
 vault,err:=OpenVault(path,key);if err!=nil{t.Fatal(err)}
 old:=[]byte("initial-private-token")
 handle,err:=vault.Store("vendor",old);if err!=nil{t.Fatal(err)}
 before,err:=vault.Version(handle.ID,"vendor");if err!=nil{t.Fatal(err)}
 if _,err:=vault.Rotate(handle.ID,"another-vendor",[]byte("replacement"));!errors.Is(err,os.ErrNotExist){
  t.Fatal("provider substitution accepted",err)
 }
 if _,err:=vault.Rotate(handle.ID,"vendor",nil);err==nil{t.Fatal("empty replacement permitted")}
 rotated,err:=vault.Rotate(handle.ID,"vendor",[]byte("new-private-token"))
 if err!=nil||rotated.ID!=handle.ID||rotated.CreatedAt!=handle.CreatedAt{t.Fatal("rotation lost handle identity",rotated,err)}
 after,err:=vault.Version(handle.ID,"vendor")
 if err!=nil||before==after{t.Fatal("credential revision was not changed",err)}
 if err:=vault.WithCredentialVersion(handle.ID,"vendor",before,func([]byte)error{
  t.Fatal("old credential revision remained usable");return nil
 });err==nil{t.Fatal("old version was accepted")}
 reopened,err:=OpenVault(path,key);if err!=nil{t.Fatal(err)}
 if err:=reopened.WithCredentialVersion(handle.ID,"vendor",after,func(secret []byte)error{
  if string(secret)!="new-private-token"{t.Fatal("rotated secret was not retained")}
  return nil
 });err!=nil{t.Fatal(err)}
 data,err:=os.ReadFile(path)
 if err!=nil||strings.Contains(string(data),"new-private-token")||strings.Contains(string(data),"initial-private-token"){
  t.Fatal("rotated vault contains plaintext")
 }
}

func TestScopedCapabilityCannotCrossCredentialRotation(t *testing.T){
 sessions,key,now:=testSessionManager(t)
 loginAt(t,sessions,key,now)
 vault,err:=OpenVault(filepath.Join(privateDir(t),"vault.json"),[]byte(strings.Repeat("j",32)))
 if err!=nil{t.Fatal(err)}
 handle,err:=vault.Store("vendor",[]byte("old-secret"));if err!=nil{t.Fatal(err)}
 broker,err:=NewScopedCredentialBroker(vault,sessions);if err!=nil{t.Fatal(err)}
 args:=json.RawMessage(`{"project_id":"jupiter"}`)
 cap,err:=broker.Prepare("local-operator",handle.ID,"vendor","workspace__workspace_create","jupiter",args,time.Minute)
 if err!=nil{t.Fatal(err)}
 if _,err:=vault.Rotate(handle.ID,"vendor",[]byte("new-secret"));err!=nil{t.Fatal(err)}
 decision,err:=broker.Consume(context.Background(),"local-operator",cap.ID,cap.Operation,cap.Target,args,
  func([]byte)error{t.Fatal("stale capability accessed a rotated credential");return nil})
 if err==nil||decision.Allowed{t.Fatal("stale credential lease was not invalidated",decision,err)}
 cap,err=broker.Prepare("local-operator",handle.ID,"vendor","workspace__workspace_create","jupiter",args,time.Minute)
 if err!=nil{t.Fatal(err)}
 decision,err=broker.Consume(context.Background(),"local-operator",cap.ID,cap.Operation,cap.Target,args,
  func(secret []byte)error{
   if string(secret)!="new-secret"{t.Fatal("wrong credential revision")}
   return nil
  })
 if err!=nil||!decision.Allowed{t.Fatal("new credential lease was rejected",decision,err)}
}
