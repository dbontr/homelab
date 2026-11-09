package mcpsvc

import (
	"context"
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
)

func TestAccessVerifier(t *testing.T) {
	private, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil { t.Fatal(err) }
	b64 := func(b []byte) string { return base64.RawURLEncoding.EncodeToString(b) }
	certs := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"keys": []any{map[string]any{
			"kty": "RSA", "kid": "test", "alg": "RS256", "use": "sig",
			"n": b64(private.N.Bytes()), "e": b64(big.NewInt(int64(private.E)).Bytes()),
		}}})
	}))
	defer certs.Close()
	v := &AccessVerifier{Issuer: "https://team.cloudflareaccess.com", Audience: "app-aud",
		Owner: "owner@example.com", CertsURL: certs.URL, Client: certs.Client()}
	sign := func(email, aud string, expires int64) string {
		t.Helper()
		head := b64([]byte(`{"alg":"RS256","kid":"test"}`))
		body, err := json.Marshal(map[string]any{
			"iss": v.Issuer, "aud": []string{aud}, "sub": "user-1",
			"email": email, "exp": expires, "iat": time.Now().Unix(),
		})
		if err != nil { t.Fatal(err) }
		msg := head + "." + b64(body)
		hash := sha256.Sum256([]byte(msg))
		sig, err := rsa.SignPKCS1v15(rand.Reader, private, crypto.SHA256, hash[:])
		if err != nil { t.Fatal(err) }
		return msg + "." + b64(sig)
	}
	ok := sign("Owner@Example.Com", "app-aud", time.Now().Add(7*24*time.Hour).Unix())
	if err := v.Verify(context.Background(), ok); err != nil {
		t.Fatalf("valid Access assertion rejected: %v", err)
	}
	cases := map[string]string{
		"no assertion": "",
		"invalid email": sign("other@example.com","app-aud",time.Now().Add(time.Hour).Unix()),
		"other app": sign("owner@example.com","other-app",time.Now().Add(time.Hour).Unix()),
		"expired": sign("owner@example.com","app-aud",time.Now().Add(-time.Hour).Unix()),
		"excessive duration": sign("owner@example.com","app-aud",time.Now().Add(16*24*time.Hour).Unix()),
		"altered signature": ok[:len(ok)-3]+"abc",
	}
	for name, assertion := range cases {
		t.Run(name, func(t *testing.T) {
			if err := v.Verify(context.Background(), assertion); err == nil {
				t.Fatal("untrusted assertion accepted")
			}
		})
	}
}

func TestAccessConfigurationFailsClosed(t *testing.T) {
	t.Setenv("ADAPTIVE_TEACHING_ACCESS_ISSUER","https://team.cloudflareaccess.com")
	t.Setenv("ADAPTIVE_TEACHING_ACCESS_AUDIENCE","aud")
	t.Setenv("ADAPTIVE_TEACHING_ACCESS_CERTS_URL","https://team.cloudflareaccess.com/cdn-cgi/access/certs")
	t.Setenv("ADAPTIVE_TEACHING_ACCESS_OWNER_FILE","")
	if _, err := AccessFromEnv(); err == nil || !strings.Contains(err.Error(),"requires") {
		t.Fatal("partial Access configuration accepted")
	}
	t.Setenv("ADAPTIVE_TEACHING_ACCESS_ISSUER","")
	t.Setenv("ADAPTIVE_TEACHING_ACCESS_AUDIENCE","")
	t.Setenv("ADAPTIVE_TEACHING_ACCESS_CERTS_URL","")
	if v, err := AccessFromEnv(); err != nil || v != nil {
		t.Fatal("fully disabled Access must preserve the static-bearer configuration")
	}
}
