package teacheval

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type stubClient struct {
	calls [][]Message
	fail  bool
}

func (s *stubClient) Complete(_ context.Context, _ string, messages []Message) (string, error) {
	s.calls = append(s.calls, append([]Message{}, messages...))
	if s.fail {
		return "", errors.New("unavailable")
	}
	return fmt.Sprintf("Reason through the premise, not just the choice. Attempt %d", len(s.calls)), nil
}

func exampleSuite() Suite {
	return Suite{Version: Version, Cases: []Case{
		{ID: "reason-one", Domain: "mathematics", LearnerTurns: []string{"My correct answer follows from an invalid assumption.", "What foundational premise am I missing?"}},
		{ID: "reason-two", Domain: "history", LearnerTurns: []string{"Two sources prove the motive.", "What else must be checked?"}},
	}}
}
func exampleRubrics() Rubrics {
	return Rubrics{Version: Version, Criteria: []Criterion{
		{ID: "reason-one", Misconception: "Answer correctness confused with sound proof", RequiredReasoning: []string{"Examine premise"}, UnsafeOrFalseClaims: []string{"One correct answer proves mastery"}, TransferTarget: "Unfamiliar proof"},
		{ID: "reason-two", Misconception: "Event corroboration confused with motive", RequiredReasoning: []string{"Independent provenance"}, UnsafeOrFalseClaims: []string{"Sources prove motive"}, TransferTarget: "New source"},
	}}
}
func TestModelRunnerNeverReceivesRubricsAndRecordsRealReplies(t *testing.T) {
	client := &stubClient{}
	suite := exampleSuite()
	run, err := RunEvaluation(context.Background(), suite, "Give only process-oriented feedback.", "foundation", "local-model", client)
	if err != nil {
		t.Fatal(err)
	}
	if run.Mode != "prompt_only_chat_completions" || run.SuiteSHA256 != SuiteDigest(suite) || len(run.Attempts) != 2 {
		t.Fatal(run)
	}
	if len(client.calls) != 4 {
		t.Fatalf("expected two turns per case, got %d", len(client.calls))
	}
	if len(client.calls[1]) != 4 || client.calls[1][2].Role != "assistant" {
		t.Fatal("model conversation did not include prior assistant reply", client.calls[1])
	}
	for _, call := range client.calls {
		payload, _ := json.Marshal(call)
		if strings.Contains(string(payload), "One correct answer proves mastery") || strings.Contains(string(payload), "Independent provenance") {
			t.Fatal("sealed evaluator rubrics leaked into a model prompt")
		}
	}
	if strings.Contains(run.Attempts[0].Messages[1].Content, "unavailable") {
		t.Fatal("stub reply was not recorded")
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "run.json")
	if err := WritePrivate(path, run); err != nil {
		t.Fatal(err)
	}
	if err := WritePrivate(path, run); err == nil {
		t.Fatal("write silently overwrote transcript")
	}
	stat, err := os.Stat(path)
	if err != nil || stat.Mode().Perm() != 0600 {
		t.Fatalf("artifact not owner-only: %v %v", stat, err)
	}
	var restored Run
	if err := ReadJSON(path, &restored); err != nil || restored.PolicySHA256 != run.PolicySHA256 {
		t.Fatal("round-trip failed", err)
	}
}
func TestModelHTTPCompatibilityAndFailureIsolation(t *testing.T) {
	n := 0
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" || r.URL.Path != "/v1/chat/completions" {
			t.Errorf("wrong API request: %s", r.URL.String())
		}
		if r.Header.Get("Authorization") != "Bearer private-token" {
			t.Error("missing token on authorized model request")
		}
		var payload struct {
			Model       string    `json:"model"`
			Messages    []Message `json:"messages"`
			Temperature float64   `json:"temperature"`
		}
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Error(err)
		}
		if payload.Model != "tutor-model" || len(payload.Messages) < 2 || payload.Temperature != 0 {
			t.Error("model request not reproducible", payload)
		}
		n++
		if n == 2 {
			w.WriteHeader(503)
			return
		}
		fmt.Fprint(w, `{"choices":[{"message":{"role":"assistant","content":"Analyze the premise before answering."}}]}`)
	}))
	defer backend.Close()
	endpoint := backend.URL + "/v1/chat/completions"
	if err := ValidateEndpoint(endpoint); err != nil {
		t.Fatal(err)
	}
	client := HTTPClient{Endpoint: endpoint, APIKey: "private-token", Client: backend.Client()}
	run, err := RunEvaluation(context.Background(), exampleSuite(), "Tutor policy", "baseline", "tutor-model", client)
	if err != nil {
		t.Fatal(err)
	}
	if run.Attempts[0].Error == "" || len(run.Attempts[0].Messages) != 3 {
		t.Fatal("partial model failure was not preserved", run.Attempts[0])
	}
	if run.Attempts[1].Error != "" || len(run.Attempts[1].Messages) != 4 {
		t.Fatal("subsequent case failed to resume", run.Attempts[1])
	}
	raw, _ := json.Marshal(run)
	if strings.Contains(string(raw), "private-token") || strings.Contains(string(raw), endpoint) {
		t.Fatal("private API connection leaked into report")
	}
	for _, endpoint := range []string{"http://example.com/v1/chat/completions", "https://user:password@example.com/v1/chat/completions", "https://example.com/v1/chat/completions?token=secret", "file:///v1/chat/completions", "https://example.com/v1/models"} {
		if err := ValidateEndpoint(endpoint); err == nil {
			t.Errorf("accepted insecure URL: %s", endpoint)
		}
	}
}
func rating(blindID, reviewer string, score int, quote string) Rating {
	dimensions := make([]DimensionReview, 0, len(Dimensions))
	for _, d := range Dimensions {
		v := quote
		if score == 0 {
			v = ""
		}
		dimensions = append(dimensions, DimensionReview{Dimension: d, Score: score, Quote: v, Explanation: "Independent rubric-based evaluation of the observed teaching response."})
	}
	return Rating{BlindID: blindID, ReviewerID: reviewer, Dimensions: dimensions}
}
func TestBlindSeparationReviewAndPairedReport(t *testing.T) {
	suite := exampleSuite()
	low, _ := RunEvaluation(context.Background(), suite, "Generic tutor", "baseline", "same-model", &stubClient{})
	high, _ := RunEvaluation(context.Background(), suite, "Explain why", "foundation", "same-model", &stubClient{})
	bundle, key, err := Blind(suite, exampleRubrics(), []Run{low, high})
	if err != nil {
		t.Fatal(err)
	}
	if len(bundle.Samples) != 4 || len(key.Records) != 4 {
		t.Fatal("wrong number of blinded transcripts")
	}
	data, _ := json.Marshal(bundle)
	if strings.Contains(string(data), `"condition":`) || strings.Contains(string(data), `"model":`) || strings.Contains(string(data), `"policy_sha256":`) || strings.Contains(string(data), low.PolicySHA256) {
		t.Fatal("blinded transcript leaked identity or policy")
	}
	reviews := Reviews{Version: Version}
	for _, sample := range bundle.Samples {
		var label string
		for _, rec := range key.Records {
			if rec.BlindID == sample.BlindID {
				label = rec.Condition
				break
			}
		}
		score, quote := 0, ""
		if label == "foundation" {
			score = 2
			quote = sample.TutorTurns[0][:10]
		}
		reviews.Ratings = append(reviews.Ratings, rating(sample.BlindID, "reviewer-one", score, quote), rating(sample.BlindID, "reviewer-two", score, quote))
	}
	report, err := ScoreBlinded(bundle, key, reviews)
	if err != nil {
		t.Fatal(err)
	}
	if report.LearningGainsMeasured || report.Status != "reviewed" || report.MatchedPairs != 2 || len(report.Conditions) != 2 || report.MeanPairedDifference == nil || *report.MeanPairedDifference != 1 {
		t.Fatal("invalid blinded evaluation report", report)
	}
	if report.ReviewerCount != 2 || report.DimensionAgreement == nil || *report.DimensionAgreement != 1 {
		t.Fatal("reviewers not accounted for", report)
	}
	modified := reviews
	modified.Ratings = append([]Rating{}, reviews.Ratings...)
	disagreement := modified.Ratings[1]
	disagreement.Dimensions = append([]DimensionReview{}, disagreement.Dimensions...)
	disagreement.Dimensions[0].Score = 1
	if disagreement.Dimensions[0].Quote == "" {
		disagreement.Dimensions[0].Quote = bundle.Samples[0].TutorTurns[0][:10]
	}
	modified.Ratings[1] = disagreement
	result, err := ScoreBlinded(bundle, key, modified)
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != "partial" || result.DisputedSamples != 1 {
		t.Fatal("review disagreement was silently merged", result)
	}
	adjudication := rating(disagreement.BlindID, "independent-adjudicator", 1, bundle.Samples[0].TutorTurns[0][:10])
	modified.Adjudications = []Rating{adjudication}
	resolved, err := ScoreBlinded(bundle, key, modified)
	if err != nil || resolved.Status != "reviewed" || resolved.DisputedSamples != 1 {
		t.Fatal("valid adjudication not accepted", resolved, err)
	}
	modified.Ratings[1].Dimensions[0].Quote = "fabricated words not in the record"
	if _, err := ScoreBlinded(bundle, key, modified); err == nil {
		t.Fatal("accepted a fabricated review quotation")
	}
	key.Records[0].TranscriptSHA256 = Digest([]byte("changed transcript"))
	if _, err := ScoreBlinded(bundle, key, reviews); err == nil {
		t.Fatal("undetected tampering in a review sample")
	}
}
func TestReviewRequiresIndependentHumanRatings(t *testing.T) {
	suite := exampleSuite()
	first, _ := RunEvaluation(context.Background(), suite, "Tutor A", "baseline", "model", &stubClient{})
	second, _ := RunEvaluation(context.Background(), suite, "Tutor B", "foundation", "model", &stubClient{})
	bundle, key, err := Blind(suite, exampleRubrics(), []Run{first, second})
	if err != nil {
		t.Fatal(err)
	}
	report, err := ScoreBlinded(bundle, key, Reviews{Version: Version})
	if err != nil {
		t.Fatal(err)
	}
	if report.Status != "partial" || report.MeanPairedDifference != nil || report.ReviewerCount != 0 {
		t.Fatal("model-only run was falsely represented as human reviewed", report)
	}
	sample := bundle.Samples[0]
	review := rating(sample.BlindID, "reviewer-one", 2, sample.TutorTurns[0][:10])
	if _, err = ScoreBlinded(bundle, key, Reviews{Version: Version, Ratings: []Rating{review, review}}); err == nil {
		t.Fatal("duplicate reviewer identity counted as independent review")
	}
}
