package session

import (
	"strings"
	"testing"
	"time"

	"github.com/dbontr/homelab-new/system/security"
)

func TestOwnerSessionScopeExpiryAndLogout(t *testing.T) {
	record, err := security.HashPassphrase([]byte("distinct-browser-session-passphrase"))
	if err != nil { t.Fatal(err) }
	manager, err := New(record)
	if err != nil { t.Fatal(err) }
	if _,_,_,err := manager.Login([]byte("wrong"));err == nil { t.Fatal("weak login accepted") }
	cookie, csrf, expiry, err := manager.Login([]byte("distinct-browser-session-passphrase"))
	if err != nil { t.Fatal(err) }
	if len(cookie) < 40 || len(csrf) < 40 || !expiry.After(time.Now()) { t.Fatal("invalid session") }
	if _, _, ok := manager.Check(cookie); !ok { t.Fatal("session not found") }
	if _, _, ok := manager.Check(strings.Repeat("x",44));ok { t.Fatal("unrecognized token accepted") }
	if manager.Logout(cookie, "wrong-csrf") { t.Fatal("CSRF bypass") }
	if !manager.Logout(cookie, csrf) { t.Fatal("valid logout failed") }
	if _,_,ok := manager.Check(cookie);ok { t.Fatal("session survived logout") }
	cookie,_,_,err = manager.Login([]byte("distinct-browser-session-passphrase"))
	if err != nil { t.Fatal(err) }
	manager.now = func() time.Time { return time.Now().Add(Duration + time.Minute) }
	if _,_,ok := manager.Check(cookie);ok { t.Fatal("expired session accepted") }
}
