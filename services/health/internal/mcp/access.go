package mcp

import (
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

// AccessVerifier independently validates the signed identity asserted by Cloudflare Access.
type AccessVerifier struct {
	issuer, audience, owner, certsURL string
	client                            *http.Client
	mu                                sync.Mutex
	keys                              map[string]*rsa.PublicKey
	validUntil                        time.Time
	lastFetch                         time.Time
}

func AccessVerifierFromEnvironment() (*AccessVerifier, error) {
	issuer := strings.TrimSpace(os.Getenv("PERSONAL_HEALTH_ACCESS_ISSUER"))
	audience := strings.TrimSpace(os.Getenv("PERSONAL_HEALTH_ACCESS_AUDIENCE"))
	certs := strings.TrimSpace(os.Getenv("PERSONAL_HEALTH_ACCESS_CERTS_URL"))
	ownerFile := strings.TrimSpace(os.Getenv("PERSONAL_HEALTH_ACCESS_OWNER_FILE"))
	if issuer == "" && audience == "" && certs == "" && ownerFile == "" {
		return nil, nil
	}
	if issuer == "" || audience == "" || certs == "" || ownerFile == "" {
		return nil, errors.New("Access requires issuer, audience, certificates URL, and owner file")
	}
	data, err := os.ReadFile(ownerFile)
	if err != nil {
		return nil, errors.New("cannot read Access owner credential")
	}
	return newAccessVerifier(issuer, audience, certs, strings.TrimSpace(string(data)), nil)
}

func newAccessVerifier(issuer, audience, certs, owner string, client *http.Client) (*AccessVerifier, error) {
	issuerURL, issueErr := url.Parse(issuer)
	certsURL, certErr := url.Parse(certs)
	if issueErr != nil || certErr != nil || issuerURL.Scheme != "https" ||
		issuerURL.Host == "" || issuerURL.User != nil || issuerURL.RawQuery != "" ||
		issuerURL.Fragment != "" || certsURL.Scheme != "https" ||
		certsURL.Host != issuerURL.Host || certsURL.User != nil ||
		certsURL.Path != "/cdn-cgi/access/certs" || certsURL.RawQuery != "" ||
		certsURL.Fragment != "" || len(audience) > 256 {
		return nil, errors.New("invalid Access issuer, audience, or certificates URL")
	}
	owner = strings.ToLower(strings.TrimSpace(owner))
	if strings.Count(owner, "@") != 1 || len(owner) > 320 ||
		strings.ContainsAny(owner, "\x00\r\n\t ") {
		return nil, errors.New("invalid Access owner identity")
	}
	if client == nil {
		client = &http.Client{
			Timeout: 5 * time.Second,
			CheckRedirect: func(*http.Request, []*http.Request) error {
				return http.ErrUseLastResponse
			},
		}
	}
	return &AccessVerifier{issuer: issuer, audience: audience, owner: owner, certsURL: certs, client: client}, nil
}

func (v *AccessVerifier) signingKey(kid string) (*rsa.PublicKey, error) {
	v.mu.Lock()
	defer v.mu.Unlock()
	now := time.Now()
	if now.Before(v.validUntil) && v.keys[kid] != nil {
		return v.keys[kid], nil
	}
	if !v.lastFetch.IsZero() && now.Sub(v.lastFetch) < 15*time.Second {
		return nil, errors.New("Access signing key unavailable")
	}
	v.lastFetch = now
	resp, err := v.client.Get(v.certsURL)
	if err != nil {
		return nil, errors.New("Access certificate retrieval failed")
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, errors.New("Access certificate service unavailable")
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, 128<<10))
	if err != nil {
		return nil, errors.New("Access certificate read failed")
	}
	var jwks struct {
		Keys []struct {
			Kty string `json:"kty"`
			Kid string `json:"kid"`
			Alg string `json:"alg"`
			Use string `json:"use"`
			N   string `json:"n"`
			E   string `json:"e"`
		} `json:"keys"`
	}
	if err := json.Unmarshal(data, &jwks); err != nil || len(jwks.Keys) == 0 || len(jwks.Keys) > 32 {
		return nil, errors.New("invalid Access certificates")
	}
	keys := make(map[string]*rsa.PublicKey)
	for _, key := range jwks.Keys {
		if key.Kty != "RSA" || key.Kid == "" || key.N == "" || key.E == "" ||
			(key.Alg != "" && key.Alg != "RS256") || (key.Use != "" && key.Use != "sig") {
			continue
		}
		n, nerr := base64.RawURLEncoding.DecodeString(key.N)
		e, eerr := base64.RawURLEncoding.DecodeString(key.E)
		if nerr != nil || eerr != nil || len(n) < 256 || len(n) > 512 || len(e) == 0 || len(e) > 4 {
			continue
		}
		exponent := new(big.Int).SetBytes(e)
		if !exponent.IsInt64() || exponent.Int64() < 3 || exponent.Int64() > 65537 || exponent.Int64()%2 == 0 {
			continue
		}
		keys[key.Kid] = &rsa.PublicKey{N: new(big.Int).SetBytes(n), E: int(exponent.Int64())}
	}
	if len(keys) == 0 {
		return nil, errors.New("no valid Access signing keys")
	}
	v.keys, v.validUntil = keys, now.Add(5*time.Minute)
	key := keys[kid]
	if key == nil {
		return nil, errors.New("Access signing key not found")
	}
	return key, nil
}

func audienceMatches(raw json.RawMessage, expected string) bool {
	var single string
	if json.Unmarshal(raw, &single) == nil {
		return single == expected
	}
	var list []string
	if json.Unmarshal(raw, &list) != nil {
		return false
	}
	for _, audience := range list {
		if audience == expected {
			return true
		}
	}
	return false
}

// Verify rejects expired, wrongly scoped, incorrectly signed, and non-owner assertions.
func (v *AccessVerifier) Verify(token string) error {
	if v == nil || len(token) < 64 || len(token) > 16<<10 {
		return errors.New("invalid Access token")
	}
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return errors.New("invalid Access token structure")
	}
	headerBytes, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return errors.New("invalid Access token header")
	}
	var header struct {
		Alg string `json:"alg"`
		Kid string `json:"kid"`
	}
	if json.Unmarshal(headerBytes, &header) != nil || header.Alg != "RS256" ||
		header.Kid == "" || len(header.Kid) > 256 {
		return errors.New("unsupported Access signature")
	}
	body, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return errors.New("invalid Access claims")
	}
	var claims struct {
		Iss   string          `json:"iss"`
		Aud   json.RawMessage `json:"aud"`
		Sub   string          `json:"sub"`
		Email string          `json:"email"`
		Exp   int64           `json:"exp"`
		Nbf   *int64          `json:"nbf"`
		Iat   *int64          `json:"iat"`
	}
	if json.Unmarshal(body, &claims) != nil {
		return errors.New("invalid Access claims")
	}
	now := time.Now().Unix()
	if claims.Iss != v.issuer || !audienceMatches(claims.Aud, v.audience) ||
		claims.Sub == "" || claims.Exp <= now || claims.Exp > now+15*24*3600 ||
		strings.ToLower(strings.TrimSpace(claims.Email)) != v.owner ||
		(claims.Nbf != nil && *claims.Nbf > now+30) ||
		(claims.Iat != nil && *claims.Iat > now+30) {
		return errors.New("Access identity or expiry rejected")
	}
	key, err := v.signingKey(header.Kid)
	if err != nil {
		return err
	}
	signature, err := base64.RawURLEncoding.DecodeString(parts[2])
	if err != nil {
		return errors.New("invalid Access signature encoding")
	}
	sum := sha256.Sum256([]byte(parts[0] + "." + parts[1]))
	if rsa.VerifyPKCS1v15(key, crypto.SHA256, sum[:], signature) != nil {
		return errors.New("Access signature verification failed")
	}
	return nil
}
