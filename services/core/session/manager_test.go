package session

import (
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"math/big"
	"strings"
	"testing"
	"time"

	"github.com/dbontr/homelab-new/system/security"
)

func signedAccess(t *testing.T, key *rsa.PrivateKey, audience, email string, expires time.Time) string {
	t.Helper()
	encode := func(value any) string {
		raw, err := json.Marshal(value)
		if err != nil { t.Fatal(err) }
		return base64.RawURLEncoding.EncodeToString(raw)
	}
	now := time.Now()
	header := encode(map[string]string{"alg":"RS256","kid":"console-signing-key","typ":"JWT"})
	claims := encode(map[string]any{
		"iss":"https://dbontr.cloudflareaccess.com",
		"aud":[]string{audience},
		"sub":"owner-id",
		"email":email,
		"iat":now.Add(-time.Minute).Unix(),
		"nbf":now.Add(-time.Minute).Unix(),
		"exp":expires.Unix(),
	})
	message := header+"."+claims
	digest := sha256.Sum256([]byte(message))
	signature, err := rsa.SignPKCS1v15(rand.Reader,key,crypto.SHA256,digest[:])
	if err != nil { t.Fatal(err) }
	return message+"."+base64.RawURLEncoding.EncodeToString(signature)
}

func TestConsoleAccessRequiresSignedOwnerIdentity(t *testing.T) {
	key,err:=rsa.GenerateKey(rand.Reader,2048)
	if err!=nil { t.Fatal(err) }
	n:=base64.RawURLEncoding.EncodeToString(key.PublicKey.N.Bytes())
	e:=base64.RawURLEncoding.EncodeToString(big.NewInt(int64(key.PublicKey.E)).Bytes())
	jwks,err:=json.Marshal(map[string]any{"keys":[]any{map[string]string{
		"kty":"RSA","alg":"RS256","kid":"console-signing-key","n":n,"e":e,
	}}})
	if err!=nil{t.Fatal(err)}
	verifier,err:=security.NewAccessVerifier("https://dbontr.cloudflareaccess.com","owner@example.test",jwks)
	if err!=nil{t.Fatal(err)}
	manager,err:=New(verifier,"console-audience",strings.Repeat("k",40))
	if err!=nil{t.Fatal(err)}
	token:=signedAccess(t,key,"console-audience","owner@example.test",time.Now().Add(time.Hour))
	csrf,ok:=manager.Check(token)
	if !ok||len(csrf)!=43{t.Fatal("verified owner rejected")}
	second,ok:=manager.Check(token)
	if !ok||second!=csrf{t.Fatal("CSRF token is not bound to identity")}
	otherManager,err:=New(verifier,"console-audience",strings.Repeat("x",40))
	if err!=nil{t.Fatal(err)}
	otherCSRF,_:=otherManager.Check(token)
	if otherCSRF==csrf{t.Fatal("CSRF is not bound to platform secret")}
	invalid:=[]string{
		"",
		token+"tampered",
		signedAccess(t,key,"another-audience","owner@example.test",time.Now().Add(time.Hour)),
		signedAccess(t,key,"console-audience","attacker@example.test",time.Now().Add(time.Hour)),
		signedAccess(t,key,"console-audience","owner@example.test",time.Now().Add(-time.Second)),
	}
	for _,assertion:=range invalid{
		if _,ok:=manager.Check(assertion);ok{t.Fatal("untrusted Access assertion accepted")}
	}
	if _,ok:=(*Manager)(nil).Check(token);ok{t.Fatal("unconfigured identity accepted")}
	if _,err:=New(nil,"console-audience",strings.Repeat("x",40));err==nil{t.Fatal("nil verifier accepted")}
	if _,err:=New(verifier,"",strings.Repeat("x",40));err==nil{t.Fatal("missing audience accepted")}
	if _,err:=New(verifier,"console-audience","short");err==nil{t.Fatal("weak platform key accepted")}
}
