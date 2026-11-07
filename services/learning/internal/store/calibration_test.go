package store

import (
	"math"
	"testing"
)

func makeCalibrationQuestion(t *testing.T, s *Store, conceptID, correctChoice, answer string, confidence *float64, assisted bool, reasoning, reasoningVerdict string) Value {
	t.Helper()
	session, err := s.StartLearningSession("Calibrate foundations", conceptID, "learn", nil)
	if err != nil {
		t.Fatal(err)
	}
	question, err := s.OpenLearningQuestion(session["id"].(string), conceptID, "", "diagnostic", "Choose the supported claim", "multiple_choice",
		[]QuestionOption{{ID: "a", Label: "First claim"}, {ID: "b", Label: "Second claim"}}, correctChoice, "Compare with the committed answer and justify the choice")
	if err != nil {
		t.Fatal(err)
	}
	id := question["id"].(string)
	if question["expected_answer"] != nil {
		t.Fatal("ungraded question disclosed the answer")
	}
	_, err = s.AnswerLearningQuestionWithReflection(id, answer, assisted, reasoning, confidence)
	if err != nil {
		t.Fatal(err)
	}
	if reasoningVerdict != "" {
		_, err = s.GradeLearningQuestionWithReasoning(id, outcomeForChoice(correctChoice, answer), "Checked against the committed answer", .96, reasoningVerdict, "Explanatory premises were separately evaluated")
	} else {
		_, err = s.GradeLearningQuestion(id, outcomeForChoice(correctChoice, answer), "Checked against the committed answer", .96)
	}
	if err != nil {
		t.Fatal(err)
	}
	full, err := s.GetLearningSession(session["id"].(string))
	if err != nil {
		t.Fatal(err)
	}
	return Value{"question_id": id, "question": question, "session": full}
}

func outcomeForChoice(correct, answer string) string {
	if correct == answer {
		return "supported"
	}
	return "gap"
}

func TestCorrectChoiceWithoutJustifiedReasoningIsNotSupportedDiagnosis(t *testing.T) {
	s := newExtendedStore(t)
	if _, err := s.AddConcept("algebra", "Algebra", "Algebraic reasoning", "mathematics"); err != nil {
		t.Fatal(err)
	}
	confidence := 0.99
	guessed := makeCalibrationQuestion(t, s, "algebra", "a", "a", &confidence, false, "", "")
	questionID := guessed["question_id"].(string)
	answer, err := s.GetLearningQuestion(questionID)
	if err != nil {
		t.Fatal(err)
	}
	if answer["answer_correct"] != true || answer["reasoning_verdict"] != nil {
		t.Fatalf("correct choice was conflated with proof: %v", answer)
	}
	probes := guessed["session"].(Value)["probes"].([]Value)
	if len(probes) != 1 || probes[0]["outcome"] != "uncertain" {
		t.Fatal("a correct guess must not establish a diagnostic prerequisite", probes)
	}
	if _, err := s.GradeLearningQuestionWithReasoning(questionID, "supported", "Repeat grade", .98, "supported", "A fabricated later argument"); err == nil {
		t.Fatal("a committed answer was graded twice")
	}
	explained := makeCalibrationQuestion(t, s, "algebra", "a", "a", &confidence, false, "The result follows by distributing multiplication.", "supported")
	explainedQ, err := s.GetLearningQuestion(explained["question_id"].(string))
	if err != nil {
		t.Fatal(err)
	}
	if explainedQ["reasoning_verdict"] != "supported" || explainedQ["reasoning_evaluation"] == nil {
		t.Fatal("independent reasoning assessment was not stored", explainedQ)
	}
	probes = explained["session"].(Value)["probes"].([]Value)
	if len(probes) != 1 || probes[0]["outcome"] != "supported" {
		t.Fatal("a supported explanation should inform diagnosis", probes)
	}
	badReasoning := makeCalibrationQuestion(t, s, "algebra", "a", "a", &confidence, false, "I think this works because every polynomial is linear.", "gap")
	probes = badReasoning["session"].(Value)["probes"].([]Value)
	if len(probes) != 1 || probes[0]["outcome"] != "gap" {
		t.Fatal("unsupported reasoning was disguised by correct choice", probes)
	}
	// The evaluator cannot invent an explanation that the learner did not write.
	another := makeCalibrationQuestion(t, s, "algebra", "b", "a", &confidence, false, "", "")
	if another["session"] == nil {
		t.Fatal("missing stored diagnostic")
	}
	session, err := s.StartLearningSession("Validate supported explanation", "algebra", "learn", nil)
	if err != nil {
		t.Fatal(err)
	}
	q, err := s.OpenLearningQuestion(session["id"].(string), "algebra", "", "diagnostic", "Which statement is true?", "multiple_choice", []QuestionOption{{ID: "a", Label: "A"}, {ID: "b", Label: "B"}}, "a", "Reason about the statement")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.AnswerLearningQuestion(q["id"].(string), "a", false); err != nil {
		t.Fatal(err)
	}
	if _, err = s.GradeLearningQuestionWithReasoning(q["id"].(string), "supported", "Correct answer", .95, "supported", "No supporting statement exists"); err == nil {
		t.Fatal("accepted a made-up explanation")
	}
	if _, err = s.GradeLearningQuestion(q["id"].(string), "supported", "Correct answer", math.NaN()); err == nil {
		t.Fatal("accepted NaN evaluator confidence")
	}
}

func TestConfidenceCalibrationUsesPrecommittedAnswersNotEvaluatorConfidence(t *testing.T) {
	s := newExtendedStore(t)
	for _, concept := range []struct{ id, domain string }{{"algebra", "mathematics"}, {"history", "history"}, {"biology", "biology"}} {
		if _, err := s.AddConcept(concept.id, concept.id, "Learner calibration", concept.domain); err != nil {
			t.Fatal(err)
		}
	}
	high, low := .9, .1
	makeCalibrationQuestion(t, s, "algebra", "a", "a", &high, false, "", "")
	makeCalibrationQuestion(t, s, "algebra", "a", "b", &high, false, "", "")
	makeCalibrationQuestion(t, s, "history", "a", "b", &low, false, "", "")
	makeCalibrationQuestion(t, s, "history", "a", "a", &low, false, "", "")
	makeCalibrationQuestion(t, s, "biology", "a", "a", nil, false, "", "")
	makeCalibrationQuestion(t, s, "biology", "a", "b", &high, true, "", "")
	report, err := s.ConfidenceCalibration("", 100)
	if err != nil {
		t.Fatal(err)
	}
	if report["mastery_unchanged"] != true {
		t.Fatal("calibration changed mastery")
	}
	c := report["choice_accuracy"].(Value)
	if c["sample_count"] != 4 || c["eligible_examined"] != 5 || c["missing_self_confidence"] != 1 {
		t.Fatal("missing or assisted responses entered calibration", c)
	}
	if math.Abs(c["observed_accuracy"].(float64)-.5) > 1e-9 || math.Abs(c["mean_confidence"].(float64)-.5) > 1e-9 {
		t.Fatal("reported confidence and objective accuracy are conflated", c)
	}
	if math.Abs(c["brier_score"].(float64)-.41) > 1e-9 {
		t.Fatal("incorrect Brier score", c)
	}
	if len(c["confident_errors"].([]Value)) != 1 || len(c["underconfident_successes"].([]Value)) != 1 {
		t.Fatal("calibration errors and hidden successes lost", c)
	}
	limited, err := s.ConfidenceCalibration("algebra", 100)
	if err != nil || limited["choice_accuracy"].(Value)["sample_count"] != 2 {
		t.Fatalf("concept-scoped calibration failed: %v %v", limited, err)
	}
	if _, err := s.ConfidenceCalibration("missing-concept", 10); err == nil {
		t.Fatal("unknown concept accepted")
	}
	if _, err := s.ConfidenceCalibration("", 10000); err == nil {
		t.Fatal("unbounded calibration query accepted")
	}
	empty, err := s.ConfidenceCalibration("biology", 100)
	if err != nil {
		t.Fatal(err)
	}
	result := empty["choice_accuracy"].(Value)
	if result["sample_count"] != 0 || result["brier_score"] != nil || result["insufficient_samples"] != true {
		t.Fatal("missing confidence must remain missing", result)
	}
}

func TestSignedResultCalibrationRejectsConflictingReports(t *testing.T) {
	s, signer := trustedTestStore(t)
	if _, err := s.AddConcept("proof", "Proof", "Verified symbolic expressions", "mathematics"); err != nil {
		t.Fatal(err)
	}
	for _, example := range []struct {
		outcome    string
		confidence *float64
		conflict   bool
	}{
		{outcome: "pass", confidence: ptrConfidence(.8)},
		{outcome: "fail", confidence: ptrConfidence(.8)},
		{outcome: "pass", confidence: ptrConfidence(.9), conflict: true},
		{outcome: "pass", confidence: nil},
	} {
		problem, err := s.CreateProblem("Exact expression", "Check a polynomial identity", "calculation", "independent", "challenging", []string{"proof"}, "mathematics", "user", "", "", "")
		if err != nil {
			t.Fatal(err)
		}
		id := problem["id"].(string)
		if _, err := s.SaveProblemDraftWithReflection(id, "x^2+2*x+1", "The binomial identity is used.", example.confidence); err != nil {
			t.Fatal(err)
		}
		receipt, signature := signLatestDraft(t, s, signer, id, "symbolic", example.outcome)
		if _, err := s.RecordVerifiedProblemCheck(receipt, signature); err != nil {
			t.Fatal(err)
		}
		if example.conflict {
			dispute, sig := signLatestDraft(t, s, signer, id, "symbolic", "fail")
			if _, err := s.RecordVerifiedProblemCheck(dispute, sig); err != nil {
				t.Fatal(err)
			}
		}
		if _, err := s.FinishProblem(id, "incomplete", "Verified comparison recorded", .9, nil); err != nil {
			t.Fatal(err)
		}
	}
	report, err := s.ConfidenceCalibration("proof", 100)
	if err != nil {
		t.Fatal(err)
	}
	results := report["authenticated_result_accuracy"].(Value)
	if results["sample_count"] != 2 || results["conflicting_checks_excluded"] != 1 || results["missing_self_confidence"] != 1 {
		t.Fatal("invalid signed result calibration population", results)
	}
	if math.Abs(results["brier_score"].(float64)-.34) > 1e-9 {
		t.Fatal("signed result Brier score incorrect", results)
	}
	if results["insufficient_samples"] != true {
		t.Fatal("small sample did not receive an uncertainty warning", results)
	}
}

func ptrConfidence(value float64) *float64 { return &value }
