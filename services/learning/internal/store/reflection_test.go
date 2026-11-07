package store

import (
	"math"
	"path/filepath"
	"testing"
)

func TestLearnerReasoningAndConfidenceRemainSeparateFromGrades(t *testing.T) {
	path := filepath.Join(t.TempDir(), "reflect.sqlite")
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	c, err := s.AddConcept("analysis", "Analysis", "Rigorous explanations", "mathematics")
	if err != nil {
		t.Fatal(err)
	}
	_ = c
	problem, err := s.CreateProblem("Derivative", "Differentiate x^2 and explain why", "derivation", "independent", "challenging", []string{"analysis"}, "mathematics", "user", "", "", "")
	if err != nil {
		t.Fatal(err)
	}
	pid := problem["id"].(string)
	certainty := 0.30
	first, err := s.SaveProblemDraftWithReflection(pid, "2*x", "By the definition of a derivative using a limit.", &certainty)
	if err != nil {
		t.Fatal(err)
	}
	if first["duplicate"] != false {
		t.Fatal("first attempt duplicated")
	}
	copied, err := s.SaveProblemDraftWithReflection(pid, "2*x", "By the definition of a derivative using a limit.", &certainty)
	if err != nil || copied["duplicate"] != true {
		t.Fatalf("expected idempotent save, got %v %v", copied, err)
	}
	updated, err := s.SaveProblemDraftWithReflection(pid, "2*x", "I have not established the limit step.", &certainty)
	if err != nil {
		t.Fatal(err)
	}
	if updated["draft_id"] == first["draft_id"] {
		t.Fatal("reasoning change reused the old revision")
	}
	drafts := updated["problem"].(Value)["drafts"].([]Value)
	if len(drafts) != 2 || drafts[0]["learner_reasoning"] == drafts[1]["learner_reasoning"] {
		t.Fatal("reasons not kept with exact revisions")
	}
	if drafts[1]["self_confidence"] != certainty {
		t.Fatal("learner confidence was not stored")
	}
	if updated["problem"].(Value)["final_confidence"] != nil {
		t.Fatal("learner confidence changed evaluator grade")
	}
	bad := math.NaN()
	if _, err = s.SaveProblemDraftWithReflection(pid, "x", "", &bad); err == nil {
		t.Fatal("accepted NaN confidence")
	}

	session, err := s.StartLearningSession("Understand analysis", "analysis", "learn", nil)
	if err != nil {
		t.Fatal(err)
	}
	sid := session["id"].(string)
	q, err := s.OpenLearningQuestion(sid, "analysis", "", "diagnostic", "Choose a valid definition", "multiple_choice",
		[]QuestionOption{{ID: "a", Label: "A limiting process"}, {ID: "b", Label: "A polynomial substitution"}}, "a", "Use a limit definition")
	if err != nil {
		t.Fatal(err)
	}
	questionID := q["id"].(string)
	over := 1.01
	if _, err = s.AnswerLearningQuestionWithReflection(questionID, "a", false, "A guess", &over); err == nil {
		t.Fatal("accepted confidence over 1")
	}
	low := 0.15
	answered, err := s.AnswerLearningQuestionWithReflection(questionID, "a", false, "I chose the limit, but cannot justify why.", &low)
	if err != nil {
		t.Fatal(err)
	}
	if answered["self_confidence"] != low || answered["learner_reasoning"] == "" {
		t.Fatal("question reflection not saved", answered)
	}
	if answered["expected_answer"] != nil {
		t.Fatal("ungraded answer key leaked")
	}
	graded, err := s.GradeLearningQuestion(questionID, "supported", "Correct option but incomplete justification", 0.98)
	if err != nil {
		t.Fatal(err)
	}
	if graded["confidence"] != float64(0.98) || graded["self_confidence"] != low {
		t.Fatal("grader and learner confidence conflated", graded)
	}
	if err = s.Close(); err != nil {
		t.Fatal(err)
	}
	s, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	reloaded, err := s.GetLearningQuestion(questionID)
	if err != nil {
		t.Fatal(err)
	}
	if reloaded["learner_reasoning"] != answered["learner_reasoning"] || reloaded["self_confidence"] != low {
		t.Fatal("question reflection did not survive restart", reloaded)
	}
	problemAfter, err := s.GetProblem(pid)
	if err != nil {
		t.Fatal(err)
	}
	if len(problemAfter["drafts"].([]Value)) != 2 {
		t.Fatal("draft reflections lost on restart")
	}
}

func TestLegacySubmissionAcceptsMissingReflection(t *testing.T) {
	s := newExtendedStore(t)
	if _, err := s.AddConcept("proof", "Proof", "Deductive proof", "logic"); err != nil {
		t.Fatal(err)
	}
	p, err := s.CreateProblem("Exercise", "Prove a trivial identity", "proof", "independent", "foundational", []string{"proof"}, "logic", "user", "", "", "")
	if err != nil {
		t.Fatal(err)
	}
	result, err := s.SaveProblemDraft(p["id"].(string), "A proof without a separate confidence rating")
	if err != nil {
		t.Fatal(err)
	}
	drafts := result["problem"].(Value)["drafts"].([]Value)
	if len(drafts) != 1 || drafts[0]["self_confidence"] != nil {
		t.Fatal("missing self-confidence was coerced into a value")
	}
}
