package store

import "testing"

func TestNextLearningActionResumesCommittedQuestionAndRepairsGap(t *testing.T) {
	s := newTestStore(t)
	defer s.Close()
	if _, err := s.AddConcept("proof", "Proof", "Justified deduction", "logic"); err != nil {
		t.Fatal(err)
	}
	session, err := s.StartLearningSession("Construct a proof", "proof", "learn", nil)
	if err != nil {
		t.Fatal(err)
	}
	id := session["id"].(string)
	action, err := s.NextLearningAction(id)
	if err != nil || action["action"] != "pose_diagnostic" {
		t.Fatalf("initial action: %v %v", action, err)
	}
	question, err := s.OpenLearningQuestion(id, "proof", "", "diagnostic", "What warrants the conclusion?", "free_response", nil, "A valid inference from premises", "Examine the inferential rule")
	if err != nil {
		t.Fatal(err)
	}
	qid := question["id"].(string)
	for _, expected := range []string{"await_answer", "grade_answer"} {
		action, err = s.NextLearningAction(id)
		if err != nil || action["action"] != expected || action["question_id"] != qid {
			t.Fatalf("pending action: %v %v", action, err)
		}
		if expected == "await_answer" {
			if _, err = s.AnswerLearningQuestion(qid, "I do not know", false); err != nil {
				t.Fatal(err)
			}
		}
	}
	if _, err = s.GradeLearningQuestion(qid, "gap", "Missing inference rule", .96); err != nil {
		t.Fatal(err)
	}
	action, err = s.NextLearningAction(id)
	if err != nil || action["action"] != "plan_repair" || action["concept_id"] != "proof" {
		t.Fatalf("repair action: %v %v", action, err)
	}
	if _, err = s.AdvanceLearningSession(id, "plan", nil); err != nil {
		t.Fatal(err)
	}
	action, err = s.NextLearningAction(id)
	if err != nil || action["action"] != "build_plan" {
		t.Fatalf("plan action: %v %v", action, err)
	}
	if _, err = s.BuildLearningPlan(id, nil, nil); err != nil {
		t.Fatal(err)
	}
	action, err = s.NextLearningAction(id)
	if err != nil || action["action"] != "advance_phase" || action["next_phase"] != "teach" {
		t.Fatalf("teaching transition: %v %v", action, err)
	}
	if _, err = s.AdvanceLearningSession(id, "teach", nil); err != nil {
		t.Fatal(err)
	}
	action, err = s.NextLearningAction(id)
	if err != nil || action["action"] != "supply_justification" {
		t.Fatalf("missing justification: %v %v", action, err)
	}
}

func TestNextLearningActionProtectsSolveModeAndSurvivesRestart(t *testing.T) {
	path := t.TempDir() + "/orchestration.db"
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.AddConcept("programming", "Programming", "Construct working programs", "engineering"); err != nil {
		t.Fatal(err)
	}
	session, err := s.StartLearningSession("Build a program", "programming", "solve", nil)
	if err != nil {
		t.Fatal(err)
	}
	id := session["id"].(string)
	action, err := s.NextLearningAction(id)
	if err != nil || action["action"] != "assign_challenge" {
		t.Fatalf("solve action: %v %v", action, err)
	}
	problem, err := s.CreateProblem("Construct an algorithm", "Create a robust parsing algorithm", "programming", "independent", "challenging", []string{"programming"}, "computer science", "user", "", "", "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.SaveProblemDraft(problem["id"].(string), "My initial incomplete algorithm"); err != nil {
		t.Fatal(err)
	}
	action, err = s.NextLearningAction(id)
	if err != nil || action["action"] != "resume_problem" || action["problem_id"] != problem["id"] {
		t.Fatalf("resume action: %v %v", action, err)
	}
	if err = s.Close(); err != nil {
		t.Fatal(err)
	}
	s, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	action, err = s.NextLearningAction(id)
	if err != nil || action["action"] != "resume_problem" || action["problem_id"] != problem["id"] {
		t.Fatalf("persistent action: %v %v", action, err)
	}
}

func TestCompletionRequiresDemonstratedMastery(t *testing.T) {
	s := newTestStore(t)
	if _, err := s.AddConcept("logic", "Logic", "Proof methods", "logic"); err != nil {
		t.Fatal(err)
	}
	session, err := s.StartLearningSession("Master logic", "logic", "assess", nil)
	if err != nil {
		t.Fatal(err)
	}
	id := session["id"].(string)
	_, err = s.AdvanceLearningSession(id, "complete", nil)
	if err == nil {
		t.Fatal("session closed without learner evidence")
	}
	action, err := s.NextLearningAction(id)
	if err != nil || action["action"] != "reassess_independently" {
		t.Fatalf("expected reassessment, got %v %v", action, err)
	}
}
