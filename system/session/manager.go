package session

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"sync"
	"time"

	"github.com/dbontr/homelab-new/system/security"
)

const (
	Duration = 6 * time.Hour
	maxSessions = 32
)

type entry struct {
	csrf string
	expires time.Time
}

// Manager owns short-lived browser sessions. No provider or MCP token is exposed.
type Manager struct {
	mu sync.Mutex
	unlock *security.OwnerUnlock
	entries map[[32]byte]entry
	now func() time.Time
}

func New(record security.PassphraseRecord) (*Manager, error) {
	unlock, err := security.NewOwnerUnlock(record)
	if err != nil { return nil, err }
	return &Manager{unlock: unlock, entries: make(map[[32]byte]entry), now: time.Now}, nil
}

func randomToken() (string, error) {
	var value [32]byte
	if _, err := rand.Read(value[:]); err != nil { return "", err }
	return base64.RawURLEncoding.EncodeToString(value[:]), nil
}

func tokenHash(token string) ([32]byte, bool) {
	var empty [32]byte
	b, err := base64.RawURLEncoding.DecodeString(token)
	if err != nil || len(b) != 32 { return empty, false }
	return sha256.Sum256(b), true
}

func (m *Manager) Login(passphrase []byte) (string, string, time.Time, error) {
	if m == nil { return "", "", time.Time{}, errors.New("sessions are not configured") }
	if err := m.unlock.Verify(passphrase); err != nil {
		return "", "", time.Time{}, errors.New("authentication failed")
	}
	cookie, err := randomToken()
	if err != nil { return "", "", time.Time{}, err }
	csrf, err := randomToken()
	if err != nil { return "", "", time.Time{}, err }
	key, valid := tokenHash(cookie)
	if !valid { return "", "", time.Time{}, errors.New("invalid session token") }
	m.mu.Lock()
	defer m.mu.Unlock()
	now := m.now()
	for id, session := range m.entries {
		if !now.Before(session.expires) { delete(m.entries, id) }
	}
	if len(m.entries) >= maxSessions {
		return "", "", time.Time{}, errors.New("session limit reached")
	}
	expiry := now.Add(Duration)
	m.entries[key] = entry{csrf: csrf, expires: expiry}
	return cookie, csrf, expiry, nil
}

func (m *Manager) Check(cookie string) (string, time.Time, bool) {
	if m == nil { return "", time.Time{}, false }
	hash, valid := tokenHash(cookie)
	if !valid { return "", time.Time{}, false }
	m.mu.Lock()
	defer m.mu.Unlock()
	session, exists := m.entries[hash]
	if !exists { return "", time.Time{}, false }
	if !m.now().Before(session.expires) {
		delete(m.entries, hash)
		return "", time.Time{}, false
	}
	return session.csrf, session.expires, true
}

func (m *Manager) Logout(cookie, csrf string) bool {
	if m == nil { return false }
	hash, valid := tokenHash(cookie)
	if !valid { return false }
	m.mu.Lock()
	defer m.mu.Unlock()
	session, exists := m.entries[hash]
	if !exists || !m.now().Before(session.expires) { return false }
	if len(csrf) != len(session.csrf) ||
		subtle.ConstantTimeCompare([]byte(csrf), []byte(session.csrf)) != 1 {
		return false
	}
	delete(m.entries, hash)
	return true
}
