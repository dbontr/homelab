package verification

import (
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"
)

const Domain = "adaptive-teaching/problem-verification/v1\n"

var verifierIDPattern = regexp.MustCompile(`^[a-z][a-z0-9]*(?:-[a-z0-9]+)*$`)

// Receipt binds a third-party report to the exact saved learner submission.
type Receipt struct {
	Version           int    `json:"version"`
	VerifierID        string `json:"verifier_id"`
	ProblemID         string `json:"problem_id"`
	ProblemSHA256     string `json:"problem_sha256"`
	DraftID           string `json:"draft_id"`
	DraftSHA256       string `json:"draft_sha256"`
	Method            string `json:"method"`
	Outcome           string `json:"outcome"`
	EvidenceReference string `json:"evidence_reference"`
	EvidenceSHA256    string `json:"evidence_sha256"`
	Toolchain         string `json:"toolchain"`
	CheckedAt         string `json:"checked_at"`
}

func Digest(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}
func validDigest(s string) bool {
	if len(s) != sha256.Size*2 {
		return false
	}
	b, e := hex.DecodeString(s)
	return e == nil && hex.EncodeToString(b) == s
}

// Message produces the canonical domain-separated bytes authenticated by Ed25519.
func Message(r Receipt) ([]byte, error) {
	if r.Version != 1 {
		return nil, errors.New("unsupported verification receipt version")
	}
	if len(r.VerifierID) > 80 || !verifierIDPattern.MatchString(r.VerifierID) {
		return nil, errors.New("invalid verifier identifier")
	}
	if r.ProblemID == "" || len(r.ProblemID) > 128 || r.DraftID == "" || len(r.DraftID) > 128 {
		return nil, errors.New("verification receipt must identify the exact problem and draft")
	}
	switch r.Method {
	case "symbolic", "formal", "executable", "experimental", "expert":
	default:
		return nil, errors.New("verification method must not be model-only")
	}
	switch r.Outcome {
	case "pass", "fail", "inconclusive":
	default:
		return nil, errors.New("invalid verification outcome")
	}
	if !validDigest(r.ProblemSHA256) || !validDigest(r.DraftSHA256) || !validDigest(r.EvidenceSHA256) {
		return nil, errors.New("verification digests must be lowercase SHA-256 hex values")
	}
	if strings.TrimSpace(r.EvidenceReference) == "" || len(r.EvidenceReference) > 2048 {
		return nil, errors.New("verification evidence reference is missing or too long")
	}
	if strings.TrimSpace(r.Toolchain) == "" || len(r.Toolchain) > 256 {
		return nil, errors.New("verification toolchain identifier is missing or too long")
	}
	when, e := time.Parse(time.RFC3339Nano, r.CheckedAt)
	if e != nil || when.After(time.Now().Add(5*time.Minute)) {
		return nil, errors.New("invalid or future verification timestamp")
	}
	b, e := json.Marshal(r)
	if e != nil {
		return nil, fmt.Errorf("encode verification receipt: %w", e)
	}
	if len(b) > 4096 {
		return nil, errors.New("verification receipt is too large")
	}
	return append([]byte(Domain), b...), nil
}
func Sign(r Receipt, key ed25519.PrivateKey) (string, error) {
	if len(key) != ed25519.PrivateKeySize {
		return "", errors.New("invalid Ed25519 private key")
	}
	b, e := Message(r)
	if e != nil {
		return "", e
	}
	return base64.StdEncoding.EncodeToString(ed25519.Sign(key, b)), nil
}
func Verify(r Receipt, signature string, key ed25519.PublicKey) bool {
	b, e := Message(r)
	if e != nil || len(key) != ed25519.PublicKeySize {
		return false
	}
	sig, e := base64.StdEncoding.DecodeString(signature)
	if e != nil || len(sig) != ed25519.SignatureSize || base64.StdEncoding.EncodeToString(sig) != signature {
		return false
	}
	return ed25519.Verify(key, b, sig)
}
