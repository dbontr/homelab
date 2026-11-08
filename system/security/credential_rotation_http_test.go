package security

import (
 "bytes"
 "encoding/json"
 "net/http"
 "net/http/httptest"
 "os"
 "path/filepath"
 "strings"
 "testing"
 "time"
)

func TestOwnerCredentialRotationRequiresFreshFactorsAndDoesNotExposeSecret(t *testing.T){
 manager,key,now:=testSessionManager(t)
 vault,err:=OpenVault(filepath.Join(privateDir(t),"vault.json"),[]byte(strings.Repeat("v",32)))
 if err!=nil{t.Fatal(err)}
 handle,err:=vault.Store("provider",[]byte("initial-test-credential"))
 if err!=nil{t.Fatal(err)}
 if err:=manager.SetVault(vault);err!=nil{t.Fatal(err)}
 before,err:=vault.Version(handle.ID,"provider")
 if err!=nil{t.Fatal(err)}
 request:=func(code string)*httptest.ResponseRecorder{
  payload,_:=json.Marshal(map[string]string{
   "provider":"provider","secret":"replacement-test-credential",
   "password":testOwnerPassword,"totp":code,
  })
  req:=httptest.NewRequest(http.MethodPut,"/v1/credentials/"+handle.ID,bytes.NewReader(payload))
  req.Header.Set("Content-Type","application/json")
  w:=httptest.NewRecorder();manager.OwnerHandler().ServeHTTP(w,req)
  return w
 }
 code:=TOTPCode(key,now.Unix()/30)
 first:=request(code)
 if first.Code!=200||strings.Contains(first.Body.String(),"replacement-test-credential"){
  t.Fatal("credential rotation failed or leaked secret",first.Code,first.Body.String())
 }
 var rotated CredentialHandle
 if err:=json.Unmarshal(first.Body.Bytes(),&rotated);err!=nil||rotated.ID!=handle.ID||rotated.Provider!="provider"{
  t.Fatal("credential identity changed",rotated,err)
 }
 current,err:=vault.Version(handle.ID,"provider")
 if err!=nil||current==before{t.Fatal("encrypted credential revision unchanged")}
 retry:=request(code)
 if retry.Code!=403{t.Fatal("reused TOTP authorized repeat rotation",retry.Code)}
 raw,err:=os.ReadFile(filepath.Join(filepath.Dir(vault.path),"vault.json"))
 if err!=nil||bytes.Contains(raw,[]byte("replacement-test-credential")){t.Fatal("rotated secret persisted in plaintext")}
 if err:=vault.WithCredentialVersion(handle.ID,"provider",before,func([]byte)error{
  t.Fatal("old credential revision was usable");return nil
 });err==nil{t.Fatal("rotation did not revoke outstanding capability revision")}
 *now=now.Add(30*time.Second)
 missing:=httptest.NewRequest(http.MethodPut,"/v1/credentials/no-such-handle",bytes.NewReader([]byte("{}")))
 missing.Header.Set("Content-Type","application/json")
 response:=httptest.NewRecorder()
 manager.OwnerHandler().ServeHTTP(response,missing)
 if response.Code==200{t.Fatal("unknown credential handle accepted")}
}
