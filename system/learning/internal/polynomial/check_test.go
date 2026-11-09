package polynomial

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"github.com/dbontr/homelab-new/system/learning/internal/store"
	"github.com/dbontr/homelab-new/system/learning/internal/verification"
	"path/filepath"
	"testing"
)

func TestExactPolynomialIdentities(t *testing.T) {
	examples := []struct{ input, expected string }{
		{"(x+1)^2", "x*x + 2*x + 1"},
		{"(x+2)*(x-2)", "x^2 - 4"},
		{"(3/2)*x + 0.25", "1.5*x+1/4"},
		{"x^0", "1"},
		{"-(x^2)+x", "x-x*x"},
		{"(x^2+x)/2", "(x*x+x)*(1/2)"},
	}
	for _, tc := range examples {
		a, err := Parse(tc.input, "x")
		if err != nil {
			t.Fatalf("%q: %v", tc.input, err)
		}
		b, err := Parse(tc.expected, "x")
		if err != nil {
			t.Fatalf("%q: %v", tc.expected, err)
		}
		if !equal(a, b) {
			t.Errorf("%q != %q", tc.input, tc.expected)
		}
	}
	bad := []string{"x/(x+1)", "x^33", "x^2 + y", "(x+1", "x**3", "sin(x)", "x^2^2", "x/0"}
	for _, input := range bad {
		if _, err := Parse(input, "x"); err == nil {
			t.Errorf("accepted unsupported expression %q", input)
		}
	}
}

func TestExactDerivativeAndIdentityReports(t *testing.T) {
	statement := "Differentiate x^3 - (1/2)*x^2 using the power rule."
	fixture := Fixture{
		ProblemID: "proof-123", ProblemSHA256: verification.Digest([]byte(statement)),
		Kind: "polynomial_derivative", Expression: "x^3-(1/2)*x^2", Variable: "x",
	}
	attempt := Attempt{ProblemID: fixture.ProblemID, Statement: statement, DraftID: "revision-1", Draft: "3*x^2-x"}
	raw, err := json.Marshal(fixture)
	if err != nil {
		t.Fatal(err)
	}
	report, err := Check(fixture, attempt, raw)
	if err != nil {
		t.Fatal(err)
	}
	if report.Outcome != "pass" {
		t.Fatal(report)
	}
	wrong := attempt
	wrong.Draft = "3*x^2+x"
	rejected, err := Check(fixture, wrong, raw)
	if err != nil || rejected.Outcome != "fail" {
		t.Fatalf("wrong derivative: %v %v", rejected, err)
	}
	unsupported := attempt
	unsupported.Draft = "sin(x)"
	unknown, err := Check(fixture, unsupported, raw)
	if err != nil || unknown.Outcome != "inconclusive" {
		t.Fatalf("unsupported notation: %v %v", unknown, err)
	}
	fake := fixture
	fake.ProblemSHA256 = verification.Digest([]byte("modified question"))
	if _, err = Check(fake, attempt, raw); err == nil {
		t.Fatal("accepted changed statement")
	}
	fake = fixture
	fake.Kind = "unrestricted-code"
	if _, err = Check(fake, attempt, raw); err == nil {
		t.Fatal("accepted untrusted execution mode")
	}
}

func TestStrictInputAndBoundedArithmetic(t *testing.T) {
	for _, input := range []string{`{"problem_id":"abc","unknown":true}`, `{"problem_id":"abc"} {"problem_id":"def"}`, `{}garbage`} {
		if _, err := Decode[Fixture]([]byte(input)); err == nil {
			t.Fatalf("accepted invalid payload %q", input)
		}
	}
	if _, err := Parse("x^32", "x"); err != nil {
		t.Fatal(err)
	}
	if _, err := Parse("(x^32)^32", "x"); err == nil {
		t.Fatal("accepted excessive degree")
	}
	if _, err := Parse("99999999999999999999999999999999999999999999999999999999999999999^32", "x"); err == nil {
		t.Fatal("accepted extreme coefficient growth")
	}
}

func TestCheckedExpressionProducesImportableSignedReceipt(t *testing.T) {
	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("ADAPTIVE_TEACHING_VERIFIER_KEYS", `{"math-checker":"`+base64.StdEncoding.EncodeToString(publicKey)+`"}`)
	db, err := store.Open(filepath.Join(t.TempDir(), "private.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err = db.AddConcept("algebra", "Algebra", "Polynomial identities", "mathematics"); err != nil {
		t.Fatal(err)
	}
	problem, err := db.CreateProblem("Polynomial expansion", "Expand (x+1)^2 as a polynomial in x", "calculation", "independent", "challenging", []string{"algebra"}, "mathematics", "user", "", "", "")
	if err != nil {
		t.Fatal(err)
	}
	pid := problem["id"].(string)
	confidence := 0.35
	saved, err := db.SaveProblemDraftWithReflection(pid, "x^2 + 2*x + 1", "The binomial law gives the middle term.", &confidence)
	if err != nil {
		t.Fatal(err)
	}
	did := saved["draft_id"].(string)
	fixture := Fixture{ProblemID: pid, ProblemSHA256: verification.Digest([]byte(problem["statement"].(string))), Kind: "polynomial_identity", Expression: "(x+1)^2", Variable: "x"}
	attempt := Attempt{ProblemID: pid, Statement: problem["statement"].(string), DraftID: did, Draft: "x^2 + 2*x + 1"}
	raw, err := json.Marshal(fixture)
	if err != nil {
		t.Fatal(err)
	}
	report, err := Check(fixture, attempt, raw)
	if err != nil {
		t.Fatal(err)
	}
	if report.Outcome != "pass" {
		t.Fatal(report)
	}
	reportBytes, err := json.Marshal(report)
	if err != nil {
		t.Fatal(err)
	}
	receipt := report.Receipt("math-checker", reportBytes)
	signature, err := verification.Sign(receipt, privateKey)
	if err != nil {
		t.Fatal(err)
	}
	stored, err := db.RecordVerifiedProblemCheck(receipt, signature)
	if err != nil || stored["authenticated_verifier"] != true {
		t.Fatalf("receipt rejected: %v %v", stored, err)
	}
	check, err := db.GetProblem(pid)
	if err != nil {
		t.Fatal(err)
	}
	if len(check["checks"].([]store.Value)) != 1 {
		t.Fatal("checker receipt not attached to problem")
	}
	if _, err = db.SaveProblemDraftWithReflection(pid, "x^2+3*x+1", "Changed answer", nil); err != nil {
		t.Fatal(err)
	}
	if _, err = db.RecordVerifiedProblemCheck(receipt, signature); err == nil {
		t.Fatal("old signed report accepted against revised draft")
	}
}
