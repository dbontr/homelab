package store

import (
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/dbontr/Adaptive-Teaching-MCP/internal/verification"
	"strings"
	"time"
)

type VerificationReceipt = verification.Receipt

func verifierKeys(raw string) (map[string]ed25519.PublicKey, error) {
	keys := map[string]ed25519.PublicKey{}
	if strings.TrimSpace(raw) == "" {
		return keys, nil
	}
	var configured map[string]string
	if err := json.Unmarshal([]byte(raw), &configured); err != nil {
		return nil, errors.New("verifier keys must be a JSON object")
	}
	if len(configured) == 0 {
		return nil, errors.New("verifier key configuration must not be empty")
	}
	for id, encoded := range configured {
		if len(id) > 80 || !slug.MatchString(id) {
			return nil, errors.New("invalid verifier identifier")
		}
		key, err := base64.StdEncoding.DecodeString(encoded)
		if err != nil || len(key) != ed25519.PublicKeySize || base64.StdEncoding.EncodeToString(key) != encoded {
			return nil, fmt.Errorf("invalid Ed25519 public key for verifier %q", id)
		}
		keys[id] = ed25519.PublicKey(key)
	}
	return keys, nil
}

func verificationMessage(receipt VerificationReceipt) ([]byte, error) {
	return verification.Message(receipt)
}

// RecordVerifiedProblemCheck accepts only a signature from a verifier key
// provided by the operator. Learner and model claims use RecordProblemCheck.
func (s *Store) RecordVerifiedProblemCheck(receipt VerificationReceipt, signature string) (Value, error) {
	key := s.trustedVerifiers[receipt.VerifierID]
	if len(key) != ed25519.PublicKeySize {
		return nil, errors.New("verification issuer is not trusted by this server")
	}
	message, err := verificationMessage(receipt)
	if err != nil {
		return nil, err
	}
	decoded, err := base64.StdEncoding.DecodeString(signature)
	if err != nil || len(decoded) != ed25519.SignatureSize || base64.StdEncoding.EncodeToString(decoded) != signature {
		return nil, errors.New("invalid verification signature encoding")
	}
	if !ed25519.Verify(key, message, decoded) {
		return nil, errors.New("verification signature does not match the trusted issuer")
	}
	return s.with(func(db *database) Value {
		problem := ensureRow(getRow(db, "SELECT statement FROM problems WHERE id=?", receipt.ProblemID), "problem", receipt.ProblemID)
		draft := ensureRow(getRow(db, "SELECT id,body,recorded_at FROM problem_drafts WHERE problem_id=? ORDER BY rowid DESC LIMIT 1", receipt.ProblemID), "draft", receipt.ProblemID)
		insist(strVal(draft["id"]) == receipt.DraftID, "verification does not match the latest draft")
		problemDigest := sha256.Sum256([]byte(strVal(problem["statement"])))
		draftDigest := sha256.Sum256([]byte(strVal(draft["body"])))
		insist(hex.EncodeToString(problemDigest[:]) == receipt.ProblemSHA256, "verification does not match the problem statement")
		insist(hex.EncodeToString(draftDigest[:]) == receipt.DraftSHA256, "verification does not match the exact draft content")
		draftTime, parseErr := time.Parse(time.RFC3339Nano, strVal(draft["recorded_at"]))
		checkedTime, _ := time.Parse(time.RFC3339Nano, receipt.CheckedAt)
		insist(parseErr == nil && !checkedTime.Before(draftTime.Add(-5*time.Minute)), "verification predates the learner draft")
		existing := getRow(db, "SELECT check_id FROM verified_problem_checks WHERE verifier_id=? AND signature=?", receipt.VerifierID, signature)
		if existing != nil {
			return Value{"check_id": existing["check_id"], "duplicate": true, "authenticated_verifier": true, "formal_certificate": false}
		}
		checkID := uuid()
		exec(db, "INSERT INTO problem_checks VALUES(?,?,?,?,?,?,?,?)", checkID, receipt.ProblemID, receipt.DraftID, receipt.Method, receipt.Outcome, receipt.EvidenceReference, receipt.Toolchain, now())
		exec(db, "INSERT INTO verified_problem_checks(check_id,verifier_id,receipt_json,signature,recorded_at) VALUES(?,?,?,?,?)", checkID, receipt.VerifierID, js(receipt), signature, now())
		return Value{"check_id": checkID, "draft_id": receipt.DraftID, "verifier_id": receipt.VerifierID, "method": receipt.Method, "outcome": receipt.Outcome, "duplicate": false, "authenticated_verifier": true, "formal_certificate": false}
	})
}
