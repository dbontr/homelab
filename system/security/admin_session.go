package security

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha1"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base32"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/dbontr/homelab-new/system/canonicaljson"
)

const (
	SessionIdleTimeout      = 120 * time.Minute
	SessionAbsoluteTimeout  = 12 * time.Hour
	ConfirmationTimeout     = 5 * time.Minute
	ConfirmedLifetime       = 2 * time.Minute
	maxPendingConfirmations = 64
)

var allowedIdentifier = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_./:-]{0,127}$`)

// OperationPolicy defines a finite set of allowed actions and resource identities.
// A privileged executor must apply the same policy before touching a resource.
type OperationPolicy struct {
	Operation   string   `json:"operation"`
	Targets     []string `json:"targets"`
	Destructive bool     `json:"destructive"`
}

type SessionConfig struct {
	Owner      string
	Client     string
	Verifier   PassphraseRecord
	TOTPSecret string
	StateFile  string
	Policies   []OperationPolicy
}

type SessionStatus struct {
	Active            bool      `json:"active"`
	Owner             string    `json:"owner,omitempty"`
	AuthenticatedAt   time.Time `json:"authenticated_at,omitempty"`
	LastActivityAt    time.Time `json:"last_activity_at,omitempty"`
	IdleExpiresAt     time.Time `json:"idle_expires_at,omitempty"`
	AbsoluteExpiresAt time.Time `json:"absolute_expires_at,omitempty"`
}

type Authorization struct {
	Allowed               bool      `json:"allowed"`
	Reason                string    `json:"reason,omitempty"`
	ConfirmationID        string    `json:"confirmation_id,omitempty"`
	ConfirmationExpiresAt time.Time `json:"confirmation_expires_at,omitempty"`
}

type PendingConfirmation struct {
	ID              string    `json:"id"`
	Operation       string    `json:"operation"`
	Target          string    `json:"target"`
	ArgumentsSHA256 string    `json:"arguments_sha256"`
	Arguments       string    `json:"arguments"`
	ExpiresAt       time.Time `json:"expires_at"`
	Confirmed       bool      `json:"confirmed"`
}

type activeSession struct {
	principal string
	id           string
	created      time.Time
	lastActivity time.Time
}

type confirmation struct {
	PendingConfirmation
	sessionID string
}

type authenticatorState struct {
	Version      int       `json:"version"`
	LastTOTPStep int64     `json:"last_totp_step"`
	Failures     int       `json:"failures"`
	LockedUntil  time.Time `json:"locked_until"`
}

// AdminSessions owns authentication, authorization and revocation state.
// Session identity and TOTP secrets never leave the security process.
type AdminSessions struct {
	mu        sync.Mutex
	owner     string
	client    string
	access *AccessVerifier
	gatewayAudience string
	verifier  PassphraseRecord
	totpKey   []byte
	stateFile string
	state     authenticatorState
	rules     map[string]OperationPolicy
	session   *activeSession
	pending   map[string]confirmation
	now       func() time.Time
	faulted   bool
}

func NewAdminSessions(cfg SessionConfig) (*AdminSessions, error) {
	if !allowedIdentifier.MatchString(cfg.Owner) || !allowedIdentifier.MatchString(cfg.Client) {
		return nil, errors.New("invalid owner or client identity")
	}
	salt, saltErr := decodePassphraseField(cfg.Verifier.Salt, 24)
	digest, digestErr := decodePassphraseField(cfg.Verifier.Digest, 32)
	if cfg.Verifier.Version != 1 || cfg.Verifier.Iterations < passphraseIterations || cfg.Verifier.Iterations > 2_000_000 || saltErr != nil || digestErr != nil {
		return nil, errors.New("invalid owner password verifier")
	}
	_ = salt
	_ = digest
	key, err := DecodeTOTPSecret(cfg.TOTPSecret)
	if err != nil {
		return nil, err
	}
	if !filepath.IsAbs(cfg.StateFile) || strings.ContainsRune(cfg.StateFile, 0) {
		return nil, errors.New("state path must be absolute")
	}
	if len(cfg.Policies) == 0 || len(cfg.Policies) > 256 {
		return nil, errors.New("at least one bounded operation policy is required")
	}
	rules := make(map[string]OperationPolicy, len(cfg.Policies))
	for _, rule := range cfg.Policies {
		if !allowedIdentifier.MatchString(rule.Operation) || len(rule.Targets) == 0 || len(rule.Targets) > 64 {
			return nil, errors.New("invalid operation policy")
		}
		if _, exists := rules[rule.Operation]; exists {
			return nil, errors.New("duplicate operation policy")
		}
		targets := make(map[string]bool, len(rule.Targets))
		for _, target := range rule.Targets {
			if !allowedIdentifier.MatchString(target) || targets[target] {
				return nil, errors.New("invalid policy resource")
			}
			targets[target] = true
		}
		// Dangerous operation names require explicit confirmation even when
		// the policy omits its destructive flag.
		rule.Destructive = rule.Destructive || dangerousOperation(rule.Operation)
		rules[rule.Operation] = rule
	}
	manager := &AdminSessions{owner: cfg.Owner, client: cfg.Client, verifier: cfg.Verifier, totpKey: key, stateFile: cfg.StateFile,
		rules: rules, pending: make(map[string]confirmation), now: time.Now, state: authenticatorState{Version: 1, LastTOTPStep: -1}}
	if err := manager.loadState(); err != nil {
		return nil, err
	}
	return manager, nil
}

func base64RawDecode(raw string) ([]byte, error) { return base64.RawStdEncoding.DecodeString(raw) }

func decodePassphraseField(raw string, length int) ([]byte, error) {
	decoded, err := base64RawDecode(raw)
	if err != nil || len(decoded) != length {
		return nil, errors.New("invalid passphrase field")
	}
	return decoded, nil
}

func dangerousOperation(name string) bool {
	// These administrative actions cannot become routine through a config typo.
	tokens := strings.FieldsFunc(strings.ToLower(name), func(r rune) bool { return r == '.' || r == '_' || r == '-' || r == '/' })
	for _, word := range tokens {
		switch word {
		case "delete", "destroy", "purge", "revoke", "rotate", "reset", "remove", "wipe", "drop", "format", "shutdown", "reboot", "restart", "activate", "switch", "prune", "firewall", "router", "network", "credential", "credentials", "trust", "rollback", "reinstall", "merge", "deploy", "apply", "force", "configure", "cleanup", "restore", "grant", "update", "patch":
			return true
		}
	}
	return false
}

// DecodeTOTPSecret accepts a Base32 RFC 6238 secret, not an OTP code.
func DecodeTOTPSecret(raw string) ([]byte, error) {
	raw = strings.ToUpper(strings.TrimSpace(raw))
	if len(raw) < 32 || len(raw) > 208 {
		return nil, errors.New("invalid TOTP secret length")
	}
	key, err := base32.StdEncoding.WithPadding(base32.NoPadding).DecodeString(raw)
	if err != nil || len(key) < 20 || len(key) > 128 {
		return nil, errors.New("invalid TOTP secret")
	}
	return key, nil
}

// TOTPCode calculates a six-digit HMAC-SHA1 code for the given time step.
// The HMAC-SHA1 algorithm and 30-second counter follow RFC 6238.
func TOTPCode(key []byte, step int64) string {
	var counter [8]byte
	binary.BigEndian.PutUint64(counter[:], uint64(step))
	mac := hmac.New(sha1.New, key)
	_, _ = mac.Write(counter[:])
	digest := mac.Sum(nil)
	offset := int(digest[len(digest)-1] & 15)
	value := binary.BigEndian.Uint32(digest[offset:offset+4]) & 0x7fffffff
	return fmt.Sprintf("%06d", value%1_000_000)
}

func (a *AdminSessions) totpStep(code string, now time.Time) (int64, bool) {
	if len(code) != 6 {
		return 0, false
	}
	for _, r := range code {
		if r < '0' || r > '9' {
			return 0, false
		}
	}
	counter := now.Unix() / 30
	// Accept one past or future step to tolerate minor clock drift.
	for _, step := range []int64{counter - 1, counter, counter + 1} {
		generated := TOTPCode(a.totpKey, step)
		if subtle.ConstantTimeCompare([]byte(code), []byte(generated)) == 1 && step > a.state.LastTOTPStep {
			return step, true
		}
	}
	return 0, false
}

func (a *AdminSessions) authenticateLocked(password, code string, now time.Time) error {
	if a.faulted {
		return errors.New("authenticator storage unavailable")
	}
	if now.Before(a.state.LockedUntil) {
		return errors.New("authentication temporarily unavailable")
	}
	// Verify both factors before reporting a result, without identifying
	// which factor was incorrect.
	passwordOK := a.verifier.Verify([]byte(password))
	step, codeOK := a.totpStep(code, now)
	next := a.state
	if !passwordOK || !codeOK {
		next.Failures++
		if next.Failures >= 5 {
			next.Failures = 0
			next.LockedUntil = now.Add(5 * time.Minute)
		}
		if err := a.saveState(next); err != nil {
			a.faulted = true
			a.session = nil
			a.pending = make(map[string]confirmation)
			return err
		}
		a.state = next
		return errors.New("authentication denied")
	}
	next.Failures = 0
	next.LockedUntil = time.Time{}
	next.LastTOTPStep = step
	if err := a.saveState(next); err != nil {
		a.faulted = true
		a.session = nil
		a.pending = make(map[string]confirmation)
		return err
	}
	a.state = next
	return nil
}

func randomID() (string, error) {
	data := make([]byte, 24)
	if _, err := rand.Read(data); err != nil {
		return "", err
	}
	return hex.EncodeToString(data), nil
}

// SetAccessVerifier installs immutable trust configuration before serving clients.
// A missing verifier always denies gateway-origin authorization.
func (a *AdminSessions) SetAccessVerifier(v *AccessVerifier,audience string) error {
 if v==nil||audience==""||len(audience)>256{return errors.New("independent Access verifier and audience required")}
 a.mu.Lock()
 defer a.mu.Unlock()
 a.access=v
 a.gatewayAudience=audience
 return nil
}

func (a *AdminSessions) VerifiedGatewayPrincipal(assertion string)(string,error){
 a.mu.Lock()
 verifier,audience:=a.access,a.gatewayAudience
 a.mu.Unlock()
 if verifier==nil{return "",errors.New("Access verifier unavailable")}
 return verifier.Verify(assertion,audience)
}

func (a *AdminSessions) Authenticate(password, code string) (SessionStatus, error) {
 return a.AuthenticateFor("local-operator",password,code)
}

// AuthenticateFor binds a browser login to a separately verified human identity.
// The principal is derived from a validated signed assertion, never a request field.
func (a *AdminSessions) AuthenticateFor(principal,password, code string) (SessionStatus, error) {
 if principal==""||len(principal)>1024{return SessionStatus{},errors.New("verified owner identity required")}
	a.mu.Lock()
	defer a.mu.Unlock()
	now := a.now().UTC()
	if err := a.authenticateLocked(password, code, now); err != nil {
		return SessionStatus{}, err
	}
	id, err := randomID()
	if err != nil {
		return SessionStatus{}, err
	}
	a.session = &activeSession{principal:principal,id: id, created: now, lastActivity: now}
	a.pending = make(map[string]confirmation)
	return a.statusLocked(now), nil
}

func (a *AdminSessions) validSession(now time.Time) bool {
	s := a.session
	if s == nil || a.faulted {
		return false
	}
	if now.Before(s.lastActivity) || !now.Before(s.lastActivity.Add(SessionIdleTimeout)) || !now.Before(s.created.Add(SessionAbsoluteTimeout)) {
		a.session = nil
		a.pending = make(map[string]confirmation)
		return false
	}
	return true
}

func (a *AdminSessions) statusLocked(now time.Time) SessionStatus {
	if !a.validSession(now) {
		return SessionStatus{Active: false}
	}
	s := a.session
	return SessionStatus{Active: true, Owner: a.owner, AuthenticatedAt: s.created, LastActivityAt: s.lastActivity,
		IdleExpiresAt: s.lastActivity.Add(SessionIdleTimeout), AbsoluteExpiresAt: s.created.Add(SessionAbsoluteTimeout)}
}

func (a *AdminSessions) Status() SessionStatus {
 a.mu.Lock()
 defer a.mu.Unlock()
 return a.statusLocked(a.now().UTC())
}

func (a *AdminSessions) StatusFor(principal string) SessionStatus {
 a.mu.Lock()
 defer a.mu.Unlock()
 status:=a.statusLocked(a.now().UTC())
 if !status.Active||principal==""||a.session.principal!=principal{return SessionStatus{Active:false}}
 return status
}

func (a *AdminSessions) RevokeFor(principal string) bool {
 a.mu.Lock()
 defer a.mu.Unlock()
 if !a.validSession(a.now().UTC())||principal==""||a.session.principal!=principal{return false}
 a.session=nil
 a.pending=make(map[string]confirmation)
 return true
}

func (a *AdminSessions) PendingFor(principal string) []PendingConfirmation {
 a.mu.Lock()
 defer a.mu.Unlock()
 now:=a.now().UTC()
 if !a.validSession(now)||principal==""||a.session.principal!=principal{return nil}
 a.pruneConfirmations(now)
 out:=make([]PendingConfirmation,0,len(a.pending))
 for _,p:=range a.pending{out=append(out,p.PendingConfirmation)}
 return out
}

// Revoke immediately invalidates the entire session and every confirmation.
// Neither an assistant nor a gateway can extend or recreate this session.
func (a *AdminSessions) Revoke() {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.session = nil
	a.pending = make(map[string]confirmation)
}

func (a *AdminSessions) Pending() []PendingConfirmation {
	a.mu.Lock()
	defer a.mu.Unlock()
	now := a.now().UTC()
	if !a.validSession(now) {
		return nil
	}
	a.pruneConfirmations(now)
	out := make([]PendingConfirmation, 0, len(a.pending))
	for _, item := range a.pending {
		out = append(out, item.PendingConfirmation)
	}
	return out
}

func (a *AdminSessions) pruneConfirmations(now time.Time) {
	for id, item := range a.pending {
		if !now.Before(item.ExpiresAt) {
			delete(a.pending, id)
		}
	}
}

// Authorize revalidates the live session for every attempt. Confirmation
// approvals are consumed atomically by an exactly matching request.
func (a *AdminSessions) Authorize(client,operation,target string,args json.RawMessage) Authorization{
 return a.AuthorizeFor("local-operator",client,operation,target,args)
}

// AuthorizeFor checks the identity bound to the active session on each call.
func (a *AdminSessions) AuthorizeFor(principal,client,operation,target string,args json.RawMessage) Authorization {
 a.mu.Lock()
 defer a.mu.Unlock()
 now:=a.now().UTC()
	if client != a.client {
		return Authorization{Reason: "unauthorized client"}
	}
	if !a.validSession(now)||principal==""||a.session.principal!=principal {
		return Authorization{Reason: "owner authentication required"}
	}
	rule, ok := a.rules[operation]
	if !ok {
		return Authorization{Reason: "operation not permitted"}
	}
	targetOK := false
	for _, allowed := range rule.Targets {
		if allowed == target {
			targetOK = true
			break
		}
	}
	if !targetOK {
		return Authorization{Reason: "resource not permitted"}
	}
	canonical, _, err := canonicaljson.Object(args)
	if err != nil {
		return Authorization{Reason: "invalid operation arguments"}
	}
	sum := sha256.Sum256(canonical)
	digest := hex.EncodeToString(sum[:])
	if !rule.Destructive {
		a.session.lastActivity = now
		return Authorization{Allowed: true}
	}
	a.pruneConfirmations(now)
	for id, item := range a.pending {
		if item.sessionID == a.session.id && item.Operation == operation && item.Target == target && item.ArgumentsSHA256 == digest {
			if item.Confirmed {
				delete(a.pending, id)
				a.session.lastActivity = now
				return Authorization{Allowed: true}
			}
			return Authorization{Reason: "confirmation required", ConfirmationID: id, ConfirmationExpiresAt: item.ExpiresAt}
		}
	}
	if len(a.pending) >= maxPendingConfirmations {
		return Authorization{Reason: "confirmation capacity exhausted"}
	}
	id, err := randomID()
	if err != nil {
		return Authorization{Reason: "confirmation unavailable"}
	}
	expires := now.Add(ConfirmationTimeout)
	if absolute := a.session.created.Add(SessionAbsoluteTimeout); expires.After(absolute) {
		expires = absolute
	}
	a.pending[id] = confirmation{PendingConfirmation: PendingConfirmation{ID: id, Operation: operation, Target: target,
		ArgumentsSHA256: digest, Arguments: redactArguments(canonical), ExpiresAt: expires}, sessionID: a.session.id}
	return Authorization{Reason: "confirmation required", ConfirmationID: id, ConfirmationExpiresAt: expires}
}

// Confirm requires a second owner-only password and a fresh, unreplayed TOTP.
// The client cannot issue or confirm its own challenges.
func (a *AdminSessions) Confirm(id,password,code string) error{
 return a.ConfirmFor("local-operator",id,password,code)
}

// ConfirmFor requires a challenge belonging to the verified browser principal.
func (a *AdminSessions) ConfirmFor(principal,id,password,code string) error {
 a.mu.Lock()
 defer a.mu.Unlock()
 now:=a.now().UTC()
	if !a.validSession(now)||principal==""||a.session.principal!=principal {
		return errors.New("owner authentication required")
	}
	a.pruneConfirmations(now)
	challenge, ok := a.pending[id]
	if !ok || challenge.sessionID != a.session.id || challenge.Confirmed {
		return errors.New("confirmation unavailable")
	}
	if err := a.authenticateLocked(password, code, now); err != nil {
		return err
	}
	challenge.Confirmed = true
	if expires := now.Add(ConfirmedLifetime); expires.Before(challenge.ExpiresAt) {
		challenge.ExpiresAt = expires
	}
	a.pending[id] = challenge
	a.session.lastActivity = now
	return nil
}

// Owner review displays only a bounded, redacted preview. The digest always
// covers the complete original arguments, including redacted values.
func redactArguments(canonical []byte) string {
	var value any
	if err := json.Unmarshal(canonical, &value); err != nil {
		return "[invalid arguments]"
	}
	var redact func(any) any
	redact = func(input any) any {
		switch x := input.(type) {
		case map[string]any:
			out := make(map[string]any, len(x))
			for name, item := range x {
				lower := strings.ToLower(name)
				private := false
				for _, field := range []string{"password", "passphrase", "token", "secret", "credential", "api_key", "cookie", "private_key", "otp", "totp", "authorization", "session"} {
					if strings.Contains(lower, field) {
						private = true
						break
					}
				}
				if private {
					out[name] = "[REDACTED]"
				} else {
					out[name] = redact(item)
				}
			}
			return out
		case []any:
			out := make([]any, len(x))
			for i, item := range x {
				out[i] = redact(item)
			}
			return out
		default:
			return input
		}
	}
	payload, err := json.Marshal(redact(value))
	if err != nil {
		return "[redacted]"
	}
	if len(payload) > 2048 {
		return string(payload[:2048]) + "..."
	}
	return string(payload)
}

func (a *AdminSessions) loadState() error {
	dir := filepath.Dir(a.stateFile)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return err
	}
	dirInfo, err := os.Lstat(dir)
	if err != nil || !dirInfo.IsDir() || dirInfo.Mode().Perm()&0077 != 0 {
		return errors.New("security state directory must be private")
	}
	pathInfo, err := os.Lstat(a.stateFile)
	if err == nil && (!pathInfo.Mode().IsRegular() || pathInfo.Mode().Perm()&0077 != 0) {
		return errors.New("security state file must be private")
	}
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	f, err := os.Open(a.stateFile)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() || info.Mode().Perm()&0077 != 0 {
		return errors.New("security state file must be private")
	}
	payload, err := io.ReadAll(io.LimitReader(f, 16<<10))
	if err != nil {
		return err
	}
	if len(payload) >= 16<<10 {
		return errors.New("security state exceeds size limit")
	}
	var persisted authenticatorState
	if err := json.Unmarshal(payload, &persisted); err != nil {
		return errors.New("invalid authenticator state")
	}
	if persisted.Version != 1 || persisted.LastTOTPStep < -1 || persisted.Failures < 0 || persisted.Failures > 4 {
		return errors.New("invalid authenticator state")
	}
	a.state = persisted
	return nil
}

func (a *AdminSessions) saveState(next authenticatorState) error {
	payload, err := json.Marshal(next)
	if err != nil {
		return err
	}
	dir := filepath.Dir(a.stateFile)
	f, err := os.CreateTemp(dir, ".auth-state-*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if err := f.Chmod(0600); err != nil {
		_ = f.Close()
		return err
	}
	if _, err := f.Write(payload); err != nil {
		_ = f.Close()
		return err
	}
	if err := f.Sync(); err != nil {
		_ = f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	if err := os.Rename(f.Name(), a.stateFile); err != nil {
		return err
	}
	d, err := os.Open(dir)
	if err != nil {
		return err
	}
	defer d.Close()
	return d.Sync()
}
