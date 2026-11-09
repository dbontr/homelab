package security

import(
 "bytes"
 "context"
 "encoding/json"
 "net/http"
 "net/http/httptest"
 "net/url"
 "path/filepath"
 "strings"
 "testing"
)

func TestOAuthCodeExchangeStoresTokensOnlyInVault(t *testing.T){
 calls:=0
 provider:=httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter,r *http.Request){
  calls++
  if r.Method!=http.MethodPost||r.PostFormValue("grant_type")!="authorization_code"||r.PostFormValue("code")!="code"||r.PostFormValue("code_verifier")==""{t.Fatal("invalid token exchange")}
  w.Header().Set("Content-Type","application/json")
  _,_=w.Write([]byte(`{"access_token":"fake-token","refresh_token":"fake-refresh","token_type":"Bearer"}`))
 }))
 defer provider.Close()
 p:=Provider{Name:"vendor",AuthorizeURL:"https://provider.example/auth",TokenURL:provider.URL,ClientID:"test-client",RedirectURL:"http://127.0.0.1:8765/callback"}
 broker,err:=NewOAuthBroker([]Provider{p});if err!=nil{t.Fatal(err)}
 vault,err:=OpenVault(filepath.Join(t.TempDir(),"vault.json"),bytes.Repeat([]byte("v"),32));if err!=nil{t.Fatal(err)}
 flow,err:=broker.Begin("vendor");if err!=nil{t.Fatal(err)}
 u,err:=url.Parse(flow.URL);if err!=nil{t.Fatal(err)}
 handle,err:=broker.CompleteAndStore(context.Background(),u.Query().Get("state"),"code",provider.Client(),vault)
 if err!=nil||handle.ID==""||calls!=1{t.Fatalf("token exchange failed: %+v %v",handle,err)}
 encoded,_:=json.Marshal(handle)
 if bytes.Contains(encoded,[]byte("fake-token")){t.Fatal("access token exposed in handle")}
 if err:=vault.WithCredential(handle.ID,"vendor",func(secret []byte)error{
  if !strings.Contains(string(secret),"fake-refresh"){t.Fatal("refresh token not stored")}
  return nil
 });err!=nil{t.Fatal(err)}
 if _,err:=broker.CompleteAndStore(context.Background(),u.Query().Get("state"),"code",provider.Client(),vault);err==nil{t.Fatal("replayed callback accepted")}
}
