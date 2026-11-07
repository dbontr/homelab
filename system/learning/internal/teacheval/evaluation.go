package teacheval

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"
)

const Version = 1

var Dimensions = []string{"diagnosis", "accuracy", "repair", "transfer", "restraint"}

type Case struct {
	ID           string   `json:"id"`
	Domain       string   `json:"domain"`
	LearnerTurns []string `json:"learner_turns"`
}

type Suite struct {
	Version int    `json:"version"`
	Cases   []Case `json:"cases"`
}

type Criterion struct {
	ID                  string   `json:"id"`
	Misconception       string   `json:"misconception"`
	RequiredReasoning   []string `json:"required_reasoning"`
	UnsafeOrFalseClaims []string `json:"unsafe_or_false_claims"`
	TransferTarget      string   `json:"transfer_target"`
}

type Rubrics struct {
	Version  int         `json:"version"`
	Criteria []Criterion `json:"criteria"`
}

func Digest(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

func ReadJSON(path string, value any) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	stat, err := f.Stat()
	if err != nil {
		return err
	}
	if !stat.Mode().IsRegular() || stat.Size() > 4<<20 {
		return errors.New("input must be a regular file no larger than 4 MiB")
	}
	decoder := json.NewDecoder(io.LimitReader(f, 4<<20+1))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(value); err != nil {
		return err
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		if err == nil {
			return errors.New("trailing JSON value")
		}
		return fmt.Errorf("trailing JSON: %w", err)
	}
	return nil
}

// WritePrivate creates a durable, exclusive artifact. No previous evaluation
// transcript or human rating can be overwritten accidentally.
func WritePrivate(path string, value any) error {
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
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

func ValidateSuite(s Suite) error {
	if s.Version != Version || len(s.Cases) < 2 || len(s.Cases) > 100 {
		return errors.New("suite requires version 1 and 2..100 cases")
	}
	seen := map[string]bool{}
	for _, c := range s.Cases {
		if !validID(c.ID) || seen[c.ID] {
			return fmt.Errorf("invalid or repeated case ID %q", c.ID)
		}
		seen[c.ID] = true
		if strings.TrimSpace(c.Domain) == "" || len(c.Domain) > 100 || len(c.LearnerTurns) < 1 || len(c.LearnerTurns) > 5 {
			return fmt.Errorf("invalid domain or number of turns for %s", c.ID)
		}
		for _, turn := range c.LearnerTurns {
			if strings.TrimSpace(turn) == "" || len(turn) > 5000 {
				return fmt.Errorf("invalid learner turn for %s", c.ID)
			}
		}
	}
	return nil
}

func validID(s string) bool {
	if len(s) < 3 || len(s) > 80 {
		return false
	}
	for i, c := range s {
		if c >= 'a' && c <= 'z' || c >= '0' && c <= '9' && i > 0 || c == '-' && i > 0 {
			continue
		}
		return false
	}
	return s[len(s)-1] != '-'
}

func ValidateRubrics(s Suite, r Rubrics) error {
	if err := ValidateSuite(s); err != nil {
		return err
	}
	if r.Version != Version || len(r.Criteria) != len(s.Cases) {
		return errors.New("one rubric per case is required")
	}
	cases := map[string]bool{}
	for _, c := range s.Cases {
		cases[c.ID] = true
	}
	seen := map[string]bool{}
	for _, c := range r.Criteria {
		if !cases[c.ID] || seen[c.ID] {
			return fmt.Errorf("unknown or duplicate criterion %q", c.ID)
		}
		seen[c.ID] = true
		if strings.TrimSpace(c.Misconception) == "" || strings.TrimSpace(c.TransferTarget) == "" || len(c.RequiredReasoning) < 1 || len(c.UnsafeOrFalseClaims) < 1 {
			return fmt.Errorf("incomplete rubric for %s", c.ID)
		}
		for _, s := range append(append([]string{}, c.RequiredReasoning...), c.UnsafeOrFalseClaims...) {
			if strings.TrimSpace(s) == "" || len(s) > 1200 {
				return fmt.Errorf("invalid rubric criterion for %s", c.ID)
			}
		}
	}
	return nil
}

type Message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type Attempt struct {
	CaseID   string    `json:"case_id"`
	Messages []Message `json:"messages"`
	Error    string    `json:"error,omitempty"`
}

type Run struct {
	Version      int       `json:"version"`
	Mode         string    `json:"mode"`
	Condition    string    `json:"condition"`
	Model        string    `json:"model"`
	SuiteSHA256  string    `json:"suite_sha256"`
	PolicySHA256 string    `json:"policy_sha256"`
	StartedAt    string    `json:"started_at"`
	Attempts     []Attempt `json:"attempts"`
}

func SuiteDigest(s Suite) string { b, _ := json.Marshal(s); return Digest(b) }
func RunEvaluation(ctx context.Context, s Suite, policy, condition, model string, client CompletionClient) (Run, error) {
	if err := ValidateSuite(s); err != nil {
		return Run{}, err
	}
	if strings.TrimSpace(policy) == "" || len(policy) > 75000 || !validID(condition) || strings.TrimSpace(model) == "" {
		return Run{}, errors.New("policy, condition or model is invalid")
	}
	out := Run{Version: Version, Mode: "prompt_only_chat_completions", Condition: condition, Model: model, SuiteSHA256: SuiteDigest(s), PolicySHA256: Digest([]byte(policy)), StartedAt: time.Now().UTC().Format(time.RFC3339Nano), Attempts: make([]Attempt, 0, len(s.Cases))}
	for _, c := range s.Cases {
		attempt := Attempt{CaseID: c.ID, Messages: []Message{}}
		request := []Message{{Role: "system", Content: policy}}
		for _, turn := range c.LearnerTurns {
			m := Message{Role: "user", Content: turn}
			request = append(request, m)
			attempt.Messages = append(attempt.Messages, m)
			reply, err := client.Complete(ctx, model, request)
			if err != nil {
				attempt.Error = err.Error()
				break
			}
			if strings.TrimSpace(reply) == "" {
				attempt.Error = "model returned no assistant text"
				break
			}
			answer := Message{Role: "assistant", Content: reply}
			request = append(request, answer)
			attempt.Messages = append(attempt.Messages, answer)
		}
		out.Attempts = append(out.Attempts, attempt)
		if err := ctx.Err(); err != nil {
			return out, err
		}
	}
	return out, nil
}

type CompletionClient interface {
	Complete(context.Context, string, []Message) (string, error)
}

type HTTPClient struct {
	Endpoint string
	APIKey   string
	Client   *http.Client
}

func ValidateEndpoint(endpoint string) error {
	u, e := url.Parse(endpoint)
	if e != nil || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return errors.New("endpoint must be a URL without credentials, query or fragment")
	}
	if u.Scheme != "https" {
		if u.Scheme != "http" || !(u.Hostname() == "127.0.0.1" || u.Hostname() == "localhost" || u.Hostname() == "::1") {
			return errors.New("non-loopback model endpoints must use HTTPS")
		}
	}
	if u.Path != "/v1/chat/completions" {
		return errors.New("endpoint must end in /v1/chat/completions")
	}
	return nil
}

func (c HTTPClient) Complete(ctx context.Context, model string, messages []Message) (string, error) {
	if err := ValidateEndpoint(c.Endpoint); err != nil {
		return "", err
	}
	reqData := struct {
		Model    string    `json:"model"`
		Messages []Message `json:"messages"`
	}{Model: model, Messages: messages}
	data, err := json.Marshal(reqData)
	if err != nil {
		return "", err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.Endpoint, bytes.NewReader(data))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	if c.APIKey != "" {
		req.Header.Set("Authorization", "Bearer "+c.APIKey)
	}
	client := c.Client
	if client == nil {
		client = &http.Client{Timeout: 90 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	}
	resp, err := client.Do(req)
	if err != nil {
		var transportError *url.Error
		if errors.As(err, &transportError) {
			return "", fmt.Errorf("model transport failed: %v", transportError.Err)
		}
		return "", errors.New("model transport failed")
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("model endpoint returned HTTP %d", resp.StatusCode)
	}
	var result struct {
		Choices []struct {
			Message Message `json:"message"`
		} `json:"choices"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 4<<20)).Decode(&result); err != nil {
		return "", fmt.Errorf("invalid model response: %w", err)
	}
	if len(result.Choices) == 0 || result.Choices[0].Message.Role != "assistant" {
		return "", errors.New("model did not return an assistant response")
	}
	return result.Choices[0].Message.Content, nil
}
