package store

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type pedagogyCase struct {
	ID                  string           `json:"id"`
	Domain              string           `json:"domain"`
	Concept             string           `json:"concept"`
	Topic               string           `json:"topic"`
	Prompt              string           `json:"prompt"`
	Options             []QuestionOption `json:"options"`
	CorrectOption       string           `json:"correct_option"`
	LearnerOption       string           `json:"learner_option"`
	LearnerExplanation  string           `json:"learner_explanation"`
	ReasoningVerdict    string           `json:"reasoning_verdict"`
	Confidence          float64          `json:"confidence"`
	ExpectedProbe       string           `json:"expected_probe"`
	ExpectedAction      string           `json:"expected_action"`
	KnownMisconception  string           `json:"known_misconception"`
	IndependentTransfer string           `json:"independent_transfer"`
}

func requirePedagogyValue(t *testing.T) func(Value, error) Value {
	t.Helper()
	return func(value Value, err error) Value {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
		return value
	}
}

func TestPedagogyCrossDisciplineDiagnosticContracts(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("testdata", "pedagogy_scenarios.json"))
	if err != nil {
		t.Fatal(err)
	}
	var suite struct {
		SchemaVersion int            `json:"schema_version"`
		Kind          string         `json:"kind"`
		Cases         []pedagogyCase `json:"cases"`
	}
	if err := json.Unmarshal(raw, &suite); err != nil {
		t.Fatal(err)
	}
	if suite.SchemaVersion != 1 || suite.Kind != "synthetic_contract_cases" || len(suite.Cases) < 8 {
		t.Fatal("invalid synthetic pedagogical contract suite")
	}
	domains := map[string]bool{}
	identifiers := map[string]bool{}
	for _, scenario := range suite.Cases {
		t.Run(scenario.ID, func(t *testing.T) {
			mustResult := requirePedagogyValue(t)
			if identifiers[scenario.ID] {
				t.Fatal("duplicate scenario identifier")
			}
			identifiers[scenario.ID] = true
			domains[scenario.Domain] = true
			if scenario.ID == "" || scenario.Domain == "" || scenario.Topic == "" || scenario.Prompt == "" ||
				scenario.IndependentTransfer == "" || len(scenario.Options) < 2 ||
				scenario.ExpectedProbe == "" || scenario.ExpectedAction == "" {
				t.Fatal("incomplete scenario")
			}
			if scenario.ReasoningVerdict != "" && strings.TrimSpace(scenario.LearnerExplanation) == "" {
				t.Fatal("reasoning cannot be graded without a recorded learner explanation")
			}
			s := newTestStore(t)
			mustResult(s.AddConcept(scenario.Concept, scenario.Topic, "Evaluate the concept and its supporting reasoning", scenario.Domain))
			session := mustResult(s.StartLearningSession("Understand "+scenario.Topic, scenario.Concept, "learn", nil))
			sid := session["id"].(string)
			question := mustResult(s.OpenLearningQuestion(sid, scenario.Concept, "", "diagnostic", scenario.Prompt, "multiple_choice",
				scenario.Options, scenario.CorrectOption, "Compare the answer against the committed key and examine all supporting premises"))
			id := question["id"].(string)
			if strings.Contains(js(question), scenario.CorrectOption) && question["expected_answer"] != nil {
				t.Fatal("ungraded question exposed its key")
			}
			if next := mustResult(s.NextLearningAction(sid)); next["action"] != "await_answer" || next["question_id"] != id {
				t.Fatal("question was not retained as the next action", next)
			}
			answer := mustResult(s.AnswerLearningQuestionWithReflection(id, scenario.LearnerOption, false, scenario.LearnerExplanation, &scenario.Confidence))
			if answer["self_confidence"] != scenario.Confidence {
				t.Fatal("recorded confidence changed before grading", answer)
			}
			if next := mustResult(s.NextLearningAction(sid)); next["action"] != "grade_answer" {
				t.Fatal("pending learner answer not preserved for assessment", next)
			}
			verdict := "gap"
			if scenario.LearnerOption == scenario.CorrectOption {
				verdict = "supported"
			}
			var grade Value
			if scenario.ReasoningVerdict == "" {
				grade = mustResult(s.GradeLearningQuestion(id, verdict, "Against the precommitted key", .95))
			} else {
				grade = mustResult(s.GradeLearningQuestionWithReasoning(id, verdict, "Against the precommitted key", .95,
					scenario.ReasoningVerdict, "Curated explanation check: "+scenario.KnownMisconception))
			}
			if grade["answer_correct"] != (scenario.LearnerOption == scenario.CorrectOption) ||
				grade["reasoning_verdict"] != nil && grade["reasoning_verdict"] != scenario.ReasoningVerdict {
				t.Fatal("answer and reasoning judgments became conflated", grade)
			}
			if scenario.ReasoningVerdict == "" && grade["reasoning_verdict"] != nil {
				t.Fatal("an absent explanation received a fabricated reasoning grade", grade)
			}
			history := mustResult(s.GetLearningSession(sid))
			probes := history["probes"].([]Value)
			if len(probes) != 1 || probes[0]["outcome"] != scenario.ExpectedProbe {
				t.Fatalf("diagnostic result does not match the curated case: %v", history)
			}
			next := mustResult(s.NextLearningAction(sid))
			if next["action"] != scenario.ExpectedAction {
				t.Fatalf("misconception repair route mismatch: %v, want %s", next, scenario.ExpectedAction)
			}
			if observed := mustResult(s.Concept(scenario.Concept))["mastery"].(Value); observed["level"] != 0 || observed["meets_target"] == true {
				t.Fatal("diagnostic grading awarded durable mastery", observed)
			}
			report := mustResult(s.ConfidenceCalibration(scenario.Concept, 25))
			choices := report["choice_accuracy"].(Value)
			if choices["sample_count"] != 1 || choices["mean_confidence"] != scenario.Confidence {
				t.Fatal("objective calibration omitted the scenario", choices)
			}
		})
	}
	if len(domains) < 8 {
		t.Fatalf("expected at least eight disciplines, saw %v", domains)
	}
}

func TestPedagogyRehearsedProblemsDoNotEstablishIndependentTransfer(t *testing.T) {
	domains := []struct {
		id, domain, statement string
	}{
		{"math", "mathematics", "Solve a nonroutine polynomial equation under an unfamiliar constraint."},
		{"circuits", "electrical engineering", "Analyze a nonroutine circuit with unfamiliar component values."},
		{"history", "history", "Compare conflicting source accounts of an unfamiliar historical event."},
		{"craft", "practical crafts", "Diagnose an unfamiliar physical failure without applying power."},
	}
	for _, subject := range domains {
		t.Run(subject.id, func(t *testing.T) {
			mustResult := requirePedagogyValue(t)
			s := newTestStore(t)
			mustResult(s.AddConcept(subject.id, subject.domain, "Independent transfer", subject.domain))
			for i := 0; i < 3; i++ {
				problem := mustResult(s.CreateProblem(subject.id, subject.statement, "other", "independent", "challenging",
					[]string{subject.id}, subject.domain, "user", "", "", ""))
				pid := problem["id"].(string)
				mustResult(s.SaveProblemDraft(pid, "An independently reasoned attempt"))
				mustResult(s.FinishProblem(pid, "solved", "A scripted evaluator judged the response supported", .94, []string{subject.id}))
			}
			portfolio := mustResult(s.MasteryPortfolio(subject.id))
			if portfolio["distinct_difficult_solutions"] != 1 || portfolio["full_mastery_evidence_recorded"] != false {
				t.Fatal("near-identical problem statements manufactured novel transfer", portfolio)
			}
			assisted := mustResult(s.CreateProblem(subject.id+" guided", subject.statement+" with a small change", "other", "guided",
				"challenging", []string{subject.id}, subject.domain, "user", "", "", ""))
			pid := assisted["id"].(string)
			mustResult(s.SaveProblemDraft(pid, "A response prepared with guidance"))
			if _, err := s.FinishProblem(pid, "solved", "The response is correct but assisted", .99, []string{subject.id}); err == nil {
				t.Fatal("guided solution was allowed to earn independent credit")
			}
		})
	}
}
