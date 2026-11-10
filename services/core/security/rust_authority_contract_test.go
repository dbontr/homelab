package security

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/dbontr/homelab-new/system/canonicaljson"
)

func rustAuthorityFixture(t *testing.T, name string) []byte {
	t.Helper()
	path := filepath.Join("..", "..", "security-authority", "tests", "fixtures", name)
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func TestSecurityAuthorityRS256ContractFixture(t *testing.T) {
	var fixture struct {
		Issuer    string          `json:"issuer"`
		Email     string          `json:"email"`
		Audience  string          `json:"audience"`
		Principal string          `json:"principal"`
		JWKS      json.RawMessage `json:"jwks"`
		Token     string          `json:"token"`
	}
	if err := json.Unmarshal(rustAuthorityFixture(t, "access-jwt.json"), &fixture); err != nil {
		t.Fatal(err)
	}
	verifier, err := NewAccessVerifier(fixture.Issuer, fixture.Email, fixture.JWKS)
	if err != nil {
		t.Fatal(err)
	}
	verifier.now = func() time.Time { return time.Unix(1770000120, 0) }
	got, err := verifier.Verify(fixture.Token, fixture.Audience)
	if err != nil || got != fixture.Principal {
		t.Fatalf("valid signed owner identity was rejected: %q, %v", got, err)
	}
	if _, err := verifier.Verify(fixture.Token, "unauthorized-audience"); err == nil {
		t.Fatal("wrong audience accepted")
	}
	verifier.now = func() time.Time { return time.Unix(1770004000, 0) }
	if _, err := verifier.Verify(fixture.Token, fixture.Audience); err == nil {
		t.Fatal("expired owner assertion accepted")
	}
}

func TestSecurityAuthorityEncryptedVaultContractFixture(t *testing.T) {
	path := filepath.Join(t.TempDir(), "credentials.json")
	if err := os.WriteFile(path, rustAuthorityFixture(t, "vault-records.json"), 0600); err != nil {
		t.Fatal(err)
	}
	vault, err := OpenVault(path, []byte(strings.Repeat("k", 32)))
	if err != nil {
		t.Fatal(err)
	}
	const handle = "abcdefghijklmnopqrstuvwxABCDEFGH"
	if err := vault.WithCredential(handle, "research", func(secret []byte) error {
		if string(secret) != "fixture-credential" {
			t.Fatalf("incompatible AES-GCM credential: %q", secret)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if err := vault.WithCredential(handle, "other", func([]byte) error {
		t.Fatal("wrong provider accessed a protected credential")
		return nil
	}); err == nil {
		t.Fatal("provider scope was not enforced")
	}
}

func TestSecurityAuthorityPBKDF2AndTOTPContract(t *testing.T) {
	record := PassphraseRecord{
		Version:    1,
		Iterations: 310000,
		Salt:       "YWJjZGVmZ2hpamtsbW5vcHFyc3R1dnd4",
		Digest:     "l9m+PJ5k/gZlE6OL9Ek9YfJ7L5ajP+9F+d5Z1mp4TXg",
	}
	if !record.Verify([]byte("correct horse battery staple")) ||
		record.Verify([]byte("wrong password")) {
		t.Fatal("PBKDF2 verification contract changed")
	}
	key, err := DecodeTOTPSecret("GEZDGNBVGY3TQOJQGEZDGNBVGY3TQOJQ")
	if err != nil {
		t.Fatal(err)
	}
	if got := TOTPCode(key, 1); got != "287082" {
		t.Fatalf("TOTP algorithm contract changed: %s", got)
	}
}

func TestSecurityAuthorityCanonicalJSONContract(t *testing.T) {
	testCases := []struct {
		input, expected string
	}{
		{`{"z":1,"a":{"y":true,"x":null}}`, `{"a":{"x":null,"y":true},"z":1}`},
		{`{"v":1234567890123456789012345678901234567890,"x":1.234e-12}`, `{"v":1234567890123456789012345678901234567890,"x":1.234e-12}`},
		{`{"operator":"<owner>&"}`, `{"operator":"\u003cowner\u003e\u0026"}`},
	}
	for _, tc := range testCases {
		actual, _, err := canonicaljson.Object(json.RawMessage(tc.input))
		if err != nil || string(actual) != tc.expected {
			t.Fatalf("canonical JSON divergence: %q, %v", actual, err)
		}
	}
}
