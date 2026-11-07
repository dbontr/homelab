package store

import (
	"strings"
	"testing"
)

func TestCommittedQuestionLifecycleAndSealedAnswer(t *testing.T) {
	s := newExtendedStore(t)
	mustResult := func(v Value, e error) Value {
		t.Helper()
		if e != nil {
			t.Fatal(e)
		}
		return v
	}
	mustResult(s.AddConcept("boundedness", "Boundedness", "Metric-space bounds", "analysis"))
	session := mustResult(s.StartLearningSession("Explain boundedness", "boundedness", "learn", nil))
	sid := session["id"].(string)
	opts := []QuestionOption{{ID: "a", Label: "Every sequence has a convergent subsequence"}, {ID: "b", Label: "There is a global bound on magnitudes"}, {ID: "c", Label: "All values are positive"}}
	question := mustResult(s.OpenLearningQuestion(sid, "boundedness", "", "diagnostic", "Which is a bound?", "multiple_choice", opts, "b", "Definition of boundedness"))
	qid := question["id"].(string)
	if strings.Contains(js(question), "Definition of boundedness") || strings.Contains(js(question), "expected_answer") {
		t.Fatal("sealed key leaked", question)
	}
	before := mustResult(s.PendingLearningQuestion(sid))
	if before["pending"] != true {
		t.Fatal(before)
	}
	_, err := s.OpenLearningQuestion(sid, "boundedness", "", "diagnostic", "Another?", "free_response", nil, "yes", "rubric")
	checkErr(t, err)
	_, err = s.AdvanceLearningSession(sid, "plan", nil)
	checkErr(t, err)
	_, err = s.AnswerLearningQuestion(qid, "not-an-option", false)
	checkErr(t, err)
	answered := mustResult(s.AnswerLearningQuestionWithReflection(qid, "b", false, "A common finite bound limits the magnitude of every term.", nil))
	if answered["status"] != "answered" || strings.Contains(js(answered), "expected_answer") {
		t.Fatal(answered)
	}
	_, err = s.AnswerLearningQuestion(qid, "a", false)
	checkErr(t, err)
	_, err = s.GradeLearningQuestion(qid, "gap", "Contrary to key", .9)
	checkErr(t, err)
	graded := mustResult(s.GradeLearningQuestionWithReasoning(qid, "supported", "Correct by the precommitted definition", .94, "supported", "The explanation supplies the uniform magnitude condition."))
	if graded["expected_answer"] != "b" || graded["mastery_unchanged"] != true {
		t.Fatal(graded)
	}
	updated := mustResult(s.GetLearningSession(sid))
	if len(updated["probes"].([]Value)) != 1 || updated["probes"].([]Value)[0]["outcome"] != "supported" {
		t.Fatal("diagnostic answer did not enter the adaptive history", updated)
	}
	recommend := mustResult(s.RecommendSessionProbe(sid))
	if recommend["action"] != "plan" {
		t.Fatal("diagnostic evidence not used for the next lesson decision", recommend)
	}
	c := mustResult(s.Concept("boundedness"))
	if c["mastery"].(Value)["level"] != 0 {
		t.Fatal("question awarded mastery", c)
	}
	_, err = s.GradeLearningQuestion(qid, "supported", "Duplicate", .5)
	checkErr(t, err)
	done := mustResult(s.PendingLearningQuestion(sid))
	if done["pending"] != false {
		t.Fatal(done)
	}
	mustResult(s.AdvanceLearningSession(sid, "plan", nil))
}

func TestRevisableTeachingQuestionsAndReturnAfterRestart(t *testing.T) {
	dir := t.TempDir() + "/q.db"
	s, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	mustResult := func(v Value, e error) Value {
		t.Helper()
		if e != nil {
			t.Fatal(e)
		}
		return v
	}
	mustResult(s.AddConcept("induction", "Induction", "Deductive proof", "logic"))
	sess := mustResult(s.StartLearningSession("Learn induction", "induction", "learn", nil))
	sid := sess["id"].(string)
	_, err = s.OpenLearningQuestion(sid, "induction", "", "prediction", "Predict the next step", "free_response", nil, "base case", "Compare expectation to true case")
	checkErr(t, err)
	mustResult(s.AdvanceLearningSession(sid, "plan", nil))
	mustResult(s.BuildLearningPlan(sid, nil, nil))
	mustResult(s.AdvanceLearningSession(sid, "teach", nil))
	q := mustResult(s.OpenLearningQuestion(sid, "induction", "", "prediction", "What should we establish first?", "free_response", nil, "base case", "Check initial value"))
	id := q["id"].(string)
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	s, err = Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	resumed := mustResult(s.GetLearningQuestion(id))
	if resumed["status"] != "pending" {
		t.Fatal(resumed)
	}
	mustResult(s.AnswerLearningQuestion(id, "We need a base case", false))
	mustResult(s.GradeLearningQuestion(id, "supported", "Good prediction", .81))
	q2 := mustResult(s.OpenLearningQuestion(sid, "induction", "", "check", "Why does the induction step preserve truth?", "free_response", nil, "P(n) implies P(n+1)", "Identify the implication"))
	if strings.Contains(js(q2), "expected_answer") {
		t.Fatal("hidden key leaked")
	}
	_, err = s.AdvanceLearningSession(sid, "practice", nil)
	checkErr(t, err)
	mustResult(s.AnswerLearningQuestion(q2["id"].(string), "Not sure", true))
	mustResult(s.GradeLearningQuestion(q2["id"].(string), "gap", "Missing implication", .86))
	mustResult(s.AdvanceLearningSession(sid, "practice", nil))
}
