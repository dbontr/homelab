package mcp

import (
	"bytes"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"math/big"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func accessTokenForTest(t *testing.T, private *rsa.PrivateKey, claims map[string]any) string {
	t.Helper()
	header, err := json.Marshal(map[string]string{"alg": "RS256", "kid": "test-key"})
	if err != nil {
		t.Fatal(err)
	}
	body, err := json.Marshal(claims)
	if err != nil {
		t.Fatal(err)
	}
	first := base64.RawURLEncoding.EncodeToString(header) + "." + base64.RawURLEncoding.EncodeToString(body)
	digest := sha256.Sum256([]byte(first))
	signature, err := rsa.SignPKCS1v15(rand.Reader, private, crypto.SHA256, digest[:])
	if err != nil {
		t.Fatal(err)
	}
	return first + "." + base64.RawURLEncoding.EncodeToString(signature)
}

func testAccess(t *testing.T) (*AccessVerifier, *rsa.PrivateKey, func()) {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/cdn-cgi/access/certs" {
			http.NotFound(w, r)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"keys": []map[string]string{{
			"kty": "RSA",
			"kid": "test-key",
			"alg": "RS256",
			"n": base64.RawURLEncoding.EncodeToString(key.N.Bytes()),
			"e": base64.RawURLEncoding.EncodeToString(big.NewInt(int64(key.E)).Bytes()),
		}}})
	}))
	verifier, err := newAccessVerifier(server.URL, "expected-audience",
		server.URL+"/cdn-cgi/access/certs", "owner@example.com", server.Client())
	if err != nil {
		server.Close()
		t.Fatal(err)
	}
	return verifier, key, server.Close
}

func validAccessClaims(issuer string) map[string]any {
	return map[string]any{
		"iss": issuer,
		"aud": []string{"expected-audience"},
		"sub": "owner-subject",
		"email": "owner@example.com",
		"iat": time.Now().Unix() - 10,
		"nbf": time.Now().Unix() - 10,
		"exp": time.Now().Add(time.Hour).Unix(),
	}
}

func TestAccessVerifierRejectsInvalidIdentityAndSignatures(t *testing.T) {
	verifier, key, closeServer := testAccess(t)
	defer closeServer()
	base := validAccessClaims(verifier.issuer)
	if err := verifier.Verify(accessTokenForTest(t, key, base)); err != nil {
		t.Fatalf("valid signed owner rejected: %v", err)
	}
	for name, mutate := range map[string]func(map[string]any){
		"wrong owner": func(c map[string]any) { c["email"] = "other@example.com" },
		"wrong audience": func(c map[string]any) { c["aud"] = "another-audience" },
		"wrong issuer": func(c map[string]any) { c["iss"] = "https://other.example" },
		"expired": func(c map[string]any) { c["exp"] = time.Now().Add(-time.Minute).Unix() },
		"future not before": func(c map[string]any) { c["nbf"] = time.Now().Add(time.Hour).Unix() },
		"missing subject": func(c map[string]any) { c["sub"] = "" },
	} {
		t.Run(name, func(t *testing.T) {
			claims := validAccessClaims(verifier.issuer)
			mutate(claims)
			if err := verifier.Verify(accessTokenForTest(t, key, claims)); err == nil {
				t.Fatal("invalid identity accepted")
			}
		})
	}
	token := accessTokenForTest(t, key, validAccessClaims(verifier.issuer))
	pieces := strings.Split(token, ".")
	pieces[2] = base64.RawURLEncoding.EncodeToString(bytes.Repeat([]byte{0x42}, 256))
	if err := verifier.Verify(strings.Join(pieces, ".")); err == nil {
		t.Fatal("forged signature accepted")
	}
	if _, err := newAccessVerifier("http://invalid.example", "aud",
		"https://invalid.example/cdn-cgi/access/certs", "owner@example.com", nil); err == nil {
		t.Fatal("non-TLS issuer accepted")
	}
}

func TestMCPAccessAuthentication(t *testing.T) {
	verifier, key, closeServer := testAccess(t)
	defer closeServer()
	s := New(nil)
	s.Access = verifier
	handler, err := s.HTTPHandler("independent-local-token", nil)
	if err != nil {
		t.Fatal(err)
	}
	testRequest := func(assertion, auth string) int {
		t.Helper()
		body := fmt.Sprintf(`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{}}`)
		req := httptest.NewRequest(http.MethodPost, "http://localhost/mcp", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Accept", "application/json")
		if assertion != "" {
			req.Header.Set("Cf-Access-Jwt-Assertion", assertion)
		}
		if auth != "" {
			req.Header.Set("Authorization", auth)
		}
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)
		return w.Code
	}
	signed := accessTokenForTest(t, key, validAccessClaims(verifier.issuer))
	if status := testRequest(signed, "Bearer opaque-oauth-token"); status != 200 {
		t.Fatalf("valid Access identity: HTTP %d", status)
	}
	if status := testRequest("", "Bearer opaque-oauth-token"); status != 401 {
		t.Fatalf("opaque OAuth token alone must fail: HTTP %d", status)
	}
	if status := testRequest("", "Bearer independent-local-token"); status != 200 {
		t.Fatalf("local token rejected: HTTP %d", status)
	}
	handler, err = s.HTTPHandler("", nil)
	if err != nil { t.Fatalf("Access-only server rejected: %v", err) }
	if status := testRequest(signed, ""); status != 200 { t.Fatalf("Access-only valid JWT: HTTP %d", status) }
	if status := testRequest("", "Bearer "); status != 401 { t.Fatalf("empty bearer token accepted: HTTP %d", status) }
	if status := testRequest("", "Bearer independent-local-token"); status != 401 { t.Fatalf("bearer override accepted: HTTP %d", status) }
	s.Access = nil
	if _, err := s.HTTPHandler("", nil); err == nil { t.Fatal("HTTP server without authentication accepted") }
}
