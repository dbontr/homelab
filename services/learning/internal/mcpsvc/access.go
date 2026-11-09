package mcpsvc

import (
	"context"
	"crypto"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"math/big"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"
)

// AccessVerifier validates the signed assertion issued by the configured
// Cloudflare Access application, independently of the client's OAuth token.
type AccessVerifier struct {
	Issuer   string
	Audience string
	Owner    string
	CertsURL string
	Client   *http.Client

	mu          sync.Mutex
	keys        map[string]*rsa.PublicKey
	validUntil  time.Time
	lastAttempt time.Time
}

func AccessFromEnv() (*AccessVerifier, error) {
	issuer := os.Getenv("ADAPTIVE_TEACHING_ACCESS_ISSUER")
	audience := os.Getenv("ADAPTIVE_TEACHING_ACCESS_AUDIENCE")
	certsURL := os.Getenv("ADAPTIVE_TEACHING_ACCESS_CERTS_URL")
	ownerFile := os.Getenv("ADAPTIVE_TEACHING_ACCESS_OWNER_FILE")
	if issuer == "" && audience == "" && certsURL == "" && ownerFile == "" {
		return nil, nil
	}
	if issuer == "" || audience == "" || certsURL == "" || ownerFile == "" {
		return nil, errors.New("Access requires issuer, audience, certificate URL and owner credential")
	}
	fi, err := os.Stat(ownerFile)
	if err != nil || !fi.Mode().IsRegular() || fi.Mode().Perm()&0077 != 0 {
		return nil, errors.New("Access owner credential must be a regular, owner-only file")
	}
	data, err := os.ReadFile(ownerFile)
	if err != nil {
		return nil, errors.New("Access owner credential unavailable")
	}
	owner := strings.ToLower(strings.TrimSpace(string(data)))
	if len(owner) > 320 || strings.Count(owner, "@") != 1 || strings.ContainsAny(owner, "\x00\r\n\t ") {
		return nil, errors.New("invalid Access owner identity")
	}
	for _, raw := range []string{issuer, certsURL} {
		u, err := url.Parse(raw)
		if err != nil || u.Scheme != "https" || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
			return nil, errors.New("Access issuer and certificate endpoints must be HTTPS URLs")
		}
	}
	if len(audience) == 0 || len(audience) > 256 {
		return nil, errors.New("invalid Access audience")
	}
	return &AccessVerifier{
		Issuer: issuer, Audience: audience, Owner: owner, CertsURL: certsURL,
		Client: &http.Client{Timeout: 5 * time.Second, CheckRedirect: func(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse }},
	}, nil
}

func (v *AccessVerifier) signingKey(ctx context.Context, kid string) (*rsa.PublicKey, error) {
	v.mu.Lock()
	defer v.mu.Unlock()
	now := time.Now()
	if now.Before(v.validUntil) && v.keys[kid] != nil {
		return v.keys[kid], nil
	}
	if !v.lastAttempt.IsZero() && now.Sub(v.lastAttempt) < 15*time.Second {
		return nil, errors.New("Access signing key not available")
	}
	v.lastAttempt = now
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, v.CertsURL, nil)
	if err != nil {
		return nil, err
	}
	response, err := v.Client.Do(req)
	if err != nil {
		return nil, errors.New("Access certificate retrieval failed")
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return nil, errors.New("Access certificate endpoint unavailable")
	}
	b, err := io.ReadAll(io.LimitReader(response.Body, 256<<10))
	if err != nil {
		return nil, err
	}
	var keys struct {
		Keys []struct {
			Kty string `json:"kty"`
			Kid string `json:"kid"`
			Alg string `json:"alg"`
			Use string `json:"use"`
			N string `json:"n"`
			E string `json:"e"`
		} `json:"keys"`
	}
	if err := json.Unmarshal(b, &keys); err != nil || len(keys.Keys) == 0 || len(keys.Keys) > 32 {
		return nil, errors.New("invalid Access JWKS")
	}
	parsed := make(map[string]*rsa.PublicKey)
	for _, k := range keys.Keys {
		if k.Kty != "RSA" || k.Kid == "" || k.N == "" || k.E == "" ||
			(k.Alg != "" && k.Alg != "RS256") || (k.Use != "" && k.Use != "sig") {
			continue
		}
		n, ne := base64.RawURLEncoding.DecodeString(k.N)
		e, ee := base64.RawURLEncoding.DecodeString(k.E)
		if ne != nil || ee != nil || len(n) < 256 || len(n) > 512 || len(e) == 0 || len(e) > 4 {
			continue
		}
		exponent := new(big.Int).SetBytes(e)
		if !exponent.IsInt64() || exponent.Int64() < 3 || exponent.Int64() > 65537 || exponent.Int64()%2 == 0 {
			continue
		}
		parsed[k.Kid] = &rsa.PublicKey{N: new(big.Int).SetBytes(n), E: int(exponent.Int64())}
	}
	if len(parsed) == 0 {
		return nil, errors.New("Access JWKS has no supported keys")
	}
	v.keys, v.validUntil = parsed, now.Add(5*time.Minute)
	if key := parsed[kid]; key != nil {
		return key, nil
	}
	return nil, errors.New("Access signing key not found")
}

func accessAudience(v any, audience string) bool {
	switch a := v.(type) {
	case string:
		return a == audience
	case []any:
		for _, item := range a {
			if item == audience { return true }
		}
	}
	return false
}

func (v *AccessVerifier) Verify(ctx context.Context, token string) error {
	if v == nil || len(token) == 0 || len(token) > 16<<10 {
		return errors.New("missing Access assertion")
	}
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return errors.New("invalid Access JWT")
	}
	head, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil { return errors.New("invalid Access header") }
	var h struct {
		Alg string `json:"alg"`
		Kid string `json:"kid"`
	}
	if json.Unmarshal(head, &h) != nil || h.Alg != "RS256" || len(h.Kid) == 0 || len(h.Kid) > 256 {
		return errors.New("unsupported Access signature")
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil { return errors.New("invalid Access claims") }
	var claims struct {
		Issuer string `json:"iss"`
		Audience any `json:"aud"`
		Email string `json:"email"`
		Subject string `json:"sub"`
		Expires float64 `json:"exp"`
		Issued float64 `json:"iat"`
		NotBefore float64 `json:"nbf"`
	}
	if json.Unmarshal(payload, &claims) != nil { return errors.New("invalid Access claims") }
	now := float64(time.Now().Unix())
	email := strings.ToLower(strings.TrimSpace(claims.Email))
	if claims.Issuer != v.Issuer || !accessAudience(claims.Audience, v.Audience) || email != v.Owner ||
		claims.Subject == "" || claims.Expires <= now || claims.Expires > now+15*24*3600 ||
		claims.Issued > now+30 || claims.NotBefore > now+30 {
		return errors.New("invalid Access identity, audience or expiration")
	}
	sig, err := base64.RawURLEncoding.DecodeString(parts[2])
	if err != nil { return errors.New("invalid Access signature") }
	key, err := v.signingKey(ctx, h.Kid)
	if err != nil { return err }
	digest := sha256.Sum256([]byte(parts[0]+"."+parts[1]))
	if rsa.VerifyPKCS1v15(key, crypto.SHA256, digest[:], sig) != nil {
		return errors.New("Access signature verification failed")
	}
	return nil
}
