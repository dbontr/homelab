package security

import (
	"context"
	"encoding/base32"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

const testOwnerPassword = "valid-owner-passphrase-exceeding-16"

func testSessionManager(t *testing.T) (*AdminSessions, []byte, *time.Time) {
	t.Helper()
	secret := []byte("12345678901234567890")
	record, err := HashPassphrase([]byte(testOwnerPassword))
	if err != nil {
		t.Fatal(err)
	}
	cfg := SessionConfig{Owner: "owner", Client: "platform-gateway", Verifier: record,
		TOTPSecret: base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(secret),
		StateFile:  filepath.Join(privateDir(t), "auth.json"), Policies: []OperationPolicy{
			{Operation: "workspace__pool_register_image", Targets: []string{"jupiter", "venus"}},
			{Operation: "workspace__workspace_create", Targets: []string{"jupiter", "venus"}},
			{Operation: "cluster__configure", Targets: []string{"saturn"}, Destructive: true},
			{Operation: "workspace__github_merge_pull_request", Targets: []string{"dbontr/homelab-new"}},
		},
	}
	a, err := NewAdminSessions(cfg)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Unix(1_700_000_000, 0).UTC()
	a.now = func() time.Time { return now }
	return a, secret, &now
}

func loginAt(t *testing.T, a *AdminSessions, secret []byte, now *time.Time) {
	t.Helper()
	status, err := a.Authenticate(testOwnerPassword, TOTPCode(secret, now.Unix()/30))
	if err != nil {
		t.Fatal(err)
	}
	if !status.Active || status.Owner != "owner" || status.AbsoluteExpiresAt.Sub(status.AuthenticatedAt) != 12*time.Hour || status.IdleExpiresAt.Sub(status.AuthenticatedAt) != 2*time.Hour {
		t.Fatalf("wrong owner session policy: %+v", status)
	}
}

func TestReplayAuthorizationDoesNotConsumeDestructiveConfirmation(t *testing.T){
 manager,key,now:=testSessionManager(t)
 loginAt(t,manager,key,now)
 args:=json.RawMessage(`{"host":"saturn"}`)
 decision:=manager.ReplayFor("local-operator","platform-gateway",
  "cluster__configure","saturn",args)
 if !decision.Allowed||len(manager.Pending())!=0{
  t.Fatalf("replay inspection consumed or created a confirmation: %+v",decision)
 }
 for _,tc:=range []struct{principal,client,operation,target string;args json.RawMessage}{
  {"other","platform-gateway","cluster__configure","saturn",args},
  {"local-operator","other-client","cluster__configure","saturn",args},
  {"local-operator","platform-gateway","cluster__configure","venus",args},
  {"local-operator","platform-gateway","unknown","saturn",args},
  {"local-operator","platform-gateway","cluster__configure","saturn",json.RawMessage(`[]`)},
 }{
  got:=manager.ReplayFor(tc.principal,tc.client,tc.operation,tc.target,tc.args)
  if got.Allowed{t.Fatal("invalid replay policy was authorized",tc)}
 }
 manager.Revoke()
 if manager.ReplayFor("local-operator","platform-gateway",
  "cluster__configure","saturn",args).Allowed{
  t.Fatal("revoked owner session authorized a recorded execution")
 }
}

func TestTOTPMatchesRFC6238SHA1(t *testing.T) {
	key := []byte("12345678901234567890")
	if code := TOTPCode(key, 59/30); code != "287082" {
		t.Fatalf("wrong RFC 6238 code: %s", code)
	}
	if _, err := DecodeTOTPSecret("not a totp secret"); err == nil {
		t.Fatal("accepted malformed TOTP secret")
	}
}

func TestSessionIdleAbsoluteExpirationAndRevocation(t *testing.T) {
	a, key, now := testSessionManager(t)
	loginAt(t, a, key, now)
	op := "workspace__workspace_create"
	args := json.RawMessage(`{"image":"debian:bookworm-slim"}`)
	if result := a.Authorize("untrusted-agent", op, "jupiter", args); result.Allowed {
		t.Fatal("client identity bypass")
	}
	if result := a.Authorize("platform-gateway", op, "mercury", args); result.Allowed {
		t.Fatal("resource substitution accepted")
	}
	if result := a.Authorize("platform-gateway", "cluster__delete", "saturn", args); result.Allowed {
		t.Fatal("unconfigured operation accepted")
	}
	if result := a.Authorize("platform-gateway", op, "jupiter", args); !result.Allowed {
		t.Fatal(result.Reason)
	}
	*now = now.Add(119 * time.Minute)
	if result := a.Authorize("platform-gateway", op, "jupiter", args); !result.Allowed {
		t.Fatal("idle session rejected early")
	}
	*now = now.Add(121 * time.Minute)
	if a.Status().Active {
		t.Fatal("idle session never expired")
	}
	// A fresh session must use a new authenticator counter.
	loginAt(t, a, key, now)
	for i := 0; i < 5; i++ {
		*now = now.Add(119 * time.Minute)
		if result := a.Authorize("platform-gateway", op, "jupiter", args); !result.Allowed {
			t.Fatalf("active session rejected at %v: %s", i, result.Reason)
		}
	}
	*now = now.Add(121 * time.Minute)
	if a.Status().Active {
		t.Fatal("absolute lifetime extended by activity")
	}
	*now = now.Add(30 * time.Second)
	loginAt(t, a, key, now)
	a.Revoke()
	if a.Status().Active || a.Authorize("platform-gateway", op, "jupiter", args).Allowed {
		t.Fatal("revoked session remained authorized")
	}
}

func TestSessionTOTPLockoutAndReplaySurviveRestart(t *testing.T) {
	a, key, now := testSessionManager(t)
	loginAt(t, a, key, now)
	if _, err := a.Authenticate(testOwnerPassword, TOTPCode(key, now.Unix()/30)); err == nil {
		t.Fatal("accepted reused TOTP")
	}
	a2, err := NewAdminSessions(SessionConfig{Owner: a.owner, Client: a.client, Verifier: a.verifier,
		TOTPSecret: base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(key), StateFile: a.stateFile,
		Policies: []OperationPolicy{{Operation: "test", Targets: []string{"resource"}}}})
	if err != nil {
		t.Fatal(err)
	}
	a2.now = a.now
	if a2.Status().Active {
		t.Fatal("session survived process restart")
	}
	if _, err := a2.Authenticate(testOwnerPassword, TOTPCode(key, now.Unix()/30)); err == nil {
		t.Fatal("replayed TOTP survived service restart")
	}
	for i := 0; i < 3; i++ {
		if _, err := a2.Authenticate("wrong-password", TOTPCode(key, now.Unix()/30)); err == nil {
			t.Fatal("accepted invalid password")
		}
	}
	a3, err := NewAdminSessions(SessionConfig{Owner: a.owner, Client: a.client, Verifier: a.verifier,
		TOTPSecret: base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(key), StateFile: a.stateFile,
		Policies: []OperationPolicy{{Operation: "test", Targets: []string{"resource"}}}})
	if err != nil {
		t.Fatal(err)
	}
	a3.now = a.now
	*now = now.Add(30 * time.Second)
	if _, err := a3.Authenticate(testOwnerPassword, TOTPCode(key, now.Unix()/30)); err == nil {
		t.Fatal("lockout lost on restart")
	}
	*now = now.Add(6 * time.Minute)
	if _, err := a3.Authenticate(testOwnerPassword, TOTPCode(key, now.Unix()/30)); err != nil {
		t.Fatal("valid account permanently locked", err)
	}
}

func TestDestructiveConfirmationExactSingleUse(t *testing.T) {
	a, key, now := testSessionManager(t)
	loginAt(t, a, key, now)
	operation := "cluster__configure"
	args := json.RawMessage(`{"revision":9007199254740993,"safe":true}`)
	pending := a.Authorize("platform-gateway", operation, "saturn", args)
	if pending.Allowed || pending.ConfirmationID == "" {
		t.Fatalf("missing confirmation: %+v", pending)
	}
	if a.Authorize("platform-gateway", operation, "saturn", json.RawMessage(`{"revision":9007199254740992,"safe":true}`)).Allowed {
		t.Fatal("changed integer accepted")
	}
	if a.Authorize("platform-gateway", operation, "saturn", json.RawMessage(`{"safe":true,"revision":9007199254740993}`)).ConfirmationID != pending.ConfirmationID {
		t.Fatal("equivalent arguments changed confirmation identity")
	}
	if a.Authorize("platform-gateway", operation, "saturn", json.RawMessage(`{"safe":false,"revision":9007199254740993}`)).Allowed {
		t.Fatal("changed arguments executed")
	}
	if err := a.Confirm(pending.ConfirmationID, testOwnerPassword, TOTPCode(key, now.Unix()/30)); err == nil {
		t.Fatal("replayed TOTP confirmed destructive action")
	}
	*now = now.Add(30 * time.Second)
	if err := a.Confirm(pending.ConfirmationID, testOwnerPassword, TOTPCode(key, now.Unix()/30)); err != nil {
		t.Fatal(err)
	}
	if result := a.Authorize("platform-gateway", operation, "saturn", args); !result.Allowed {
		t.Fatalf("confirmed operation rejected: %+v", result)
	}
	if result := a.Authorize("platform-gateway", operation, "saturn", args); result.Allowed || result.ConfirmationID == pending.ConfirmationID {
		t.Fatal("destructive authorization reused")
	}
	a.Revoke()
	if err := a.Confirm(pending.ConfirmationID, testOwnerPassword, TOTPCode(key, now.Unix()/30)); err == nil {
		t.Fatal("confirmation survived revocation")
	}
}

func TestUnverifiedRequestsDoNotRefreshSession(t *testing.T) {
	a, key, now := testSessionManager(t)
	loginAt(t, a, key, now)
	*now = now.Add(119 * time.Minute)
	if a.Authorize("platform-gateway", "not-in-policy", "jupiter", nil).Allowed {
		t.Fatal("unauthorized operation accepted")
	}
	*now = now.Add(2 * time.Minute)
	if a.Status().Active {
		t.Fatal("denied action extended idle lifetime")
	}
}

func TestCorruptStateOrBroadPermissionsFailClosed(t *testing.T) {
	a, _, _ := testSessionManager(t)
	cases := []struct {
		name, body string
		mode       os.FileMode
	}{
		{"invalid", "not-json", 0600},
		{"invalid-version", `{"version":0,"last_totp_step":0,"failures":0}`, 0600},
		{"public", `{"version":1,"last_totp_step":0,"failures":0}`, 0644},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(privateDir(t), "auth.json")
			if err := os.WriteFile(path, []byte(tc.body), tc.mode); err != nil {
				t.Fatal(err)
			}
			// Explicitly set permissions: the process umask may mask the public-mode fixture.
			if err := os.Chmod(path, tc.mode); err != nil { t.Fatal(err) }
			if _, err := NewAdminSessions(SessionConfig{Owner: a.owner, Client: a.client, Verifier: a.verifier,
				TOTPSecret: base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(a.totpKey), StateFile: path,
				Policies: []OperationPolicy{{Operation: "test", Targets: []string{"resource"}}}}); err == nil {
				t.Fatal("unsafe authenticator state accepted")
			}
		})
	}
}

func TestOwnerAndClientEndpointsAreSeparated(t *testing.T) {
	a, key, now := testSessionManager(t)
 verifier,assertion,principal:=accessFixture(t,"gateway-aud")
 if err:=a.SetAccessVerifier(verifier,"gateway-aud");err!=nil{t.Fatal(err)}
	owner := a.OwnerHandler()
	gateway := a.ClientHandler()
	login := `{"password":"` + testOwnerPassword + `","totp":"` + TOTPCode(key, now.Unix()/30) + `"}`
	call := func(h http.Handler, method, path, body string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(method, path, strings.NewReader(body))
  r.Header.Set("Cf-Access-Jwt-Assertion",assertion)
		if body != "" {
			r.Header.Set("Content-Type", "application/json")
		}
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		return w
	}
	if w := call(gateway, "POST", "/v1/session", login); w.Code != http.StatusMethodNotAllowed {
		t.Fatalf("gateway could authenticate owner: %d", w.Code)
	}
	if w := call(owner, "POST", "/v1/session", login); w.Code != 200 || strings.Contains(w.Body.String(), testOwnerPassword) {
		t.Fatalf("owner login failed or disclosed password: %d", w.Code)
	}
 *now=now.Add(30*time.Second)
 if _,err:=a.AuthenticateFor(principal,testOwnerPassword,TOTPCode(key,now.Unix()/30));err!=nil{t.Fatal(err)}

	request := `{"operation":"workspace__workspace_create","target":"jupiter","arguments":{"cpu":2}}`
	if w := call(owner, "POST", "/v1/authorize", request); w.Code != http.StatusNotFound {
		t.Fatalf("owner could authorize without client channel: %d", w.Code)
	}
	if w := call(gateway, "POST", "/v1/authorize", request); w.Code != 200 || !strings.Contains(w.Body.String(), `"allowed":true`) {
		t.Fatalf("authorized request failed: %s", w.Body.String())
	}
	if w := call(gateway, "DELETE", "/v1/session", ""); w.Code != 200 || a.Status().Active {
		t.Fatal("gateway could not revoke session")
	}
}

func TestConcurrentConfirmationIsSingleUse(t *testing.T) {
	a, key, now := testSessionManager(t)
	loginAt(t, a, key, now)
	args := json.RawMessage(`{"revision":"abc"}`)
	pending := a.Authorize("platform-gateway", "cluster__configure", "saturn", args)
	*now = now.Add(30 * time.Second)
	if err := a.Confirm(pending.ConfirmationID, testOwnerPassword, TOTPCode(key, now.Unix()/30)); err != nil {
		t.Fatal(err)
	}
	var successes atomic.Int32
	var wg sync.WaitGroup
	for i := 0; i < 32; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if a.Authorize("platform-gateway", "cluster__configure", "saturn", args).Allowed {
				successes.Add(1)
			}
		}()
	}
	wg.Wait()
	if successes.Load() != 1 {
		t.Fatalf("confirmed operation used %d times", successes.Load())
	}
}

func TestUnixGatewaySocket(t *testing.T) {
	a, key, now := testSessionManager(t)
 verifier,assertion,principal:=accessFixture(t,"gateway-aud")
 if err:=a.SetAccessVerifier(verifier,"gateway-aud");err!=nil{t.Fatal(err)}
 if _,err:=a.AuthenticateFor(principal,testOwnerPassword,TOTPCode(key,now.Unix()/30));err!=nil{t.Fatal(err)}
 identityCtx:=WithAccessAssertion(context.Background(),assertion)
	dir := privateDir(t)
	ownerPath := filepath.Join(dir, "owner.sock")
	clientPath := filepath.Join(dir, "client.sock")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	result := make(chan error, 1)
	clientGID := os.Getgid()
	if clientGID == 0 {
		clientGID = 1
	}
	go func() { result <- a.ServeSockets(ctx, ownerPath, clientPath, clientGID) }()
	var c *AdminClient
	for i := 0; i < 200; i++ {
		client, err := NewAdminClient(clientPath)
		if err == nil {
			_, err = client.Status(identityCtx)
			if err == nil {
				c = client
				break
			}
		}
		time.Sleep(5 * time.Millisecond)
	}
	if c == nil {
		t.Fatal("socket server did not initialize")
	}
	info, err := os.Stat(ownerPath)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0600 {
		t.Fatalf("owner socket mode %v", info.Mode().Perm())
	}
	info, err = os.Stat(clientPath)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0660 {
		t.Fatalf("gateway socket mode %v", info.Mode().Perm())
	}
	decision, err := c.Authorize(identityCtx, "workspace__workspace_create", "jupiter", json.RawMessage(`{"cpu":2}`))
	if err != nil || !decision.Allowed {
		t.Fatalf("client was denied: %+v %v", decision, err)
	}
	if err := c.Revoke(identityCtx); err != nil {
		t.Fatal(err)
	}
	if a.Status().Active {
		t.Fatal("socket revocation was not immediate")
	}
	cancel()
	select {
	case err := <-result:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("socket server failed to stop")
	}
}

func privateDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.Chmod(dir, 0700); err != nil {
		t.Fatal(err)
	}
	return dir
}

func TestConfirmationPreviewsRedactSecrets(t *testing.T) {
	a, key, now := testSessionManager(t)
	loginAt(t, a, key, now)
	args := json.RawMessage(`{"password":"private-example-value","config":{"api_key":"secret-example","force":true}}`)
	pending := a.Authorize("platform-gateway", "cluster__configure", "saturn", args)
	if pending.ConfirmationID == "" {
		t.Fatal("missing destructive confirmation")
	}
	entries := a.Pending()
	if len(entries) != 1 {
		t.Fatalf("expected one pending confirmation, got %d", len(entries))
	}
	preview := entries[0].Arguments
	if strings.Contains(preview, "private-example-value") || strings.Contains(preview, "secret-example") || !strings.Contains(preview, "[REDACTED]") {
		t.Fatalf("credential leaked through confirmation preview: %s", preview)
	}
	if entries[0].ArgumentsSHA256 == "" {
		t.Fatal("full argument digest missing")
	}
}

func TestDangerousOperationsRequireConfirmationEvenWhenNotMarked(t *testing.T) {
	a, key, now := testSessionManager(t)
	loginAt(t, a, key, now)
	// A repository merge requires confirmation even when its declarative
	// policy has an omitted destructive flag.
	decision := a.Authorize("platform-gateway", "workspace__github_merge_pull_request", "dbontr/homelab-new", json.RawMessage(`{"pr":12}`))
	if decision.Allowed || decision.ConfirmationID == "" {
		t.Fatal("dangerous operation bypassed confirmation")
	}
}
