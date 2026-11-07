package polynomial

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/dbontr/Adaptive-Teaching-MCP/internal/verification"
)

// Fixture is authorized reference material, not an assertion supplied by a learner.
// It is held by the checker, not by the tutoring MCP server.
type Fixture struct {
	ProblemID     string `json:"problem_id"`
	ProblemSHA256 string `json:"problem_sha256"`
	Kind          string `json:"kind"`
	Expression    string `json:"expression"`
	Variable      string `json:"variable"`
}

// Attempt is an exact, operator-exported snapshot of a saved learner draft.
type Attempt struct {
	ProblemID string `json:"problem_id"`
	Statement string `json:"statement"`
	DraftID   string `json:"draft_id"`
	Draft     string `json:"draft"`
}

type Report struct {
	Version             int    `json:"version"`
	Checker             string `json:"checker"`
	ProblemID           string `json:"problem_id"`
	DraftID             string `json:"draft_id"`
	ProblemSHA256       string `json:"problem_sha256"`
	DraftSHA256         string `json:"draft_sha256"`
	FixtureSHA256       string `json:"fixture_sha256"`
	Kind                string `json:"kind"`
	Variable            string `json:"variable"`
	Outcome             string `json:"outcome"`
	Reason              string `json:"reason"`
	ExpectedPolynomial  string `json:"expected_polynomial,omitempty"`
	SubmittedPolynomial string `json:"submitted_polynomial,omitempty"`
	Scope               string `json:"scope"`
	CheckedAt           string `json:"checked_at"`
}

const CheckerVersion = "adaptive-polynomial-verifier/1"

// Decode rejects unknown properties and trailing documents, avoiding ambiguous input contracts.
func Decode[T any](raw []byte) (T, error) {
	var out T
	if len(raw) == 0 || len(raw) > 24000 {
		return out, errors.New("input exceeds 24000-byte limit or is empty")
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&out); err != nil {
		return out, err
	}
	var extra any
	if err := decoder.Decode(&extra); err == nil {
		return out, errors.New("multiple JSON values are not permitted")
	} else if !errors.Is(err, io.EOF) {
		return out, err
	}
	return out, nil
}

// Check validates the operator-controlled fixture and proves or refutes the
// submitted univariate polynomial expression by exact rational arithmetic.
// It cannot certify the learner's explanatory steps or non-polynomial claims.
func Check(fixture Fixture, attempt Attempt, fixtureBytes []byte) (Report, error) {
	if fixture.ProblemID == "" || attempt.ProblemID == "" || fixture.ProblemID != attempt.ProblemID {
		return Report{}, errors.New("fixture and attempt refer to different problems")
	}
	if attempt.DraftID == "" || len(attempt.DraftID) > 128 || len(attempt.ProblemID) > 128 || len(attempt.Statement) > 20000 || len(attempt.Draft) > 20000 {
		return Report{}, errors.New("attempt identifiers or contents are invalid")
	}
	if fixture.ProblemSHA256 != verification.Digest([]byte(attempt.Statement)) {
		return Report{}, errors.New("fixture does not match the immutable problem statement")
	}
	if len(fixtureBytes) == 0 {
		return Report{}, errors.New("trusted fixture bytes are required")
	}
	if fixture.Kind != "polynomial_identity" && fixture.Kind != "polynomial_derivative" {
		return Report{}, errors.New("unsupported trusted fixture kind")
	}
	expected, err := Parse(fixture.Expression, fixture.Variable)
	if err != nil {
		return Report{}, fmt.Errorf("invalid trusted fixture: %w", err)
	}
	if fixture.Kind == "polynomial_derivative" {
		expected = derivative(expected)
	}
	result := Report{
		Version: 1, Checker: CheckerVersion, ProblemID: attempt.ProblemID, DraftID: attempt.DraftID,
		ProblemSHA256: verification.Digest([]byte(attempt.Statement)),
		DraftSHA256:   verification.Digest([]byte(attempt.Draft)),
		FixtureSHA256: verification.Digest(fixtureBytes),
		Kind:          fixture.Kind, Variable: fixture.Variable, Outcome: "inconclusive",
		ExpectedPolynomial: canonical(expected),
		Scope:              "Exact univariate polynomial identity over rational coefficients. Explanatory reasoning, modeling assumptions and non-polynomial claims are not checked.",
		CheckedAt:          time.Now().UTC().Format(time.RFC3339Nano),
	}
	submitted, err := Parse(strings.TrimSpace(attempt.Draft), fixture.Variable)
	if err != nil {
		result.Reason = "Submitted expression is outside the supported exact polynomial language: " + err.Error()
		return result, nil
	}
	result.SubmittedPolynomial = canonical(submitted)
	if equal(expected, submitted) {
		result.Outcome = "pass"
		result.Reason = "Exact polynomial coefficients match."
	} else {
		result.Outcome = "fail"
		result.Reason = "Exact polynomial coefficients differ."
	}
	return result, nil
}

// Receipt covers a saved report's exact bytes. The report is retained by the checker.
// The tutoring server stores the digest and authenticated issuer, not the private key.
func (r Report) Receipt(verifierID string, reportBytes []byte) verification.Receipt {
	digest := verification.Digest(reportBytes)
	return verification.Receipt{
		Version: 1, VerifierID: verifierID, ProblemID: r.ProblemID, ProblemSHA256: r.ProblemSHA256,
		DraftID: r.DraftID, DraftSHA256: r.DraftSHA256, Method: "symbolic", Outcome: r.Outcome,
		EvidenceReference: "sha256:" + digest, EvidenceSHA256: digest, Toolchain: r.Checker, CheckedAt: r.CheckedAt,
	}
}
