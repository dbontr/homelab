package main

import (
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/dbontr/homelab-new/system/learning/internal/polynomial"
	"github.com/dbontr/homelab-new/system/learning/internal/verification"
)

func writeFixture(t *testing.T, path string, v any) {
	t.Helper()
	b, e := json.Marshal(v)
	if e != nil {
		t.Fatal(e)
	}
	if e = os.WriteFile(path, b, 0600); e != nil {
		t.Fatal(e)
	}
}
func TestSignerRunsRealExactChecker(t *testing.T) {
	dir := t.TempDir()
	priv := filepath.Join(dir, "signer.secret")
	pub := filepath.Join(dir, "signer.public")
	if err := run([]string{"keygen", "-id", "math-checker", "-private", priv, "-public", pub}); err != nil {
		t.Fatal(err)
	}
	publicBytes, err := os.ReadFile(pub)
	if err != nil {
		t.Fatal(err)
	}
	publicConfig := map[string]string{}
	if err = json.Unmarshal(publicBytes, &publicConfig); err != nil {
		t.Fatal(err)
	}
	publicKey, err := base64.StdEncoding.DecodeString(publicConfig["math-checker"])
	if err != nil {
		t.Fatal(err)
	}
	statement := "Expand (x+1)^2"
	fixture := polynomial.Fixture{ProblemID: "polynomial-problem", ProblemSHA256: verification.Digest([]byte(statement)), Kind: "polynomial_identity", Expression: "(x+1)^2", Variable: "x"}
	attempt := polynomial.Attempt{ProblemID: fixture.ProblemID, Statement: statement, DraftID: "saved-draft-1", Draft: "x^2+2*x+1"}
	fixturePath := filepath.Join(dir, "trusted-fixture.json")
	attemptPath := filepath.Join(dir, "learner-attempt.json")
	reportPath := filepath.Join(dir, "report.json")
	receiptPath := filepath.Join(dir, "signed.json")
	writeFixture(t, fixturePath, fixture)
	writeFixture(t, attemptPath, attempt)
	command := []string{"verify", "-id", "math-checker", "-private", priv, "-fixture", fixturePath, "-attempt", attemptPath, "-report", reportPath, "-receipt", receiptPath}
	if err = run(command); err != nil {
		t.Fatal(err)
	}
	reportBytes, err := os.ReadFile(reportPath)
	if err != nil {
		t.Fatal(err)
	}
	report := polynomial.Report{}
	if err = json.Unmarshal(reportBytes, &report); err != nil {
		t.Fatal(err)
	}
	if report.Outcome != "pass" {
		t.Fatal(report)
	}
	signatureBytes, err := os.ReadFile(receiptPath)
	if err != nil {
		t.Fatal(err)
	}
	sealed := struct {
		Receipt   verification.Receipt `json:"receipt"`
		Signature string               `json:"signature"`
	}{}
	if err = json.Unmarshal(signatureBytes, &sealed); err != nil {
		t.Fatal(err)
	}
	if !verification.Verify(sealed.Receipt, sealed.Signature, ed25519.PublicKey(publicKey)) {
		t.Fatal("signed checker receipt failed public verification")
	}
	if sealed.Receipt.EvidenceSHA256 != verification.Digest(reportBytes) {
		t.Fatal("report content digest mismatch")
	}
	if err = run(command); err == nil {
		t.Fatal("verification overwrote immutable evidence outputs")
	}
	attempt.Draft = "x^2+3*x+1"
	wrongPath := filepath.Join(dir, "wrong.json")
	writeFixture(t, wrongPath, attempt)
	wrongReport := filepath.Join(dir, "wrong-report.json")
	wrongReceipt := filepath.Join(dir, "wrong-receipt.json")
	if err = run([]string{"verify", "-id", "math-checker", "-private", priv, "-fixture", fixturePath, "-attempt", wrongPath, "-report", wrongReport, "-receipt", wrongReceipt}); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(wrongReport)
	if err != nil {
		t.Fatal(err)
	}
	if err = json.Unmarshal(data, &report); err != nil || report.Outcome != "fail" {
		t.Fatalf("incorrect submission: %v %v", report, err)
	}
	if _, err = os.Stat(wrongReceipt); err != nil {
		t.Fatal("signed negative verdict absent")
	}
}

func TestSignerRejectsTamperedFixtureAndUnreadableKey(t *testing.T) {
	dir := t.TempDir()
	priv := filepath.Join(dir, "private")
	pub := filepath.Join(dir, "public")
	if err := run([]string{"keygen", "-id", "math-checker", "-private", priv, "-public", pub}); err != nil {
		t.Fatal(err)
	}
	statement := "Expand x^2"
	fixture := polynomial.Fixture{ProblemID: "problem", ProblemSHA256: verification.Digest([]byte("a different statement")), Kind: "polynomial_identity", Expression: "x^2", Variable: "x"}
	attempt := polynomial.Attempt{ProblemID: "problem", Statement: statement, DraftID: "draft", Draft: "x*x"}
	fixtureFile := filepath.Join(dir, "fixture")
	attemptFile := filepath.Join(dir, "attempt")
	writeFixture(t, fixtureFile, fixture)
	writeFixture(t, attemptFile, attempt)
	report := filepath.Join(dir, "report")
	receipt := filepath.Join(dir, "receipt")
	args := []string{"verify", "-id", "math-checker", "-private", priv, "-fixture", fixtureFile, "-attempt", attemptFile, "-report", report, "-receipt", receipt}
	if err := run(args); err == nil {
		t.Fatal("accepted a mismatched trusted fixture")
	}
	if _, err := os.Stat(receipt); !os.IsNotExist(err) {
		t.Fatal("signed untrusted fixture")
	}
	fixture.ProblemSHA256 = verification.Digest([]byte(statement))
	writeFixture(t, fixtureFile+".good", fixture)
	args[6] = fixtureFile + ".good"
	if err := os.Chmod(priv, 0644); err != nil {
		t.Fatal(err)
	}
	if err := run(args); err == nil {
		t.Fatal("accepted world-readable signing key")
	}
}
