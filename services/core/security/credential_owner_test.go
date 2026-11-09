package security

import (
 "bytes"
 "encoding/json"
 "net/http"
 "net/http/httptest"
 "path/filepath"
 "strings"
 "testing"
 "time"
)

func TestOwnerCredentialEnrollmentAndRevocation(t *testing.T) {
 manager,key,now:=testSessionManager(t)
 vault,err:=OpenVault(filepath.Join(privateDir(t),"vault.json"),[]byte(strings.Repeat("k",32)))
 if err!=nil{t.Fatal(err)}
 if err:=manager.SetVault(vault);err!=nil{t.Fatal(err)}
 owner:=manager.OwnerHandler()
 request:=func(method,path string,input any)*httptest.ResponseRecorder{
  var encoded []byte
  if input!=nil{encoded,_=json.Marshal(input)}
  req:=httptest.NewRequest(method,path,bytes.NewReader(encoded))
  if input!=nil{req.Header.Set("Content-Type","application/json")}
  response:=httptest.NewRecorder()
  owner.ServeHTTP(response,req)
  return response
 }
 record:=map[string]string{"provider":"example-provider","secret":"TOP_SECRET_SHOULD_NOT_LEAK","password":testOwnerPassword,"totp":TOTPCode(key,now.Unix()/30)}
 if req:=request(http.MethodPost,"/v1/credentials",record);req.Code!=200||strings.Contains(req.Body.String(),record["secret"]){
  t.Fatalf("owner credential enrollment failed or leaked plaintext (%d)",req.Code)
 }
 list:=request(http.MethodGet,"/v1/credentials",nil)
 if list.Code!=200||strings.Contains(list.Body.String(),"TOP_SECRET_SHOULD_NOT_LEAK"){t.Fatal("credential list leaked plaintext")}
 var handles []CredentialHandle
 if err:=json.Unmarshal(list.Body.Bytes(),&handles);err!=nil||len(handles)!=1{t.Fatal("incorrect credential inventory")}
 if req:=request(http.MethodPost,"/v1/credentials",record);req.Code!=403{t.Fatal("reused authenticator code accepted")}
 client:=manager.ClientHandler()
 unknown:=httptest.NewRecorder()
 client.ServeHTTP(unknown,httptest.NewRequest(http.MethodGet,"/v1/credentials",nil))
 if unknown.Code==200{t.Fatal("gateway could enumerate owner credential inventory")}
 *now=now.Add(30*time.Second)
 result:=request(http.MethodDelete,"/v1/credentials/"+handles[0].ID,map[string]string{
  "password":testOwnerPassword,"totp":TOTPCode(key,now.Unix()/30),
 })
 if result.Code!=200{t.Fatalf("owner credential revocation failed: %d",result.Code)}
 if list:=request(http.MethodGet,"/v1/credentials",nil);list.Code!=200||strings.Contains(list.Body.String(),handles[0].ID){t.Fatal("revoked credential remains listed")}
}

func TestOwnerCredentialOperationsFailWithoutConfiguredVault(t *testing.T){
 manager,_,_:=testSessionManager(t)
 h:=manager.OwnerHandler()
 r:=httptest.NewRecorder()
 h.ServeHTTP(r,httptest.NewRequest("GET","/v1/credentials",nil))
 if r.Code!=http.StatusServiceUnavailable{t.Fatalf("missing vault should fail closed: %d",r.Code)}
}
