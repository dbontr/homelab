package machineidentity

import (
 "crypto"
 "crypto/rand"
 "crypto/rsa"
 "crypto/sha256"
 "encoding/base64"
 "encoding/json"
 "net/http"
 "net/http/httptest"
 "strings"
 "testing"
 "time"
)

func testIssuer(t *testing.T)(http.Handler,*rsa.PrivateKey){
 t.Helper()
 key,err:=rsa.GenerateKey(rand.Reader,3072)
 if err!=nil{t.Fatal(err)}
 h,err:=NewHandler(Config{Issuer:"https://identity.example.test",
  Audience:"compute-projects",ClientID:"engineering",
  ClientSecret:[]byte(strings.Repeat("a",64)),PrivateKey:key})
 if err!=nil{t.Fatal(err)}
 return h,key
}

func post(h http.Handler,user,secret,body,contentType string)*httptest.ResponseRecorder{
 w:=httptest.NewRecorder()
 r:=httptest.NewRequest(http.MethodPost,"http://localhost/oauth2/token",strings.NewReader(body))
 r.SetBasicAuth(user,secret)
 r.Header.Set("Content-Type",contentType)
 h.ServeHTTP(w,r)
 return w
}

func TestOAuthMachineIdentityAndPublicJWKS(t *testing.T){
 h,key:=testIssuer(t)
 response:=post(h,"engineering",strings.Repeat("a",64),
  "grant_type=client_credentials","application/x-www-form-urlencoded")
 if response.Code!=200{t.Fatalf("status %d: %s",response.Code,response.Body.String())}
 if response.Header().Get("Cache-Control")!="no-store"{t.Fatal("token response cacheable")}
 var token struct{
  AccessToken string `json:"access_token"`
  TokenType string `json:"token_type"`
  ExpiresIn int `json:"expires_in"`
 }
 if err:=json.Unmarshal(response.Body.Bytes(),&token);err!=nil{t.Fatal(err)}
 if token.TokenType!="Bearer"||token.ExpiresIn!=300{t.Fatal("invalid token metadata")}
 parts:=strings.Split(token.AccessToken,".")
 if len(parts)!=3{t.Fatal("invalid JWT structure")}
 claimsBytes,err:=base64.RawURLEncoding.DecodeString(parts[1])
 if err!=nil{t.Fatal(err)}
 var claims map[string]any
 if err:=json.Unmarshal(claimsBytes,&claims);err!=nil{t.Fatal(err)}
 if claims["iss"]!="https://identity.example.test"||claims["aud"]!="compute-projects"||
  claims["sub"]!="engineering"{t.Fatal("issuer, audience, or subject mismatch")}
 exp:=int64(claims["exp"].(float64))
 if exp<time.Now().Unix()+280||exp>time.Now().Unix()+305{t.Fatal("invalid TTL")}
 digest:=sha256.Sum256([]byte(parts[0]+"."+parts[1]))
 signature,_:=base64.RawURLEncoding.DecodeString(parts[2])
 if err:=rsa.VerifyPKCS1v15(&key.PublicKey,crypto.SHA256,digest[:],signature);err!=nil{t.Fatal(err)}
 jwks:=httptest.NewRecorder()
 h.ServeHTTP(jwks,httptest.NewRequest(http.MethodGet,"http://localhost/.well-known/jwks.json",nil))
 if jwks.Code!=200||!strings.Contains(jwks.Body.String(),`"alg":"RS256"`){t.Fatal("public RSA key unavailable")}
 second:=post(h,"engineering",strings.Repeat("a",64),
  "grant_type=client_credentials","application/x-www-form-urlencoded")
 if second.Body.String()==response.Body.String(){t.Fatal("token replay identity must be unique")}
}

func TestRejectUnauthorizedGrantsAndClients(t *testing.T){
 h,_:=testIssuer(t)
 cases:=[]struct{
  user,secret,body,contentType string
  status int
 }{
  {"other",strings.Repeat("a",64),"grant_type=client_credentials","application/x-www-form-urlencoded",401},
  {"engineering","bad","grant_type=client_credentials","application/x-www-form-urlencoded",401},
  {"engineering",strings.Repeat("a",64),"grant_type=password","application/x-www-form-urlencoded",400},
  {"engineering",strings.Repeat("a",64),"grant_type=client_credentials&scope=admin","application/x-www-form-urlencoded",400},
  {"engineering",strings.Repeat("a",64),"grant_type=client_credentials","application/json",415},
  {"engineering",strings.Repeat("a",64),"grant_type=client_credentials&grant_type=client_credentials","application/x-www-form-urlencoded",400},
 }
 for _,tc:=range cases{
  got:=post(h,tc.user,tc.secret,tc.body,tc.contentType)
  if got.Code!=tc.status{t.Errorf("invalid request returned %d, expected %d",got.Code,tc.status)}
 }
 w:=httptest.NewRecorder()
 h.ServeHTTP(w,httptest.NewRequest(http.MethodGet,"http://localhost/oauth2/token",nil))
 if w.Code!=http.StatusMethodNotAllowed{t.Fatal("GET unexpectedly accepts token requests")}
}

func TestRejectInsecureIssuerAndWeakKey(t *testing.T){
 key,err:=rsa.GenerateKey(rand.Reader,2048)
 if err!=nil{t.Fatal(err)}
 cfg:=Config{Issuer:"http://identity.example.test",Audience:"compute",
  ClientID:"engineering",ClientSecret:[]byte(strings.Repeat("a",64)),PrivateKey:key}
 if _,err:=NewHandler(cfg);err==nil{t.Fatal("HTTP issuer accepted")}
 cfg.Issuer="https://identity.example.test"
 if _,err:=NewHandler(cfg);err==nil{t.Fatal("weak RSA key accepted")}
}
