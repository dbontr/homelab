package main

import (
	"bytes"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"strings"

	"github.com/dbontr/Adaptive-Teaching-MCP/internal/polynomial"
	"github.com/dbontr/Adaptive-Teaching-MCP/internal/verification"
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "verifier:", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	if len(args) == 0 {
		return errors.New("usage: polynomial-verifier keygen|verify [flags]")
	}
	switch args[0] {
	case "keygen":
		return keygen(args[1:])
	case "verify":
		return verify(args[1:])
	default:
		return errors.New("unknown subcommand; use keygen or verify")
	}
}

func writeExclusive(path string, data []byte, perm os.FileMode) error {
	if path == "" {
		return errors.New("output path is required")
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, perm)
	if err != nil {
		return err
	}
	if _, err = f.Write(data); err != nil {
		f.Close()
		os.Remove(path)
		return err
	}
	if err = f.Sync(); err != nil {
		f.Close()
		os.Remove(path)
		return err
	}
	if err = f.Close(); err != nil {
		os.Remove(path)
		return err
	}
	return nil
}
func loadFile(path string, max int) ([]byte, error) {
	if path == "" {
		return nil, errors.New("input path is required")
	}
	info, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, errors.New("input must be a regular file, not a symlink")
	}
	if info.Size() > int64(max) {
		return nil, errors.New("input file exceeds size limit")
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	if len(b) > max {
		return nil, errors.New("input file exceeds size limit")
	}
	return b, nil
}
func loadPrivate(path string) (ed25519.PrivateKey, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() || info.Mode().Perm()&0077 != 0 {
		return nil, errors.New("private key must be a regular owner-only file")
	}
	b, err := loadFile(path, 256)
	if err != nil {
		return nil, err
	}
	encoded := strings.TrimSpace(string(b))
	key, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil || len(key) != ed25519.PrivateKeySize {
		return nil, errors.New("invalid Ed25519 private key")
	}
	reconstructed := ed25519.NewKeyFromSeed(key[:32])
	if !bytes.Equal(key, reconstructed) {
		return nil, errors.New("Ed25519 private key does not match seed")
	}
	return ed25519.PrivateKey(key), nil
}
func keygen(args []string) error {
	fs := flag.NewFlagSet("keygen", flag.ContinueOnError)
	id := fs.String("id", "", "verifier identifier")
	privateFile := fs.String("private", "", "private key output")
	publicFile := fs.String("public", "", "public key configuration output")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() > 0 || *privateFile == "" || *publicFile == "" || *privateFile == *publicFile {
		return errors.New("keygen requires distinct -private and -public paths")
	}
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return err
	}
	probe := verification.Receipt{Version: 1, VerifierID: *id, ProblemID: "placeholder", ProblemSHA256: verification.Digest(nil),
		DraftID: "placeholder", DraftSHA256: verification.Digest(nil), Method: "symbolic", Outcome: "pass",
		EvidenceReference: "placeholder", EvidenceSHA256: verification.Digest(nil), Toolchain: polynomial.CheckerVersion, CheckedAt: "2026-01-01T00:00:00Z"}
	if _, err = verification.Message(probe); err != nil {
		return err
	}
	publicJSON, err := json.MarshalIndent(map[string]string{*id: base64.StdEncoding.EncodeToString(pub)}, "", "  ")
	if err != nil {
		return err
	}
	if err = writeExclusive(*privateFile, []byte(base64.StdEncoding.EncodeToString(priv)+"\n"), 0600); err != nil {
		return err
	}
	if err = writeExclusive(*publicFile, append(publicJSON, '\n'), 0644); err != nil {
		return err
	}
	fmt.Fprintln(os.Stdout, "Verifier key pair written. Protect the private key; configure only the public key in the teaching service.")
	return nil
}
func verify(args []string) error {
	fs := flag.NewFlagSet("verify", flag.ContinueOnError)
	id := fs.String("id", "", "operator-authorized verifier identifier")
	privateFile := fs.String("private", "", "owner-only Ed25519 private key file")
	fixtureFile := fs.String("fixture", "", "trusted operator-owned fixture")
	attemptFile := fs.String("attempt", "", "exact saved learner attempt export")
	reportFile := fs.String("report", "", "durable evidence report output")
	receiptFile := fs.String("receipt", "", "signed receipt output")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() > 0 || *reportFile == "" || *receiptFile == "" || *reportFile == *receiptFile {
		return errors.New("verify requires distinct -report and -receipt output paths")
	}
	fixtureBytes, err := loadFile(*fixtureFile, 24000)
	if err != nil {
		return err
	}
	attemptBytes, err := loadFile(*attemptFile, 24000)
	if err != nil {
		return err
	}
	fixture, err := polynomial.Decode[polynomial.Fixture](fixtureBytes)
	if err != nil {
		return fmt.Errorf("fixture: %w", err)
	}
	attempt, err := polynomial.Decode[polynomial.Attempt](attemptBytes)
	if err != nil {
		return fmt.Errorf("attempt: %w", err)
	}
	private, err := loadPrivate(*privateFile)
	if err != nil {
		return err
	}
	report, err := polynomial.Check(fixture, attempt, fixtureBytes)
	if err != nil {
		return err
	}
	reportBytes, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		return err
	}
	reportBytes = append(reportBytes, '\n')
	receipt := report.Receipt(*id, reportBytes)
	signature, err := verification.Sign(receipt, private)
	if err != nil {
		return err
	}
	sealed := struct {
		Receipt   verification.Receipt `json:"receipt"`
		Signature string               `json:"signature"`
	}{Receipt: receipt, Signature: signature}
	receiptBytes, err := json.MarshalIndent(sealed, "", "  ")
	if err != nil {
		return err
	}
	if err = writeExclusive(*reportFile, reportBytes, 0600); err != nil {
		return err
	}
	if err = writeExclusive(*receiptFile, append(receiptBytes, '\n'), 0600); err != nil {
		return err
	}
	fmt.Fprintf(os.Stdout, "Verification outcome: %s. Evidence and signed receipt saved.\n", report.Outcome)
	return nil
}
