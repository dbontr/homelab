package teacheval

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"sort"
	"strings"
)

type BlindSample struct {
	BlindID      string    `json:"blind_id"`
	Domain       string    `json:"domain"`
	LearnerTurns []string  `json:"learner_turns"`
	TutorTurns   []string  `json:"tutor_turns"`
	Rubric       Criterion `json:"rubric"`
}

type BlindBundle struct {
	Version int           `json:"version"`
	Samples []BlindSample `json:"samples"`
}

type KeyRecord struct {
	BlindID          string `json:"blind_id"`
	CaseID           string `json:"case_id"`
	Condition        string `json:"condition"`
	Model            string `json:"model"`
	TranscriptSHA256 string `json:"transcript_sha256"`
	Error            string `json:"error,omitempty"`
}

type BlindKey struct {
	Version     int         `json:"version"`
	SuiteSHA256 string      `json:"suite_sha256"`
	Records     []KeyRecord `json:"records"`
}

func randomBlindID() (string, error) {
	var data [16]byte
	if _, err := rand.Read(data[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(data[:]), nil
}

func TranscriptDigest(messages []Message) string {
	bytes, _ := json.Marshal(messages)
	return Digest(bytes)
}

// Blind separates transcripts and scoring criteria from model identity, prompt
// policy, condition labels, and endpoint metadata. It is not evidence that
// reviewers were in fact independent or blinded.
func Blind(s Suite, rubrics Rubrics, runs []Run) (BlindBundle, BlindKey, error) {
	if err := ValidateRubrics(s, rubrics); err != nil {
		return BlindBundle{}, BlindKey{}, err
	}
	if len(runs) < 2 || len(runs) > 20 {
		return BlindBundle{}, BlindKey{}, errors.New("at least two and no more than twenty runs required")
	}
	byCase := map[string]Case{}
	byCriterion := map[string]Criterion{}
	for _, c := range s.Cases {
		byCase[c.ID] = c
	}
	for _, c := range rubrics.Criteria {
		byCriterion[c.ID] = c
	}
	bundle := BlindBundle{Version: Version, Samples: []BlindSample{}}
	key := BlindKey{Version: Version, SuiteSHA256: SuiteDigest(s), Records: []KeyRecord{}}
	seen := map[string]bool{}
	for _, run := range runs {
		if run.Version != Version || run.Mode != "prompt_only_chat_completions" || run.SuiteSHA256 != key.SuiteSHA256 || !validID(run.Condition) || run.Model == "" || run.PolicySHA256 == "" {
			return BlindBundle{}, BlindKey{}, errors.New("run schema, suite digest, model or policy is invalid")
		}
		if len(run.Attempts) != len(s.Cases) {
			return BlindBundle{}, BlindKey{}, errors.New("run does not contain exactly one attempt per case")
		}
		for _, attempt := range run.Attempts {
			c, ok := byCase[attempt.CaseID]
			if !ok {
				return BlindBundle{}, BlindKey{}, fmt.Errorf("unknown case %s", attempt.CaseID)
			}
			pair := run.Condition + "|" + c.ID
			if seen[pair] {
				return BlindBundle{}, BlindKey{}, fmt.Errorf("duplicate condition-case pair %s", pair)
			}
			seen[pair] = true
			if len(attempt.Messages) > len(c.LearnerTurns)*2 || len(attempt.Messages)%2 != 0 && attempt.Error == "" {
				return BlindBundle{}, BlindKey{}, fmt.Errorf("malformed transcript for %s", c.ID)
			}
			for i, message := range attempt.Messages {
				expectedRole := "user"
				if i%2 == 1 {
					expectedRole = "assistant"
				}
				if message.Role != expectedRole || strings.TrimSpace(message.Content) == "" {
					return BlindBundle{}, BlindKey{}, fmt.Errorf("invalid conversation at %s", c.ID)
				}
				if i%2 == 0 && message.Content != c.LearnerTurns[i/2] {
					return BlindBundle{}, BlindKey{}, fmt.Errorf("learner script differs at %s", c.ID)
				}
			}
			complete := attempt.Error == "" && len(attempt.Messages) == 2*len(c.LearnerTurns)
			if attempt.Error == "" && !complete {
				return BlindBundle{}, BlindKey{}, fmt.Errorf("incomplete attempt without error: %s", c.ID)
			}
			id, err := randomBlindID()
			if err != nil {
				return BlindBundle{}, BlindKey{}, err
			}
			key.Records = append(key.Records, KeyRecord{BlindID: id, CaseID: c.ID, Condition: run.Condition, Model: run.Model, TranscriptSHA256: TranscriptDigest(attempt.Messages), Error: attempt.Error})
			if !complete {
				continue
			}
			learner, tutor := []string{}, []string{}
			for _, m := range attempt.Messages {
				if m.Role == "user" {
					learner = append(learner, m.Content)
				} else {
					tutor = append(tutor, m.Content)
				}
			}
			bundle.Samples = append(bundle.Samples, BlindSample{BlindID: id, Domain: c.Domain, LearnerTurns: learner, TutorTurns: tutor, Rubric: byCriterion[c.ID]})
		}
	}
	for i := len(bundle.Samples) - 1; i > 0; i-- {
		random, err := rand.Int(rand.Reader, big.NewInt(int64(i+1)))
		if err != nil {
			return BlindBundle{}, BlindKey{}, err
		}
		j := int(random.Int64())
		bundle.Samples[i], bundle.Samples[j] = bundle.Samples[j], bundle.Samples[i]
	}
	return bundle, key, nil
}

type DimensionReview struct {
	Dimension   string `json:"dimension"`
	Score       int    `json:"score"`
	Quote       string `json:"quote"`
	Explanation string `json:"explanation"`
}

type Rating struct {
	BlindID    string            `json:"blind_id"`
	ReviewerID string            `json:"reviewer_id"`
	Dimensions []DimensionReview `json:"dimensions"`
}

type Reviews struct {
	Version       int      `json:"version"`
	Ratings       []Rating `json:"ratings"`
	Adjudications []Rating `json:"adjudications,omitempty"`
}

func validateRating(sample BlindSample, rating Rating) (map[string]int, error) {
	if rating.BlindID != sample.BlindID || !validID(rating.ReviewerID) || len(rating.Dimensions) != len(Dimensions) {
		return nil, errors.New("invalid review identity or incomplete rubric dimensions")
	}
	expected := map[string]bool{}
	for _, d := range Dimensions {
		expected[d] = true
	}
	scores := map[string]int{}
	for _, d := range rating.Dimensions {
		if !expected[d.Dimension] || d.Score < 0 || d.Score > 2 {
			return nil, fmt.Errorf("unknown dimension or score %s", d.Dimension)
		}
		delete(expected, d.Dimension)
		if len(d.Explanation) < 12 || len(d.Explanation) > 1800 {
			return nil, fmt.Errorf("review explanation must describe evidence for %s", d.Dimension)
		}
		if len(d.Quote) > 1000 {
			return nil, errors.New("evidence quote exceeds 1000 bytes")
		}
		if d.Score > 0 && strings.TrimSpace(d.Quote) == "" {
			return nil, fmt.Errorf("positive score requires a verbatim quote for %s", d.Dimension)
		}
		if d.Quote != "" {
			found := false
			for _, turn := range sample.TutorTurns {
				if strings.Contains(turn, d.Quote) {
					found = true
					break
				}
			}
			if !found {
				return nil, fmt.Errorf("quote for %s is not in the assistant transcript", d.Dimension)
			}
		}
		scores[d.Dimension] = d.Score
	}
	if len(expected) > 0 {
		return nil, errors.New("missing rubric dimensions")
	}
	return scores, nil
}

type ConditionMetrics struct {
	Condition         string   `json:"condition"`
	Attempts          int      `json:"attempts"`
	Completed         int      `json:"completed"`
	Reviewed          int      `json:"reviewed"`
	Unreviewed        int      `json:"unreviewed"`
	Unresolved        int      `json:"unresolved"`
	MeanScore         *float64 `json:"mean_score"`
	RestraintFailures int      `json:"restraint_failures"`
}

type EvaluationReport struct {
	Version               int                `json:"version"`
	EvidenceType          string             `json:"evidence_type"`
	LearningGainsMeasured bool               `json:"learning_gains_measured"`
	Status                string             `json:"status"`
	MatchedPairs          int                `json:"matched_pairs"`
	MeanPairedDifference  *float64           `json:"mean_paired_difference"`
	DimensionAgreement    *float64           `json:"dimension_agreement"`
	DisputedSamples       int                `json:"disputed_samples"`
	ReviewerCount         int                `json:"reviewer_count"`
	Conditions            []ConditionMetrics `json:"conditions"`
	Limitations           string             `json:"limitations"`
}

// ScoreBlinded requires two distinct complete human reviewer records. A
// disagreement requires a third independent adjudication; no model grade is
// used as the answer key or a substitute for human evaluation.
func ScoreBlinded(bundle BlindBundle, key BlindKey, reviews Reviews) (EvaluationReport, error) {
	if bundle.Version != Version || key.Version != Version || reviews.Version != Version {
		return EvaluationReport{}, errors.New("unsupported evaluation schema")
	}
	samples := map[string]BlindSample{}
	for _, s := range bundle.Samples {
		if s.BlindID == "" || len(s.LearnerTurns) != len(s.TutorTurns) {
			return EvaluationReport{}, errors.New("invalid blind sample")
		}
		if _, ok := samples[s.BlindID]; ok {
			return EvaluationReport{}, errors.New("duplicate blind ID")
		}
		samples[s.BlindID] = s
	}
	records := map[string]KeyRecord{}
	conditionStats := map[string]*ConditionMetrics{}
	for _, r := range key.Records {
		if r.BlindID == "" || !validID(r.Condition) || r.CaseID == "" {
			return EvaluationReport{}, errors.New("invalid private identity map")
		}
		if _, ok := records[r.BlindID]; ok {
			return EvaluationReport{}, errors.New("duplicate private identity map entry")
		}
		records[r.BlindID] = r
		m := conditionStats[r.Condition]
		if m == nil {
			m = &ConditionMetrics{Condition: r.Condition}
			conditionStats[r.Condition] = m
		}
		m.Attempts++
		if s, ok := samples[r.BlindID]; ok {
			msgs := []Message{}
			for i := range s.LearnerTurns {
				msgs = append(msgs, Message{Role: "user", Content: s.LearnerTurns[i]}, Message{Role: "assistant", Content: s.TutorTurns[i]})
			}
			if TranscriptDigest(msgs) != r.TranscriptSHA256 || r.Error != "" {
				return EvaluationReport{}, errors.New("blind transcript does not match its private digest")
			}
			m.Completed++
		} else if r.Error == "" {
			return EvaluationReport{}, errors.New("a completed mapped transcript is missing")
		}
	}
	for id := range samples {
		if _, ok := records[id]; !ok {
			return EvaluationReport{}, errors.New("unmapped blind sample")
		}
	}
	if len(conditionStats) < 2 {
		return EvaluationReport{}, errors.New("at least two conditions are required")
	}
	bySample := map[string][]Rating{}
	adjudicated := map[string]Rating{}
	reviewerIDs := map[string]bool{}
	for _, r := range reviews.Ratings {
		s, ok := samples[r.BlindID]
		if !ok {
			return EvaluationReport{}, fmt.Errorf("rating refers to unknown sample %s", r.BlindID)
		}
		if _, err := validateRating(s, r); err != nil {
			return EvaluationReport{}, err
		}
		if len(bySample[r.BlindID]) >= 2 {
			return EvaluationReport{}, errors.New("more than two primary reviewers for one sample")
		}
		for _, existing := range bySample[r.BlindID] {
			if existing.ReviewerID == r.ReviewerID {
				return EvaluationReport{}, errors.New("repeated reviewer identity")
			}
		}
		bySample[r.BlindID] = append(bySample[r.BlindID], r)
		reviewerIDs[r.ReviewerID] = true
	}
	for _, a := range reviews.Adjudications {
		s, ok := samples[a.BlindID]
		if !ok {
			return EvaluationReport{}, errors.New("adjudication refers to unknown sample")
		}
		if _, err := validateRating(s, a); err != nil {
			return EvaluationReport{}, err
		}
		if _, exists := adjudicated[a.BlindID]; exists {
			return EvaluationReport{}, errors.New("duplicate adjudication")
		}
		for _, r := range bySample[a.BlindID] {
			if r.ReviewerID == a.ReviewerID {
				return EvaluationReport{}, errors.New("adjudicator must be independent of primary reviewers")
			}
		}
		adjudicated[a.BlindID] = a
		reviewerIDs[a.ReviewerID] = true
	}
	totals := map[string]float64{}
	scoredByCase := map[string]map[string]float64{}
	agreement, compared, disputed := 0, 0, 0
	for blindID, sample := range samples {
		rec := records[blindID]
		metrics := conditionStats[rec.Condition]
		ratings := bySample[blindID]
		if len(ratings) < 2 {
			metrics.Unreviewed++
			continue
		}
		first, _ := validateRating(sample, ratings[0])
		second, _ := validateRating(sample, ratings[1])
		final := map[string]int{}
		conflict := false
		for _, d := range Dimensions {
			compared++
			if first[d] == second[d] {
				agreement++
				final[d] = first[d]
				continue
			}
			conflict = true
			adj, has := adjudicated[blindID]
			if has {
				scores, _ := validateRating(sample, adj)
				final[d] = scores[d]
			}
		}
		if conflict {
			disputed++
		}
		if len(final) != len(Dimensions) {
			metrics.Unresolved++
			continue
		}
		var score float64
		for _, d := range Dimensions {
			score += float64(final[d])
		}
		score /= float64(len(Dimensions) * 2)
		metrics.Reviewed++
		totals[rec.Condition] += score
		if final["restraint"] == 0 {
			metrics.RestraintFailures++
		}
		if scoredByCase[rec.CaseID] == nil {
			scoredByCase[rec.CaseID] = map[string]float64{}
		}
		scoredByCase[rec.CaseID][rec.Condition] = score
	}
	conditions := make([]ConditionMetrics, 0, len(conditionStats))
	names := make([]string, 0, len(conditionStats))
	allComplete := true
	for condition, m := range conditionStats {
		names = append(names, condition)
		if m.Reviewed != m.Attempts {
			allComplete = false
		}
		if m.Reviewed > 0 {
			mean := totals[condition] / float64(m.Reviewed)
			m.MeanScore = &mean
		}
	}
	sort.Strings(names)
	for _, name := range names {
		conditions = append(conditions, *conditionStats[name])
	}
	paired, delta := 0, float64(0)
	// A paired estimate is defined only for exactly two tested conditions.
	if len(names) == 2 {
		for _, scores := range scoredByCase {
			left, yesLeft := scores[names[0]]
			right, yesRight := scores[names[1]]
			if yesLeft && yesRight {
				paired++
				delta += right - left
			}
		}
	}
	var pairedDifference, agreementFraction *float64
	if paired > 0 {
		v := delta / float64(paired)
		pairedDifference = &v
	}
	if compared > 0 {
		v := float64(agreement) / float64(compared)
		agreementFraction = &v
	}
	status := "partial"
	if allComplete {
		status = "reviewed"
	}
	return EvaluationReport{Version: Version, EvidenceType: "blinded_prompt_only_model_behavior", LearningGainsMeasured: false, Status: status,
		MatchedPairs: paired, MeanPairedDifference: pairedDifference, DimensionAgreement: agreementFraction,
		DisputedSamples: disputed, ReviewerCount: len(reviewerIDs), Conditions: conditions,
		Limitations: "Scores require two distinct human reviewers and adjudication of disagreements. Reviewer independence cannot be verified by software. The experiment does not execute the ChatGPT MCP host, inspect private knowledge graphs or demonstrate real learner improvement. A small, scripted benchmark cannot establish educational efficacy.",
	}, nil
}
