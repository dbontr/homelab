package store

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func requireVerificationValue(t *testing.T) func(Value, error) Value {
	t.Helper()
	return func(value Value, err error) Value {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
		return value
	}
}

func trustedTestStore(t *testing.T) (*Store, ed25519.PrivateKey) {
	t.Helper()
	public, private, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("ADAPTIVE_TEACHING_VERIFIER_KEYS", fmt.Sprintf(`{"test-verifier":"%s"}`, base64.StdEncoding.EncodeToString(public)))
	return newExtendedStore(t), private
}

func signLatestDraft(t *testing.T, s *Store, private ed25519.PrivateKey, problemID, method, outcome string) (VerificationReceipt, string) {
	t.Helper()
	info, err := s.with(func(db *database) Value {
		p := ensureRow(getRow(db, "SELECT statement FROM problems WHERE id=?", problemID), "problem", problemID)
		d := ensureRow(getRow(db, "SELECT id,body FROM problem_drafts WHERE problem_id=? ORDER BY rowid DESC LIMIT 1", problemID), "draft", problemID)
		return Value{"statement": p["statement"], "draft_id": d["id"], "body": d["body"]}
	})
	if err != nil {
		t.Fatal(err)
	}
	statementDigest := sha256.Sum256([]byte(info["statement"].(string)))
	draftDigest := sha256.Sum256([]byte(info["body"].(string)))
	evidenceDigest := sha256.Sum256([]byte("independently retained test-report fixture"))
	receipt := VerificationReceipt{
		Version:           1,
		VerifierID:        "test-verifier",
		ProblemID:         problemID,
		ProblemSHA256:     fmt.Sprintf("%x", statementDigest),
		DraftID:           info["draft_id"].(string),
		DraftSHA256:       fmt.Sprintf("%x", draftDigest),
		Method:            method,
		Outcome:           outcome,
		EvidenceReference: "artifact://test-run/result",
		EvidenceSHA256:    fmt.Sprintf("%x", evidenceDigest),
		Toolchain:         "test-verifier/v1",
		CheckedAt:         time.Now().UTC().Format(time.RFC3339Nano),
	}
	message, err := verificationMessage(receipt)
	if err != nil {
		t.Fatal(err)
	}
	return receipt, base64.StdEncoding.EncodeToString(ed25519.Sign(private, message))
}

func TestVerificationReceiptBindsToExactDraftAndRejectsForgery(t *testing.T) {
	require := requireVerificationValue(t)
	s, private := trustedTestStore(t)
	require(s.AddConcept("compiler", "Compiler", "Parsing and compilation", "engineering"))
	p := require(s.CreateProblem("Parser", "Implement a parser and explain its behavior", "programming", "independent", "challenging", []string{"compiler"}, "engineering", "user", "", "", ""))
	pid := p["id"].(string)
	require(s.SaveProblemDraft(pid, "First program and justification"))
	receipt, signature := signLatestDraft(t, s, private, pid, "executable", "pass")
	tampered := receipt
	tampered.Outcome = "fail"
	_, err := s.RecordVerifiedProblemCheck(tampered, signature)
	fail(t, err)
	_, otherKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	message, err := verificationMessage(receipt)
	if err != nil {
		t.Fatal(err)
	}
	_, err = s.RecordVerifiedProblemCheck(receipt, base64.StdEncoding.EncodeToString(ed25519.Sign(otherKey, message)))
	fail(t, err)
	accepted := require(s.RecordVerifiedProblemCheck(receipt, signature))
	if accepted["authenticated_verifier"] != true || accepted["formal_certificate"] != false {
		t.Fatal("incorrect assurance status", accepted)
	}
	again := require(s.RecordVerifiedProblemCheck(receipt, signature))
	if again["duplicate"] != true || again["check_id"] != accepted["check_id"] {
		t.Fatal("receipt replay was not idempotent", again)
	}
	require(s.SaveProblemDraft(pid, "Revised program with a different behavior"))
	_, err = s.RecordVerifiedProblemCheck(receipt, signature)
	fail(t, err)
	require(s.RecordProblemCheck(pid, "formal", "pass", "reported proof checker", "not signed"))
	require(s.FinishProblem(pid, "solved", "Model-judged completion", .92, []string{"compiler"}))
	portfolio := require(s.MasteryPortfolio("compiler"))
	if portfolio["externally_checked_solutions"] != 0 {
		t.Fatal("stale and unsigned checks counted", portfolio)
	}
	newReceipt, newSignature := signLatestDraft(t, s, private, pid, "executable", "fail")
	require(s.RecordVerifiedProblemCheck(newReceipt, newSignature))
	portfolio = require(s.MasteryPortfolio("compiler"))
	if portfolio["externally_checked_solutions"] != 0 {
		t.Fatal("a failed check counted", portfolio)
	}
	positive, positiveSignature := signLatestDraft(t, s, private, pid, "executable", "pass")
	require(s.RecordVerifiedProblemCheck(positive, positiveSignature))
	portfolio = require(s.MasteryPortfolio("compiler"))
	if portfolio["externally_checked_solutions"] != 0 || portfolio["conflicting_checker_reports"] != 1 {
		t.Fatal("conflicting authenticated checks must block corroboration", portfolio)
	}
}

func TestVerifierConfigurationFailsClosed(t *testing.T) {
	require := requireVerificationValue(t)
	t.Setenv("ADAPTIVE_TEACHING_VERIFIER_KEYS", "{invalid")
	_, err := Open(filepath.Join(t.TempDir(), "invalid.sqlite"))
	fail(t, err)
	t.Setenv("ADAPTIVE_TEACHING_VERIFIER_KEYS", `{"issuer":"not-a-valid-key"}`)
	_, err = Open(filepath.Join(t.TempDir(), "short.sqlite"))
	fail(t, err)
	t.Setenv("ADAPTIVE_TEACHING_VERIFIER_KEYS", "")
	s := newExtendedStore(t)
	require(s.AddConcept("circuit", "Circuit", "Circuit theory", "engineering"))
	p := require(s.CreateProblem("Voltage divider", "Explain and implement a voltage divider", "calculation", "independent", "challenging", []string{"circuit"}, "engineering", "user", "", "", ""))
	require(s.SaveProblemDraft(p["id"].(string), "A complete derivation"))
	_, private, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	receipt, signature := signLatestDraft(t, s, private, p["id"].(string), "symbolic", "pass")
	_, err = s.RecordVerifiedProblemCheck(receipt, signature)
	fail(t, err)
}

func TestVerificationReceiptRejectsInvalidDigestsAndFutureReports(t *testing.T) {
	require := requireVerificationValue(t)
	s, private := trustedTestStore(t)
	require(s.AddConcept("logic", "Logic", "Deductive reasoning", "mathematics"))
	p := require(s.CreateProblem("Inference", "Prove a valid inference rule", "proof", "independent", "challenging", []string{"logic"}, "mathematics", "user", "", "", ""))
	require(s.SaveProblemDraft(p["id"].(string), "A complete proof"))
	receipt, _ := signLatestDraft(t, s, private, p["id"].(string), "formal", "pass")
	receipt.ProblemSHA256 = strings.Repeat("0", 64)
	message, err := verificationMessage(receipt)
	if err != nil {
		t.Fatal(err)
	}
	_, err = s.RecordVerifiedProblemCheck(receipt, base64.StdEncoding.EncodeToString(ed25519.Sign(private, message)))
	fail(t, err)
	receipt, _ = signLatestDraft(t, s, private, p["id"].(string), "formal", "pass")
	receipt.CheckedAt = time.Now().UTC().Add(24 * time.Hour).Format(time.RFC3339Nano)
	_, err = verificationMessage(receipt)
	fail(t, err)
	receipt.Method = "model"
	_, err = verificationMessage(receipt)
	fail(t, err)
}
