package session

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"errors"

	"github.com/dbontr/homelab-new/system/security"
)

// Manager grants bounded browser access only to identities whose Cloudflare
// Access assertions pass signature, issuer, audience, lifetime and owner checks.
// It stores no passwords, browser cookies, or independently renewable sessions.
type Manager struct {
	verifier *security.AccessVerifier
	audience string
	csrfKey  [32]byte
}

func New(verifier *security.AccessVerifier, audience, platformSecret string) (*Manager, error) {
	if verifier == nil || audience == "" || len(platformSecret) < 32 {
		return nil, errors.New("signed console identity and private CSRF key are required")
	}
	return &Manager{
		verifier: verifier,
		audience: audience,
		csrfKey: sha256.Sum256([]byte(platformSecret)),
	}, nil
}

// Check re-verifies the Access JWT on every call. CSRF is bound to the
// assertion, so an expired or replaced Access token cannot reuse its token.
func (m *Manager) Check(assertion string) (csrf string, ok bool) {
	if m == nil {
		return "", false
	}
	if _, err := m.verifier.Verify(assertion, m.audience); err != nil {
		return "", false
	}
	mac := hmac.New(sha256.New, m.csrfKey[:])
	_, _ = mac.Write([]byte("homelab-console-csrf/v1\x00"))
	_, _ = mac.Write([]byte(assertion))
	return base64.RawURLEncoding.EncodeToString(mac.Sum(nil)), true
}
