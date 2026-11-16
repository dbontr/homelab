package bridge

import (
 "crypto"
 "crypto/rand"
 "crypto/rsa"
 "crypto/sha256"
 "encoding/base64"
 "encoding/json"
 "math/big"
 "net/http"
 "net/http/httptest"
 "strings"
 "testing"
 "time"

 "github.com/dbontr/homelab-new/system/security"
)

func ownerAssertionFixture(t *testing.T)(*security.AccessVerifier,func(string,string,time.Time)string){
 t.Helper()
 key,err:=rsa.GenerateKey(rand.Reader,2048)
 if err!=nil{t.Fatal(err)}
 encoded:=func(v []byte)string{return base64.RawURLEncoding.EncodeToString(v)}
 exponent:=big.NewInt(int64(key.PublicKey.E)).Bytes()
 jwks,err:=json.Marshal(map[string]any{"keys":[]map[string]string{{
  "kty":"RSA","kid":"owner-key","alg":"RS256","use":"sig",
  "n":encoded(key.PublicKey.N.Bytes()),"e":encoded(exponent),
 }}})
 if err!=nil{t.Fatal(err)}
 verifier,err:=security.NewAccessVerifier("https://access.example.test","owner@example.test",jwks)
 if err!=nil{t.Fatal(err)}
 sign:=func(audience,email string,expires time.Time)string{
  header,_:=json.Marshal(map[string]string{"alg":"RS256","kid":"owner-key","typ":"JWT"})
  now:=time.Now()
  payload,_:=json.Marshal(map[string]any{
   "iss":"https://access.example.test","aud":[]string{audience},
   "sub":"owner-subject","email":email,
   "iat":now.Add(-time.Minute).Unix(),"nbf":now.Add(-time.Minute).Unix(),
   "exp":expires.Unix(),
  })
  message:=encoded(header)+"."+encoded(payload)
  digest:=sha256.Sum256([]byte(message))
  signature,err:=rsa.SignPKCS1v15(rand.Reader,key,crypto.SHA256,digest[:])
  if err!=nil{t.Fatal(err)}
  return message+"."+encoded(signature)
 }
 return verifier,sign
}

func TestManagedAccessOwnerMCPAuthorization(t *testing.T){
 verifier,sign:=ownerAssertionFixture(t)
 var received []string
 backend:=httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter,r *http.Request){
  if r.Header.Get("Authorization")!="Bearer "+internalSecret{
   t.Error("internal service credential was not restored")
  }
  if r.Header.Get("Cookie")!=""{t.Error("untrusted cookies reached service")}
  received=append(received,r.Header.Get("Cf-Access-Jwt-Assertion"))
  _,_=w.Write([]byte(`{"result":"ok"}`))
 }))
 defer backend.Close()
 handler,err:=New(Config{Surface:MCP,BackendURL:backend.URL,
  ClientToken:clientSecret,ServiceToken:internalSecret,
  AccessVerifier:verifier,AccessAudience:"gateway-audience"})
 if err!=nil{t.Fatal(err)}
 valid:=sign("gateway-audience","owner@example.test",time.Now().Add(time.Minute))
 requestMCP:=func(token,assertion string)int{
  r:=httptest.NewRequest(http.MethodPost,"/mcp",strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"ping"}`))
  if token!=""{r.Header.Set("Authorization","Bearer "+token)}
  if assertion!=""{r.Header.Set("Cf-Access-Jwt-Assertion",assertion)}
  r.Header.Set("Cookie","untrusted")
  w:=httptest.NewRecorder()
  handler.ServeHTTP(w,r)
  return w.Code
 }
 if got:=requestMCP("oauth:opaque-managed-token",valid);got!=200{
  t.Fatalf("signed owner behind managed OAuth denied: %d",got)
 }
 if len(received)!=1||received[0]!=valid{t.Fatal("verified owner identity lost before service dispatch")}
 for _,assertion:=range []string{
  "", "forged",sign("wrong-audience","owner@example.test",time.Now().Add(time.Minute)),
  sign("gateway-audience","other@example.test",time.Now().Add(time.Minute)),
  sign("gateway-audience","owner@example.test",time.Now().Add(-time.Minute)),
 }{
  if got:=requestMCP("oauth:untrusted",assertion);got!=http.StatusUnauthorized{
   t.Fatalf("unverified owner was admitted: %d",got)
  }
 }
 if len(received)!=1{t.Fatal("unverified identity reached service")}
 if got:=requestMCP(clientSecret,"forged");got!=200{
  t.Fatalf("existing private MCP client lost access: %d",got)
 }
 if len(received)!=2||received[1]!=""{
  t.Fatal("private-token caller forwarded a forged Access identity")
 }
 if got:=requestMCP("",valid);got!=200{
  t.Fatalf("Cloudflare signed owner without a static bearer denied: %d",got)
 }
 if len(received)!=3||received[2]!=valid{
  t.Fatal("signed assertion was not forwarded")
 }
}

func TestManagedOAuthVerifierCannotAuthorizeNativeAPI(t *testing.T){
 verifier,_:=ownerAssertionFixture(t)
 backend,calls,closeBackend:=upstream(t)
 defer closeBackend()
 for _,cfg:=range []Config{
  {Surface:API,BackendURL:backend,ClientToken:clientSecret,ServiceToken:internalSecret,
   AccessVerifier:verifier,AccessAudience:"gateway-audience"},
  {Surface:MCP,BackendURL:backend,ClientToken:clientSecret,ServiceToken:internalSecret,
   AccessVerifier:verifier},
  {Surface:MCP,BackendURL:backend,ClientToken:clientSecret,ServiceToken:internalSecret,
   AccessAudience:"gateway-audience"},
 }{
  if _,err:=New(cfg);err==nil{t.Fatal("ambiguous Access authority configuration was accepted")}
 }
 if *calls!=0{t.Fatal("invalid authentication configuration reached a service")}
}
