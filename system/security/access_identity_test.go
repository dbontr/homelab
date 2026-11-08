package security

import (
 "crypto"
 "crypto/rand"
 "crypto/rsa"
 "crypto/sha256"
 "encoding/base64"
 "encoding/json"
 "math/big"
 "testing"
 "time"
)

func accessFixture(t *testing.T,audience string)(*AccessVerifier,string,string){
 t.Helper()
 key,err:=rsa.GenerateKey(rand.Reader,2048)
 if err!=nil{t.Fatal(err)}
 encode:=func(v any)string{
  raw,err:=json.Marshal(v);if err!=nil{t.Fatal(err)}
  return base64.RawURLEncoding.EncodeToString(raw)
 }
 exponent:=big.NewInt(int64(key.PublicKey.E)).Bytes()
 jwks,_:=json.Marshal(map[string]any{"keys":[]any{map[string]any{
  "kid":"key1","kty":"RSA","alg":"RS256",
  "n":base64.RawURLEncoding.EncodeToString(key.PublicKey.N.Bytes()),
  "e":base64.RawURLEncoding.EncodeToString(exponent),
 }}})
 verifier,err:=NewAccessVerifier("https://issuer.example.test","owner@example.test",jwks)
 if err!=nil{t.Fatal(err)}
 now:=time.Now()
 payload:=map[string]any{"iss":"https://issuer.example.test","sub":"user-123",
  "email":"owner@example.test","aud":audience,"iat":now.Add(-time.Minute).Unix(),
  "nbf":now.Add(-time.Minute).Unix(),"exp":now.Add(20*time.Minute).Unix()}
 message:=encode(map[string]any{"alg":"RS256","kid":"key1","typ":"JWT"})+"."+encode(payload)
 sum:=sha256.Sum256([]byte(message))
 signature,err:=rsa.SignPKCS1v15(rand.Reader,key,crypto.SHA256,sum[:])
 if err!=nil{t.Fatal(err)}
 assertion:=message+"."+base64.RawURLEncoding.EncodeToString(signature)
 principal,err:=verifier.Verify(assertion,audience)
 if err!=nil{t.Fatal(err)}
 return verifier,assertion,principal
}

func TestAccessVerifierSignedOwnerOnly(t *testing.T){
 verifier,token,principal:=accessFixture(t,"gateway-aud")
 if principal!="https://issuer.example.test#user-123#owner@example.test"{t.Fatalf("bad principal %q",principal)}
 if _,err:=verifier.Verify(token,"browser-aud");err==nil{t.Fatal("wrong audience accepted")}
 if _,err:=verifier.Verify(token+"x","gateway-aud");err==nil{t.Fatal("tampering accepted")}
 if _,err:=verifier.Verify("not-a-jwt","gateway-aud");err==nil{t.Fatal("malformed assertion accepted")}
 if _,err:=verifier.Verify("","gateway-aud");err==nil{t.Fatal("missing assertion accepted")}
}

func TestIdentityBoundSessionRejectsAnotherPrincipal(t *testing.T){
 manager,key,now:=testSessionManager(t)
 _,_,principal:=accessFixture(t,"gateway-aud")
 _,err:=manager.AuthenticateFor(principal,testOwnerPassword,TOTPCode(key,now.Unix()/30))
 if err!=nil{t.Fatal(err)}
 args:=json.RawMessage(`{"project_id":"jupiter"}`)
 if manager.AuthorizeFor("attacker","platform-gateway","workspace__workspace_create","jupiter",args).Allowed{
  t.Fatal("attacker reused owner unlock")
 }
 if manager.StatusFor("attacker").Active||manager.RevokeFor("attacker"){
  t.Fatal("another principal observed or revoked owner session")
 }
 if !manager.AuthorizeFor(principal,"platform-gateway","workspace__workspace_create","jupiter",args).Allowed{
  t.Fatal("verified owner authorization failed")
 }
 if !manager.RevokeFor(principal)||manager.StatusFor(principal).Active{
  t.Fatal("verified owner revocation failed")
 }
}
